package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProductionPanic/rootnet-cli/internal/sshx"
	"github.com/ProductionPanic/rootnet-cli/internal/transfer"
	"github.com/ProductionPanic/rootnet-cli/internal/vfs"
)

// location is a cp argument: a local path or host:path.
type location struct {
	query string // host query, "" for local
	path  string
}

// parseLocation splits "host:path". A colon after a slash (./a:b) or a
// single-letter prefix (C:\x) means a local path.
func parseLocation(arg string) location {
	i := strings.IndexByte(arg, ':')
	if i <= 0 || strings.ContainsAny(arg[:i], `/\`) || (i == 1 && len(arg) > 2 && (arg[2] == '\\' || arg[2] == '/')) {
		return location{path: arg}
	}
	return location{query: arg[:i], path: arg[i+1:]}
}

// connections opens each remote host once.
type connections struct {
	a     *app
	ctx   context.Context
	byID  map[int64]*vfs.RemoteFS
	names map[string]*vfs.RemoteFS
}

func (c *connections) fs(loc location) (vfs.FS, string, error) {
	if loc.query == "" {
		p, err := filepath.Abs(expandHome(loc.path))
		return vfs.LocalFS{}, p, err
	}
	r, ok := c.names[loc.query]
	if !ok {
		h, err := c.a.resolve(c.ctx, loc.query)
		if err != nil {
			return nil, "", err
		}
		if r, ok = c.byID[h.ID]; !ok {
			if r, err = sshx.DialSFTP(c.ctx, h); err != nil {
				return nil, "", err
			}
			c.byID[h.ID] = r
			_ = c.a.store.MarkUsed(c.ctx, h.ID)
		}
		c.names[loc.query] = r
	}
	p, err := r.Resolve(loc.path)
	return r, p, err
}

func (c *connections) close() {
	for _, r := range c.byID {
		r.Close()
	}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func (a *app) cpCmd() *cobra.Command {
	var overwrite, skip, newer, rename bool
	var workers int
	cmd := &cobra.Command{
		Use:   "cp <source>... <destination>",
		Short: "Copy files to, from or between hosts",
		Long: `Copy files and directories (recursively) using host:path for remote
locations. Remote paths are relative to the login directory unless they
start with "/". Transfers go over ssh using the same config as connecting.

Files are written as name.rootnet-part and renamed when complete, so an
interrupted copy never leaves a half-written file in place.`,
		Example: `  rootnet cp ./dump.sql appel:/tmp/
  rootnet cp appel:/var/www/site/wp-config.php .
  rootnet cp -r ./theme appel:wp-content/themes/   # directories are always recursive
  rootnet cp appel:backup.tar.gz other:/srv/       # host to host`,
		Args: cobra.MinimumNArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if strings.Contains(toComplete, ":") || strings.ContainsAny(toComplete, "/.~") {
				return nil, cobra.ShellCompDirectiveDefault
			}
			hosts, dir := a.completeHosts(cmd, args, toComplete)
			for i := range hosts {
				name, _, _ := strings.Cut(string(hosts[i]), "\t")
				hosts[i] = cobra.Completion(name + ":")
			}
			return hosts, dir | cobra.ShellCompDirectiveNoSpace
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			policy := transfer.Ask
			switch {
			case overwrite:
				policy = transfer.Overwrite
			case skip:
				policy = transfer.Skip
			case newer:
				policy = transfer.OverwriteIfNewer
			case rename:
				policy = transfer.RenameNew
			}
			return a.copy(cmd.Context(), cmd.ErrOrStderr(), args[:len(args)-1], args[len(args)-1], policy, workers)
		},
	}
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "f", false, "overwrite existing files")
	cmd.Flags().BoolVar(&skip, "skip", false, "skip existing files")
	cmd.Flags().BoolVarP(&newer, "newer", "u", false, "overwrite only if the source is newer")
	cmd.Flags().BoolVar(&rename, "rename", false, `keep both, writing "name (1).ext"`)
	cmd.Flags().BoolP("recursive", "r", true, "accepted for scp compatibility; directories are always copied recursively")
	cmd.Flags().IntVarP(&workers, "jobs", "j", 4, "files copied in parallel")
	cmd.MarkFlagsMutuallyExclusive("overwrite", "skip", "newer", "rename")
	return cmd
}

