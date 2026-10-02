package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ProductionPanic/rootnet-cli/internal/match"
	"github.com/ProductionPanic/rootnet-cli/internal/store"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/hostform"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/picker"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/theme"
)

func (a *app) getCmd() *cobra.Command {
	var format string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get [query]",
		Short: "Print user@host for a host",
		Long: `Print the ssh destination of a host. The picker (if needed) is drawn on
stderr, so only the result ends up on stdout.`,
		Example: `  ssh $(rootnet get appel)
  rootnet get appel --format '{{.User}}@{{.Hostname}}:{{.Port}}'
  rootnet get appel --json | jq .remote_path`,
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			var tmpl *template.Template
			if format != "" {
				var err error
				if tmpl, err = template.New("format").Parse(format); err != nil {
					return fmt.Errorf("--format: %w", err)
				}
			}
			h, err := a.resolve(cmd.Context(), strings.Join(args, " "))
			if errors.Is(err, picker.ErrCancelled) {
				return err
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case asJSON:
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(h)
			case tmpl != nil:
				if err := tmpl.Execute(out, h); err != nil {
					return err
				}
				_, err = fmt.Fprintln(out)
				return err
			default:
				_, err = fmt.Fprint(out, h.Target())
				if isTerminal(os.Stdout) {
					fmt.Fprintln(out)
				}
				return err
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "Go template applied to the host (fields: Name, User, Hostname, Port, RemotePath, ...)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the host as JSON")
	cmd.MarkFlagsMutuallyExclusive("format", "json")
	return cmd
}

