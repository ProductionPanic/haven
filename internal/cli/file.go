package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ProductionPanic/haven/v2/internal/store"
	"github.com/ProductionPanic/haven/v2/internal/transfer"
	"github.com/ProductionPanic/haven/v2/internal/tui/theme"
	"github.com/ProductionPanic/haven/v2/internal/vfs"
)

// The file commands are meant to be driven by scripts and LLMs: they never
// prompt, never open a picker, take content from a flag or stdin and refuse
// risky things (overwriting, deleting directories, touching production
// hosts) unless a flag says so explicitly.

const (
	// defaultMaxRead is how much "file read" prints unless --max-bytes says otherwise.
	defaultMaxRead = 1 << 20
	// maxContent caps what create and modify accept and load.
	maxContent = 10 << 20
	// maxScan caps how far "file read" scans into a file looking for --offset.
	maxScan = 64 << 20
)

func (a *app) fileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "file",
		Short: "List, read, create, modify and delete files, locally or on a host",
		Long: `Work with single files without a terminal UI, locally or on a host with
host:path (the same syntax as "haven cp"). Made for scripts and LLMs: nothing
prompts, host queries must match exactly one host, and every command takes
--json. Run "haven llm" for a guide aimed at LLMs.

Writes are atomic (a half-written file never replaces the old one). Changing
files on a host whose environment is "production" needs --allow-production.`,
		Example: `  haven file list acme:/var/www/site
  haven file read acme:wp-config.php --offset 1 --limit 40
  echo 'hello' | haven file create acme:/tmp/hello.txt
  haven file modify acme:/tmp/hello.txt --old hello --new goodbye
  haven file delete acme:/tmp/hello.txt`,
	}
	cmd.AddCommand(
		a.fileListCmd(),
		a.fileReadCmd(),
		a.fileCreateCmd(),
		a.fileModifyCmd(),
		a.fileDeleteCmd(),
	)
	return cmd
}

// fileConns opens connections for a file command. write commands refuse
// production hosts unless allowProd is set.
func (a *app) fileConns(ctx context.Context, write, allowProd bool) *connections {
	c := newConnections(ctx, a)
	c.strict = true
	if write {
		c.guard = func(h store.Host) error {
			if theme.IsProduction(h.Environment) && !allowProd {
				return fmt.Errorf("%s is a production host; pass --allow-production to change files there, but only when the user asked for it", h.Name)
			}
			return nil
		}
	}
	return c
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func notExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// looksBinary reports whether data (the start of a file) has a NUL byte.
func looksBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

// rejectSymlink stops writes through symlinks: the atomic replace would
// swap the link itself for a regular file.
func rejectSymlink(t target, e vfs.Entry) error {
	if e.Link != "" {
		return fmt.Errorf("%s is a symlink to %s; use the target path instead", t.display(), e.Link)
	}
	return nil
}

// readContent returns the content to write: --content when given, stdin
// otherwise. Empty content is an error unless allowEmpty, so a forgotten
// pipe can't silently produce an empty file.
func readContent(cmd *cobra.Command, content string, given, allowEmpty bool) (string, error) {
	if !given {
		in := cmd.InOrStdin()
		if f, ok := in.(*os.File); ok && isTerminal(f) {
			return "", errors.New("no content: pass --content or pipe it to stdin")
		}
		b, err := io.ReadAll(io.LimitReader(in, maxContent+1))
		if err != nil {
			return "", err
		}
		content = string(b)
	}
	switch {
	case len(content) > maxContent:
		return "", fmt.Errorf("content is larger than %s", transfer.FormatBytes(maxContent))
	case content == "" && !allowEmpty:
		return "", errors.New("content is empty; pass --empty if an empty file is what you want")
	}
	return content, nil
}

// ---- list ----

type listEntry struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"` // file, dir or link
	Size     int64     `json:"size"`
	Mode     string    `json:"mode"`
	Modified time.Time `json:"modified"`
	Link     string    `json:"link,omitempty"`
}

type listResult struct {
	Host    string      `json:"host,omitempty"`
	Path    string      `json:"path"`
	Entries []listEntry `json:"entries"`
}