func (a *app) copy(ctx context.Context, out io.Writer, srcArgs []string, dstArg string, policy transfer.Policy, workers int) error {
	conns := &connections{a: a, ctx: ctx, byID: map[int64]*vfs.RemoteFS{}, names: map[string]*vfs.RemoteFS{}}
	defer conns.close()

	// All sources must live on the same file system.
	var srcFS vfs.FS
	var sources []string
	for _, s := range srcArgs {
		f, p, err := conns.fs(parseLocation(s))
		if err != nil {
			return err
		}
		if srcFS != nil && f != srcFS {
			return errors.New("all sources must be on the same host")
		}
		srcFS = f
		sources = append(sources, p)
	}
	dstFS, dst, err := conns.fs(parseLocation(dstArg))
	if err != nil {
		return err
	}

	req := transfer.Request{Src: srcFS, Sources: sources, Dst: dstFS, DstDir: dst, Policy: policy}
	existing, statErr := dstFS.Stat(dst)
	switch {
	case statErr == nil && existing.IsDir:
		// copy into it
	case len(sources) > 1 || strings.HasSuffix(dstArg, "/"):
		if statErr == nil {
			return fmt.Errorf("%s is not a directory", dstArg)
		}
		if err := dstFS.MkdirAll(dst); err != nil {
			return err
		}
	default:
		req.DstDir, req.DstName = dstFS.Dir(dst), dstFS.Base(dst)
	}

	interactiveOut := false
	if f, ok := out.(*os.File); ok {
		interactiveOut = isTerminal(f)
	}
	askable := interactive()
	var prompt *bufio.Reader
	if policy == transfer.Ask && askable {
		tty, err := os.Open("/dev/tty")
		if err == nil {
			defer tty.Close()
			prompt = bufio.NewReader(tty)
		} else {
			prompt = bufio.NewReader(os.Stdin)
		}
	}

	e := transfer.New(workers)
	done := make(chan transfer.Progress, 1)
	skippedConflicts := 0
	go func() {
		for ev := range e.Events() {
			switch ev := ev.(type) {
			case transfer.Progress:
				if interactiveOut {
					fmt.Fprint(out, "\r\033[K"+progressLine(ev))
				}
				if ev.Finished {
					done <- ev
					return
				}
			case transfer.Conflict:
				if prompt == nil {
					skippedConflicts++
					fmt.Fprintf(out, "%sskipped %s: already exists\n", clearLine(interactiveOut), ev.File.DstPath)
					ev.Reply <- transfer.Decision{Policy: transfer.Skip}
					continue
				}
				ev.Reply <- askConflict(out, prompt, ev)
			}
		}
	}()
	e.Start(ctx, req)
	p := <-done
	if interactiveOut {
		fmt.Fprint(out, "\r\033[K")
	}

	if copied := p.Settled - p.Failed - p.Skipped; copied > 0 || p.Skipped > 0 {
		fmt.Fprintf(out, "%s: %s", p.Label, plural(copied, "file"))
		if p.Skipped > 0 {
			fmt.Fprintf(out, " (%d skipped)", p.Skipped)
		}
		if p.Rate > 0 && copied > 0 {
			fmt.Fprintf(out, ", %s at %s", transfer.FormatBytes(p.Done), transfer.FormatRate(p.Rate))
		}
		fmt.Fprintln(out)
	}
	switch {
	case p.Cancelled:
		return errors.New("cancelled")
	case p.Err != nil:
		return p.Err
	case skippedConflicts > 0:
		return fmt.Errorf("%d existing files skipped; use --overwrite, --newer or --rename", skippedConflicts)
	}
	return nil
}

func clearLine(tty bool) string {
	if tty {
		return "\r\033[K"
	}
	return ""
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func progressLine(p transfer.Progress) string {
	s := fmt.Sprintf("%s  %3.0f%%  %s/%s", p.Label, p.Fraction()*100,
		transfer.FormatBytes(p.Done), transfer.FormatBytes(p.Total))
	if p.Rate > 0 {
		s += "  " + transfer.FormatRate(p.Rate)
	}
	if eta := p.ETA(); eta != "" {
		s += "  ETA " + eta
	}
	return s
}

func askConflict(out io.Writer, in *bufio.Reader, c transfer.Conflict) transfer.Decision {
	for {
		fmt.Fprintf(out, "%s%s exists (%s, %s).\n  [o]verwrite [s]kip [n]ewer only [r]ename — capital letter applies to all: ",
			clearLine(true), c.File.DstPath, transfer.FormatBytes(c.Existing.Size), c.Existing.ModTime.Local().Format("2 Jan 2006 15:04"))
		line, err := in.ReadString('\n')
		if err != nil {
			return transfer.Decision{Policy: transfer.Skip}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		forAll := strings.ToUpper(line) == line
		switch strings.ToLower(line)[0] {
		case 'o':
			return transfer.Decision{Policy: transfer.Overwrite, ForAll: forAll}
		case 's':
			return transfer.Decision{Policy: transfer.Skip, ForAll: forAll}
		case 'n':
			return transfer.Decision{Policy: transfer.OverwriteIfNewer, ForAll: forAll}
		case 'r':
			return transfer.Decision{Policy: transfer.RenameNew, ForAll: forAll}
		}
	}
}