func (a *app) lsCmd() *cobra.Command {
	var tag string
	var asJSON, byUse bool
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List hosts",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			hosts, err := a.store.List(cmd.Context(), tag)
			if err != nil {
				return err
			}
			if byUse {
				hosts = match.Rank(hosts, time.Now())
			}
			out := cmd.OutOrStdout()
			if asJSON {
				if hosts == nil {
					hosts = []store.Host{}
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(hosts)
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tTARGET\tPORT\tENV\tTAGS\tLAST USED")
			for _, h := range hosts {
				last := "-"
				if h.LastUsedAt != nil {
					last = humanizeSince(time.Since(*h.LastUsedAt))
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n", h.Name, h.Target(), h.Port,
					dash(h.Environment), dash(strings.Join(h.Tags, ",")), last)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "only hosts with this tag")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.Flags().BoolVarP(&byUse, "recent", "r", false, "sort by frecency instead of name")
	cmd.RegisterFlagCompletionFunc("tag", a.completeTags)
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func humanizeSince(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// hostFlags are the per-field flags shared by add and edit.
type hostFlags struct {
	name, user, hostname, identity, jump, path, extra, notes, env string
	port                                                          int
	tags                                                          []string
}

func (f *hostFlags) register(fs *pflag.FlagSet, withName bool) {
	if withName {
		fs.StringVar(&f.name, "name", "", "rename the host")
		fs.StringVar(&f.user, "user", "", "ssh user")
		fs.StringVar(&f.hostname, "hostname", "", "server address or ~/.ssh/config alias")
	}
	fs.IntVarP(&f.port, "port", "p", 0, "ssh port")
	fs.StringVarP(&f.identity, "identity", "i", "", "identity file")
	fs.StringVarP(&f.jump, "jump", "J", "", "jump host (ProxyJump)")
	fs.StringVar(&f.path, "path", "", "remote start directory")
	fs.StringVar(&f.extra, "extra-args", "", "extra ssh arguments")
	fs.StringVar(&f.notes, "notes", "", "free-form notes")
	fs.StringVarP(&f.env, "env", "e", "", "environment: production, staging, development, ...")
	fs.StringSliceVarP(&f.tags, "tag", "t", nil, "tag (repeatable or comma separated; replaces existing tags)")
}

// apply copies every flag the user set onto h.
func (f *hostFlags) apply(fs *pflag.FlagSet, h *store.Host) {
	set := func(name string, dst *string, v string) {
		if fs.Changed(name) {
			*dst = v
		}
	}
	set("name", &h.Name, f.name)
	set("user", &h.User, f.user)
	set("hostname", &h.Hostname, f.hostname)
	set("identity", &h.IdentityFile, f.identity)
	set("jump", &h.JumpHost, f.jump)
	set("path", &h.RemotePath, f.path)
	set("extra-args", &h.ExtraArgs, f.extra)
	set("notes", &h.Notes, f.notes)
	set("env", &h.Environment, f.env)
	if fs.Changed("port") {
		h.Port = f.port
	}
	if fs.Changed("tag") {
		h.Tags = store.NormalizeTags(f.tags)
	}
}

func anyChanged(fs *pflag.FlagSet) bool {
	changed := false
	fs.Visit(func(fl *pflag.Flag) {
		if fl.Name != "db" {
			changed = true
		}
	})
	return changed
}

func (a *app) nameTaken(cmd *cobra.Command, except int64) func(string) bool {
	return func(name string) bool {
		h, err := a.store.Get(cmd.Context(), name)
		return err == nil && h.ID != except
	}
}

func (a *app) addCmd() *cobra.Command {
	var f hostFlags
	cmd := &cobra.Command{
		Use:   "add [name] [destination]",
		Short: "Add a host (interactive form when no destination is given)",
		Example: `  rootnet add
  rootnet add appelenburg.nl web@nuthatch.sys.rootnet.io --path /var/www/site -t wordpress -e production`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var h store.Host
			if len(args) > 0 {
				h.Name = args[0]
			}
			if len(args) > 1 {
				h.Hostname = args[1]
				if user, host, ok := strings.Cut(args[1], "@"); ok {
					h.User, h.Hostname = user, host
				}
			}
			f.apply(cmd.Flags(), &h)
			if len(args) < 2 {
				var err error
				h, err = hostform.Run("Add host", h, a.nameTaken(cmd, 0))
				if errors.Is(err, hostform.ErrCancelled) {
					return nil
				}
				if err != nil {
					return err
				}
			}
			h, err := a.store.Create(cmd.Context(), h)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Added %s (%s)\n", h.Name, h.Target())
			if reserved(h.Name) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: %q is also a subcommand; connect with \"rootnet ssh %s\".\n", h.Name, h.Name)
			}
			return nil
		},
	}
	f.register(cmd.Flags(), false)
	return cmd
}

func (a *app) editCmd() *cobra.Command {
	var f hostFlags
	cmd := &cobra.Command{
		Use:               "edit <query>",
		Short:             "Edit a host (interactive form unless field flags are given)",
		Example:           "  rootnet edit appel\n  rootnet edit appel --port 2222 --tag wordpress,prod",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := a.resolve(cmd.Context(), strings.Join(args, " "))
			if errors.Is(err, picker.ErrCancelled) {
				return nil
			}
			if err != nil {
				return err
			}
			if anyChanged(cmd.Flags()) {
				f.apply(cmd.Flags(), &h)
			} else {
				h, err = hostform.Run("Edit "+h.Name, h, a.nameTaken(cmd, h.ID))
				if errors.Is(err, hostform.ErrCancelled) {
					return nil
				}
				if err != nil {
					return err
				}
			}
			if h, err = a.store.Update(cmd.Context(), h); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Saved %s (%s)\n", h.Name, h.Target())
			return nil
		},
	}
	f.register(cmd.Flags(), true)
	return cmd
}

func (a *app) rmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:               "rm <query>",
		Aliases:           []string{"remove", "delete"},
		Short:             "Remove a host",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := a.resolve(cmd.Context(), strings.Join(args, " "))
			if errors.Is(err, picker.ErrCancelled) {
				return nil
			}
			if err != nil {
				return err
			}
			if !yes {
				if !interactive() {
					return errors.New("refusing to delete without a terminal; pass --yes")
				}
				title := fmt.Sprintf("Delete %s (%s)?", h.Name, h.Target())
				if theme.IsProduction(h.Environment) {
					title = "⚠ PRODUCTION · " + title
				}
				confirm := false
				err := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).
					Affirmative("Delete").Negative("Cancel").Value(&confirm))).
					WithOutput(os.Stderr).Run()
				if err != nil && !errors.Is(err, huh.ErrUserAborted) {
					return err
				}
				if !confirm {
					return nil
				}
			}
			if err := a.store.Delete(cmd.Context(), h.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Removed %s\n", h.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return cmd
}

func (a *app) completeTags(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if err := a.open(cmd.ErrOrStderr()); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	tags, err := a.store.Tags(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return tags, cobra.ShellCompDirectiveNoFileComp
}
