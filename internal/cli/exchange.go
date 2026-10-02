package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/exchange"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/legacy"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/store"
)

func (a *app) importCmd() *cobra.Command {
	var overwrite bool
	var format string
	cmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Import hosts from TOML or the legacy rootnet_hosts.txt format",
		Long: `Import hosts from a file ("-" for stdin). The format is picked from the
extension (.toml → TOML, anything else → legacy "name | user@host" lines)
unless --format is given. Without a file, ~/rootnet_hosts.txt is used.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := legacy.DefaultPath()
			if len(args) == 1 {
				path = args[0]
			}
			var r io.Reader = cmd.InOrStdin()
			if path != "-" {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				r = f
			}
			if format == "" {
				format = "legacy"
				if path == "-" || strings.EqualFold(filepath.Ext(path), ".toml") {
					format = "toml"
				}
			}
			var hosts []store.Host
			var err error
			switch format {
			case "toml":
				hosts, err = exchange.ReadTOML(r)
			case "legacy", "txt":
				hosts, err = legacy.Parse(r)
			default:
				return fmt.Errorf("unknown format %q (want toml or legacy)", format)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			res := exchange.Import(cmd.Context(), a.store, hosts, overwrite)
			for _, e := range res.Errors {
				fmt.Fprintln(cmd.ErrOrStderr(), "  skipped", e)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Imported %d new, updated %d, skipped %d\n",
				res.Created, res.Updated, len(res.Errors))
			return nil
		},
	}
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "update hosts that already exist")
	cmd.Flags().StringVar(&format, "format", "", "input format: toml or legacy")
	return cmd
}

func (a *app) exportCmd() *cobra.Command {
	var format, output, tag string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export hosts as TOML or ssh_config",
		Example: `  rootnet export > hosts.toml
  rootnet export --format ssh-config -o ~/.ssh/config.d/rootnet`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			hosts, err := a.store.List(cmd.Context(), tag)
			if err != nil {
				return err
			}
			var w io.Writer = cmd.OutOrStdout()
			var f *os.File
			if output != "" && output != "-" {
				if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
					return err
				}
				// Write to a temp file and rename so a failed export never
				// leaves a truncated ssh config behind.
				if f, err = os.CreateTemp(filepath.Dir(output), ".rootnet-export-*"); err != nil {
					return err
				}
				defer os.Remove(f.Name())
				w = f
			}
			switch format {
			case "toml":
				err = exchange.WriteTOML(w, hosts)
			case "ssh-config", "ssh_config", "sshconfig":
				err = exchange.WriteSSHConfig(w, hosts)
			default:
				err = fmt.Errorf("unknown format %q (want toml or ssh-config)", format)
			}
			if f == nil {
				return err
			}
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err == nil {
				err = os.Rename(f.Name(), output)
			}
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %d hosts to %s\n", len(hosts), output)
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "toml", "output format: toml or ssh-config")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to file instead of stdout")
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "only hosts with this tag")
	cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]cobra.Completion{"toml", "ssh-config"}, cobra.ShellCompDirectiveNoFileComp))
	cmd.RegisterFlagCompletionFunc("tag", a.completeTags)
	return cmd
}
