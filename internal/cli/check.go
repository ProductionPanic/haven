package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ProductionPanic/haven/v2/internal/check"
	"github.com/ProductionPanic/haven/v2/internal/match"
	"github.com/ProductionPanic/haven/v2/internal/store"
)

func (a *app) runner() check.Runner {
	if a.checkRun != nil {
		return a.checkRun
	}
	return check.SSH
}

// checkHosts checks hosts over ssh and saves the results.
func (a *app) checkHosts(ctx context.Context, hosts []store.Host) ([]store.Check, error) {
	results := check.Hosts(ctx, a.runner(), hosts, time.Now)
	for i, h := range hosts {
		if err := a.store.SaveCheck(ctx, h.ID, results[i]); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// warnIfDown checks h after it was saved and warns on stderr when the
// login fails. The host stays saved either way.
func (a *app) warnIfDown(cmd *cobra.Command, h store.Host) error {
	results, err := a.checkHosts(cmd.Context(), []store.Host{h})
	if err != nil {
		return err
	}
	if c := results[0]; !c.OK {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", check.Summary(h, c))
	}
	return nil
}

func (a *app) checkCmd() *cobra.Command {
	var tag string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check [query]",
		Short: "Check that ssh logins still work",
		Long: `Log in to each host without a terminal (BatchMode, 5 second timeout) and
report whether it worked. Without a query every host is checked; with one,
every host that matches. Exits with status 1 when any check fails.`,
		Example: `  haven check
  haven check --tag wordpress
  haven check monotone --json`,
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			hosts, err := a.store.List(cmd.Context(), tag)
			if err != nil {
				return err
			}
			if query := strings.Join(args, " "); query != "" {
				hosts = match.Find(hosts, query, time.Now())
				if len(hosts) == 0 {
					return fmt.Errorf("no host matches %q", query)
				}
			}
			results, err := a.checkHosts(cmd.Context(), hosts)
			if err != nil {
				return err
			}
			failed := 0
			for i := range hosts {
				hosts[i].Check = &results[i]
				if !results[i].OK {
					failed++
				}
			}
			if err := printChecks(cmd.OutOrStdout(), hosts, asJSON); err != nil {
				return err
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d hosts failed the ssh check", failed, len(hosts))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "only hosts with this tag")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.RegisterFlagCompletionFunc("tag", a.completeTags)
	return cmd
}

func printChecks(out io.Writer, hosts []store.Host, asJSON bool) error {
	if asJSON {
		if hosts == nil {
			hosts = []store.Host{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(hosts)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTARGET\tSTATUS\tERROR")
	for _, h := range hosts {
		errText := ""
		if h.Check != nil {
			errText = h.Check.Error
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Name, h.Target(), check.Status(h.Check), dash(errText))
	}
	return tw.Flush()
}
