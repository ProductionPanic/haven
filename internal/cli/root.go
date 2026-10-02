// Package cli wires rootnet's cobra commands together.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"charm.land/fang/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/ProductionPanic/rootnet-cli/internal/legacy"
	"github.com/ProductionPanic/rootnet-cli/internal/match"
	"github.com/ProductionPanic/rootnet-cli/internal/sshx"
	"github.com/ProductionPanic/rootnet-cli/internal/store"
	"github.com/ProductionPanic/rootnet-cli/internal/tui"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/picker"
)

// app carries state shared by all commands.
type app struct {
	dbPath string
	store  *store.Store
}

// Execute runs the rootnet command line.
func Execute(ctx context.Context) error {
	a := &app{}
	root := a.rootCmd()
	defer a.close()
	return fang.Execute(ctx, root,
		fang.WithVersion(version()),
		fang.WithErrorHandler(errorHandler),
	)
}

// errorHandler is fang's default handler without the capitalisation,
// which mangles host names in messages.
func errorHandler(w io.Writer, styles fang.Styles, err error) {
	styles.ErrorText = styles.ErrorText.UnsetTransform()
	fang.DefaultErrorHandler(w, styles, err)
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "dev"
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rootnet [query]",
		Short: "Find and connect to your servers",
		Long: `rootnet keeps a list of servers and connects to them over ssh.

Without arguments, rootnet opens the host manager. With a query, it
connects straight away when exactly one host matches and opens a picker
otherwise. Results are ranked by how often and how
recently you used them. Use "rootnet ssh <query>" when a host is named like
one of the subcommands.`,
		Example: `  rootnet                    # open the host manager
  rootnet appel              # connect to appelenburg.nl
  rootnet get appel          # print web@server for use in scripts
  rootnet add                # add a host interactively
  scp dump.sql $(rootnet get appel):/tmp/`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: a.completeHosts,
		SilenceUsage:      true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return a.open(cmd.ErrOrStderr())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return a.ui(cmd.Context())
			}
			return a.connect(cmd.Context(), strings.Join(args, " "))
		},
	}
	root.PersistentFlags().StringVar(&a.dbPath, "db", "", "database path (default $ROOTNET_DB or ~/.config/rootnet/rootnet.db)")
	root.AddCommand(
		a.sshCmd(),
		a.uiCmd(),
		a.getCmd(),
		a.lsCmd(),
		a.addCmd(),
		a.editCmd(),
		a.rmCmd(),
		a.importCmd(),
		a.exportCmd(),
	)
	return root
}

// reserved reports whether name collides with a subcommand.
func reserved(name string) bool {
	switch strings.ToLower(name) {
	case "ssh", "get", "ls", "add", "edit", "rm", "import", "export", "completion", "help", "ui", "files", "cp", "man":
		return true
	}
	return false
}

func (a *app) sshCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "ssh <query>",
		Short:             "Connect to a host (works even for hosts named like a subcommand)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.connect(cmd.Context(), strings.Join(args, " "))
		},
	}
}

// open opens the database, importing ~/rootnet_hosts.txt on first run.
func (a *app) open(notice interface{ Write([]byte) (int, error) }) error {
	if a.store != nil {
		return nil
	}
	path := a.dbPath
	if path == "" {
		var err error
		if path, err = store.DefaultPath(); err != nil {
			return err
		}
	}
	_, statErr := os.Stat(path)
	fresh := errors.Is(statErr, os.ErrNotExist)

	s, err := store.Open(path)
	if err != nil {
		return err
	}
	a.store = s
	if fresh {
		return a.importLegacy(notice)
	}
	return nil
}

func (a *app) importLegacy(notice interface{ Write([]byte) (int, error) }) error {
	src := legacy.DefaultPath()
	f, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	hosts, err := legacy.Parse(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	ctx := context.Background()
	n := 0
	for _, h := range hosts {
		if _, err := a.store.Create(ctx, h); err == nil {
			n++
		}
	}
	if err := os.Rename(src, src+".bak"); err != nil {
		return err
	}
	fmt.Fprintf(notice, "rootnet: imported %d hosts from %s (original kept as %s.bak)\n", n, src, src)
	return nil
}

func (a *app) close() {
	if a.store != nil {
		a.store.Close()
		a.store = nil
	}
}

// resolve turns a query into a single host, opening the picker when the
// query is empty or ambiguous.
func (a *app) resolve(ctx context.Context, query string) (store.Host, error) {
	hosts, err := a.store.List(ctx, "")
	if err != nil {
		return store.Host{}, err
	}
	if len(hosts) == 0 {
		return store.Host{}, errors.New(`no hosts yet; add one with "rootnet add"`)
	}
	now := time.Now()
	matches := match.Find(hosts, query, now)
	title := "Rootnet hosts"
	switch {
	case len(matches) == 1 && query != "":
		return matches[0], nil
	case len(matches) == 0:
		matches = match.Rank(hosts, now)
		title = fmt.Sprintf("No match for %q", query)
	case query != "":
		title = fmt.Sprintf("%d hosts match %q", len(matches), query)
	}
	if !interactive() {
		return store.Host{}, ambiguousError(query, matches)
	}
	return picker.Pick(title, matches)
}

func ambiguousError(query string, hosts []store.Host) error {
	if len(hosts) > 10 {
		hosts = hosts[:10]
	}
	names := make([]string, len(hosts))
	for i, h := range hosts {
		names[i] = h.Name
	}
	return fmt.Errorf("%q is ambiguous: %s", query, strings.Join(names, ", "))
}

func isTerminal(f *os.File) bool { return term.IsTerminal(f.Fd()) }

// interactive reports whether a TUI can be shown: either stdin is a
// terminal or the controlling terminal can be opened (Bubble Tea falls back
// to /dev/tty, which is what makes `ssh $(rootnet get foo)` work).
func interactive() bool {
	if isTerminal(os.Stdin) {
		return true
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	tty.Close()
	return true
}

func (a *app) uiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Open the host manager (also what plain \"rootnet\" does)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.ui(cmd.Context())
		},
	}
}

// ui runs the full-screen host manager and connects to the host chosen there.
func (a *app) ui(ctx context.Context) error {
	if !interactive() {
		return errors.New("the host manager needs a terminal")
	}
	h, err := tui.Run(ctx, a.store)
	if err != nil || h == nil {
		return err
	}
	return a.exec(ctx, *h)
}

func (a *app) connect(ctx context.Context, query string) error {
	h, err := a.resolve(ctx, query)
	if errors.Is(err, picker.ErrCancelled) {
		return nil
	}
	if err != nil {
		return err
	}
	return a.exec(ctx, h)
}

// exec records the connection and replaces rootnet with ssh.
func (a *app) exec(ctx context.Context, h store.Host) error {
	args, err := sshx.ConnectArgs(h)
	if err != nil {
		return err
	}
	if err := a.store.MarkUsed(ctx, h.ID); err != nil {
		return err
	}
	a.close() // exec never returns, so deferred cleanup won't run
	return sshx.Exec(args)
}

func (a *app) completeHosts(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if err := a.open(cmd.ErrOrStderr()); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	hosts, err := a.store.List(cmd.Context(), "")
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var out []cobra.Completion
	prefix := strings.ToLower(toComplete)
	for _, h := range match.Rank(hosts, time.Now()) {
		if strings.HasPrefix(strings.ToLower(h.Name), prefix) {
			out = append(out, cobra.CompletionWithDesc(h.Name, h.Target()))
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
