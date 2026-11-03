package cli

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ProductionPanic/haven/v2/internal/match"
	"github.com/ProductionPanic/haven/v2/internal/store"
	"github.com/ProductionPanic/haven/v2/internal/tui/theme"
)

// serverInfo is what "haven servers" reports about a host: everything a
// script or LLM needs to pick a server and address its files, and nothing
// about keys or ssh arguments.
type serverInfo struct {
	Name       string     `json:"name"`
	Target     string     `json:"target"`
	Port       int        `json:"port"`
	Production bool       `json:"production"`
	Env        string     `json:"environment,omitempty"`
	Tags       []string   `json:"tags"`
	RemotePath string     `json:"remote_path,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	SSHOK      *bool      `json:"ssh_ok"` // result of the last "haven check"; null when never checked
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
	CheckError string     `json:"check_error,omitempty"`
	FilePrefix string     `json:"file_prefix"` // prefix for haven file / haven cp locations
}

func newServerInfo(h store.Host) serverInfo {
	port := h.Port
	if port == 0 {
		port = 22
	}
	s := serverInfo{
		Name:       h.Name,
		Target:     h.Target(),
		Port:       port,
		Production: theme.IsProduction(h.Environment),
		Env:        h.Environment,
		Tags:       h.Tags,
		RemotePath: h.RemotePath,
		Notes:      h.Notes,
		FilePrefix: h.Name + ":",
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	if h.Check != nil {
		ok, at := h.Check.OK, h.Check.At
		s.SSHOK, s.CheckedAt, s.CheckError = &ok, &at, h.Check.Error
	}
	return s
}

func (a *app) serversCmd() *cobra.Command {
	var tag string
	cmd := &cobra.Command{
		Use:   "servers [query]",
		Short: "List the servers haven can reach, as JSON (made for LLMs)",
		Long: `Print the hosts haven knows as a JSON array, with what a script or LLM
needs to use them: the name to put before a path ("file_prefix", for
"haven file ..." and "haven cp"), whether it is a production host, its
tags and notes, and the result of the last ssh check.

An optional query narrows the list the same way it does for "haven ssh". For
a table meant for people, use "haven ls".`,
		Example: `  haven servers
  haven servers --tag wordpress
  haven servers appel | jq '.[0].file_prefix'`,
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			hosts, err := a.store.List(cmd.Context(), tag)
			if err != nil {
				return err
			}
			if q := strings.Join(args, " "); q != "" {
				hosts = match.Find(hosts, q, time.Now())
			}
			out := make([]serverInfo, 0, len(hosts))
			for _, h := range hosts {
				out = append(out, newServerInfo(h))
			}
			return printJSON(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "only hosts with this tag")
	cmd.RegisterFlagCompletionFunc("tag", a.completeTags)
	return cmd
}