func (a *app) fileListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:               "list [location]",
		Aliases:           []string{"ls"},
		Short:             "List a directory (default: the current directory)",
		Long:              "List a directory, directories first. Hidden files are included.",
		Example:           "  haven file list\n  haven file list acme:/var/www/site --json",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: a.completeLocations,
		RunE: func(cmd *cobra.Command, args []string) error {
			loc := "."
			if len(args) == 1 {
				loc = args[0]
			}
			conns := a.fileConns(cmd.Context(), false, false)
			defer conns.close()
			t, err := conns.target(parseLocation(loc))
			if err != nil {
				return err
			}
			e, err := t.fs.Stat(t.path)
			if err != nil {
				return err
			}
			if !e.IsDir {
				return fmt.Errorf("%s is a file, not a directory; read it with \"haven file read\"", t.display())
			}
			entries, err := t.fs.ReadDir(t.path)
			if err != nil {
				return err
			}
			vfs.Sort(entries, vfs.SortName)
			res := listResult{Path: t.path, Entries: make([]listEntry, 0, len(entries))}
			if t.host != nil {
				res.Host = t.host.Name
			}
			for _, e := range entries {
				le := listEntry{Name: e.Name, Type: "file", Size: e.Size, Mode: e.Mode.Perm().String(), Modified: e.ModTime, Link: e.Link}
				switch {
				case e.Link != "":
					le.Type = "link"
				case e.IsDir:
					le.Type = "dir"
				}
				res.Entries = append(res.Entries, le)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return printJSON(out, res)
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for _, e := range res.Entries {
				name := e.Name
				switch e.Type {
				case "dir":
					name += "/"
				case "link":
					name += " -> " + e.Link
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", e.Type, e.Mode, e.Size, e.Modified.Local().Format("2006-01-02 15:04"), name)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

// ---- read ----

type readResult struct {
	Host      string    `json:"host,omitempty"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	StartLine int       `json:"start_line"`
	EndLine   int       `json:"end_line"`
	Truncated bool      `json:"truncated"` // more content follows EndLine
	Content   string    `json:"content"`
}

// readLines reads lines from r starting at line offset (1-based) for at
// most limit lines (0 = no limit) and maxBytes bytes of output.
func readLines(r io.Reader, offset, limit, maxBytes int) (readResult, error) {
	res := readResult{StartLine: offset}
	br := bufio.NewReader(io.LimitReader(r, maxScan))
	if head, _ := br.Peek(8000); looksBinary(head) {
		return res, errors.New("binary file; not showing it")
	}
	var buf strings.Builder
	for n := 1; ; n++ {
		line, err := br.ReadString('\n')
		if line != "" && n >= offset {
			if limit > 0 && n-offset >= limit {
				res.Truncated = true
				break
			}
			if buf.Len()+len(line) > maxBytes {
				res.Truncated = true
				if buf.Len() == 0 { // a single line over the limit: cut it
					buf.WriteString(line[:maxBytes])
					res.EndLine = n
				}
				break
			}
			buf.WriteString(line)
			res.EndLine = n
		}
		if err != nil {
			if err != io.EOF {
				return res, err
			}
			break
		}
	}
	res.Content = buf.String()
	if res.EndLine == 0 {
		res.EndLine = offset - 1
	}
	return res, nil
}

func (a *app) fileReadCmd() *cobra.Command {
	var asJSON, numbered bool
	var offset, limit, maxBytes int
	cmd := &cobra.Command{
		Use:   "read <location>",
		Short: "Print a file's contents",
		Long: `Print a text file. Without --json only the content goes to stdout. Big
files are cut off after --max-bytes (default 1 MiB); a note on stderr (or
"truncated" in --json) tells you where to continue with --offset. Binary
files are refused.`,
		Example: `  haven file read acme:/etc/hostname
  haven file read acme:/var/log/nginx/error.log --offset 200 --limit 50 --numbered
  haven file read ./config.toml --json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeLocations,
		RunE: func(cmd *cobra.Command, args []string) error {
			if offset < 1 {
				return errors.New("--offset starts at 1")
			}
			if limit < 0 || maxBytes < 1 {
				return errors.New("--limit and --max-bytes must be positive")
			}
			conns := a.fileConns(cmd.Context(), false, false)
			defer conns.close()
			t, err := conns.target(parseLocation(args[0]))
			if err != nil {
				return err
			}
			e, err := t.fs.Stat(t.path)
			if err != nil {
				return err
			}
			if e.IsDir {
				return fmt.Errorf("%s is a directory; list it with \"haven file list\"", t.display())
			}
			f, err := t.fs.Open(t.path)
			if err != nil {
				return err
			}
			defer f.Close()
			res, err := readLines(f, offset, limit, maxBytes)
			if err != nil {
				return fmt.Errorf("%s: %w", t.display(), err)
			}
			res.Path, res.Size, res.Modified = t.path, e.Size, e.ModTime
			if t.host != nil {
				res.Host = t.host.Name
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return printJSON(out, res)
			}
			content := res.Content
			if numbered {
				content = numberLines(content, res.StartLine)
			}
			if _, err := io.WriteString(out, content); err != nil {
				return err
			}
			if res.Truncated {
				fmt.Fprintf(cmd.ErrOrStderr(), "haven: %s: stopped after line %d; continue with --offset %d\n", t.display(), res.EndLine, res.EndLine+1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON (path, size, lines, truncated, content)")
	cmd.Flags().BoolVarP(&numbered, "numbered", "n", false, "prefix lines with their line number")
	cmd.Flags().IntVar(&offset, "offset", 1, "first line to show (1-based)")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of lines (0 = all)")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", defaultMaxRead, "maximum number of bytes to print")
	return cmd
}

func numberLines(s string, first int) string {
	var b strings.Builder
	for i, line := range strings.SplitAfter(s, "\n") {
		if line == "" {
			continue
		}
		fmt.Fprintf(&b, "%6d\t%s", first+i, line)
	}
	return b.String()
}

// ---- create ----

type changeResult struct {
	Action       string `json:"action"`
	Host         string `json:"host,omitempty"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	Replacements int    `json:"replacements,omitempty"`
}

func (a *app) fileCreateCmd() *cobra.Command {
	var asJSON, force, parents, allowProd, empty bool
	var content string
	cmd := &cobra.Command{
		Use:   "create <location>",
		Short: "Create a file from --content or stdin",
		Long: `Create a new file. The content comes from --content or, when that is
absent, from stdin (best for anything with newlines or quotes). It fails when
the file already exists unless --force is given, and when the parent
directory is missing unless --parents is given. To change part of an existing
file use "haven file modify".`,
		Example: `  haven file create ./notes.txt --content 'hello'
  haven file create acme:/var/www/site/robots.txt <<'EOF'
  User-agent: *
  Disallow: /private/
  EOF
  haven file create --parents acme:backups/2026/readme.txt --content 'x'`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeLocations,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readContent(cmd, content, cmd.Flags().Changed("content"), empty)
			if err != nil {
				return err
			}
			conns := a.fileConns(cmd.Context(), true, allowProd)
			defer conns.close()
			t, err := conns.target(parseLocation(args[0]))
			if err != nil {
				return err
			}
			var mode fs.FileMode
			switch e, err := t.fs.Stat(t.path); {
			case err == nil && e.IsDir:
				return fmt.Errorf("%s is a directory", t.display())
			case err == nil && !force:
				return fmt.Errorf("%s already exists; use \"haven file modify\" to change it or --force to replace it", t.display())
			case err == nil:
				if err := rejectSymlink(t, e); err != nil {
					return err
				}
				mode = e.Mode.Perm()
			case !notExist(err):
				return err
			}
			if err := ensureParent(t, parents); err != nil {
				return err
			}
			if err := transfer.PutFile(t.fs, t.path, strings.NewReader(body), mode); err != nil {
				return err
			}
			return reportChange(cmd, t, "created", int64(len(body)), 0, asJSON)
		},
	}
	cmd.Flags().StringVar(&content, "content", "", "content of the file (default: read stdin)")
	cmd.Flags().BoolVar(&empty, "empty", false, "allow creating an empty file")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace the file when it already exists")
	cmd.Flags().BoolVarP(&parents, "parents", "p", false, "create missing parent directories")
	cmd.Flags().BoolVar(&allowProd, "allow-production", false, "allow changes on a production host")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func ensureParent(t target, create bool) error {
	dir := t.fs.Dir(t.path)
	e, err := t.fs.Stat(dir)
	switch {
	case err == nil && !e.IsDir:
		return fmt.Errorf("%s is not a directory", dir)
	case err == nil:
		return nil
	case !notExist(err):
		return err
	case !create:
		return fmt.Errorf("directory %s does not exist; pass --parents to create it", dir)
	}
	return t.fs.MkdirAll(dir)
}

func reportChange(cmd *cobra.Command, t target, action string, size int64, replacements int, asJSON bool) error {
	out := cmd.OutOrStdout()
	if asJSON {
		res := changeResult{Action: action, Path: t.path, Size: size, Replacements: replacements}
		if t.host != nil {
			res.Host = t.host.Name
		}
		return printJSON(out, res)
	}
	detail := fmt.Sprintf("%d bytes", size)
	if replacements > 0 {
		detail += fmt.Sprintf(", %s replaced", plural(replacements, "occurrence"))
	}
	_, err := fmt.Fprintf(out, "%s %s (%s)\n", action, t.display(), detail)
	return err
}

// ---- modify ----

// replaceText swaps old for new in s. old must occur exactly once, unless
// all is set.
func replaceText(s, old, repl string, all bool) (string, int, error) {
	if old == "" {
		return "", 0, errors.New("--old must not be empty")
	}
	switch n := strings.Count(s, old); {
	case n == 0:
		return "", 0, errors.New("--old text not found; read the file again and copy the text exactly, including whitespace")
	case n > 1 && !all:
		return "", 0, fmt.Errorf("--old text occurs %d times; include more surrounding text to make it unique or pass --all", n)
	case all:
		return strings.ReplaceAll(s, old, repl), n, nil
	}
	return strings.Replace(s, old, repl, 1), 1, nil
}

func (a *app) fileModifyCmd() *cobra.Command {
	var asJSON, allowProd, appendMode, overwrite, all, empty bool
	var oldText, newText, content string
	cmd := &cobra.Command{
		Use:   "modify <location>",
		Short: "Change an existing file: replace text, append, or overwrite",
		Long: `Change a file that already exists. Pick exactly one mode:

  --old TEXT --new TEXT   replace TEXT. It must occur exactly once (or pass
                          --all to replace every occurrence). --new may be
                          empty to delete the text.
  --append                add the content (--content or stdin) to the end
  --overwrite             replace the whole file with the content

Permissions are kept. Binary files and files over 10 MiB are refused.`,
		Example: `  haven file modify acme:wp-config.php --old "WP_DEBUG', false" --new "WP_DEBUG', true"
  echo 'Disallow: /tmp/' | haven file modify acme:/var/www/site/robots.txt --append
  haven file modify ./notes.txt --overwrite --content 'fresh start'`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeLocations,
		RunE: func(cmd *cobra.Command, args []string) error {
			replacing := cmd.Flags().Changed("old") || cmd.Flags().Changed("new")
			modes := 0
			for _, on := range []bool{replacing, appendMode, overwrite} {
				if on {
					modes++
				}
			}
			switch {
			case modes != 1:
				return errors.New("choose exactly one mode: --old with --new, --append or --overwrite")
			case replacing && !(cmd.Flags().Changed("old") && cmd.Flags().Changed("new")):
				return errors.New("--old and --new go together")
			case replacing && cmd.Flags().Changed("content"):
				return errors.New("--content does not combine with --old/--new")
			}
			var body string
			if !replacing {
				var err error
				body, err = readContent(cmd, content, cmd.Flags().Changed("content"), empty && overwrite)
				if err != nil {
					return err
				}
			}

			conns := a.fileConns(cmd.Context(), true, allowProd)
			defer conns.close()
			t, err := conns.target(parseLocation(args[0]))
			if err != nil {
				return err
			}
			e, err := t.fs.Stat(t.path)
			switch {
			case notExist(err):
				return fmt.Errorf("%s does not exist; create it with \"haven file create\"", t.display())
			case err != nil:
				return err
			case e.IsDir:
				return fmt.Errorf("%s is a directory", t.display())
			}
			if err := rejectSymlink(t, e); err != nil {
				return err
			}
			if e.Size > maxContent {
				return fmt.Errorf("%s is larger than %s", t.display(), transfer.FormatBytes(maxContent))
			}

			var next string
			replaced := 0
			if overwrite {
				next = body
			} else {
				cur, err := loadText(t)
				if err != nil {
					return err
				}
				if appendMode {
					next = cur + body
				} else if next, replaced, err = replaceText(cur, oldText, newText, all); err != nil {
					return fmt.Errorf("%s: %w", t.display(), err)
				}
				if len(next) > maxContent {
					return fmt.Errorf("the result would be larger than %s", transfer.FormatBytes(maxContent))
				}
			}
			if err := transfer.PutFile(t.fs, t.path, strings.NewReader(next), e.Mode.Perm()); err != nil {
				return err
			}
			action := "modified"
			if overwrite {
				action = "overwrote"
			} else if appendMode {
				action = "appended to"
			}
			return reportChange(cmd, t, action, int64(len(next)), replaced, asJSON)
		},
	}
	cmd.Flags().StringVar(&oldText, "old", "", "text to replace (must occur exactly once unless --all)")
	cmd.Flags().StringVar(&newText, "new", "", "replacement text (may be empty)")
	cmd.Flags().BoolVar(&all, "all", false, "replace every occurrence of --old")
	cmd.Flags().BoolVar(&appendMode, "append", false, "append the content to the file")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace the whole file with the content")
	cmd.Flags().StringVar(&content, "content", "", "content for --append or --overwrite (default: read stdin)")
	cmd.Flags().BoolVar(&empty, "empty", false, "with --overwrite: allow emptying the file")
	cmd.Flags().BoolVar(&allowProd, "allow-production", false, "allow changes on a production host")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

// loadText reads a whole text file.
func loadText(t target) (string, error) {
	f, err := t.fs.Open(t.path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxContent+1))
	if err != nil {
		return "", err
	}
	if looksBinary(b) {
		return "", fmt.Errorf("%s is a binary file", t.display())
	}
	return string(b), nil
}

// ---- delete ----

type deleteResult struct {
	Action string `json:"action"`
	Host   string `json:"host,omitempty"`
	Path   string `json:"path"`
	Dir    bool   `json:"directory"`
}

func (a *app) fileDeleteCmd() *cobra.Command {
	var asJSON, recursive, allowProd, ignoreMissing bool
	cmd := &cobra.Command{
		Use:     "delete <location>...",
		Aliases: []string{"rm"},
		Short:   "Delete files (directories need --recursive)",
		Long: `Delete one or more files. Directories are only removed with --recursive.
Every location is checked before anything is deleted, so one mistake doesn't
leave a half-done job. The filesystem root and the home directory are never
deleted. This does not touch haven's host list; for that use "haven rm".`,
		Example: `  haven file delete acme:/tmp/old.log
  haven file delete --recursive acme:/tmp/build-cache
  haven file delete --ignore-missing ./a.txt ./b.txt`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeLocations,
		RunE: func(cmd *cobra.Command, args []string) error {
			conns := a.fileConns(cmd.Context(), true, allowProd)
			defer conns.close()

			type item struct {
				t   target
				dir bool
			}
			var items []item
			for _, arg := range args {
				t, err := conns.target(parseLocation(arg))
				if err != nil {
					return err
				}
				e, err := t.fs.Stat(t.path)
				switch {
				case notExist(err) && ignoreMissing:
					continue
				case notExist(err):
					return fmt.Errorf("%s does not exist", t.display())
				case err != nil:
					return err
				}
				if err := protectedPath(t); err != nil {
					return err
				}
				dir := e.IsDir && e.Link == ""
				if dir && !recursive {
					return fmt.Errorf("%s is a directory; pass --recursive to delete it and everything in it", t.display())
				}
				items = append(items, item{t, dir})
			}

			out := cmd.OutOrStdout()
			for _, it := range items {
				if err := it.t.fs.Remove(it.t.path); err != nil {
					return fmt.Errorf("%s: %w", it.t.display(), err)
				}
				if asJSON {
					res := deleteResult{Action: "deleted", Path: it.t.path, Dir: it.dir}
					if it.t.host != nil {
						res.Host = it.t.host.Name
					}
					if err := printJSON(out, res); err != nil {
						return err
					}
					continue
				}
				fmt.Fprintf(out, "deleted %s\n", it.t.display())
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "delete directories and their contents")
	cmd.Flags().BoolVar(&ignoreMissing, "ignore-missing", false, "don't fail when a path doesn't exist")
	cmd.Flags().BoolVar(&allowProd, "allow-production", false, "allow changes on a production host")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON (one object per deleted path)")
	return cmd
}

// protectedPath refuses to delete the root or the home directory.
func protectedPath(t target) error {
	if t.fs.Dir(t.path) == t.path {
		return fmt.Errorf("refusing to delete %s", t.display())
	}
	if home, err := t.fs.Home(); err == nil && home == t.path {
		return fmt.Errorf("refusing to delete the home directory %s", t.display())
	}
	return nil
}
