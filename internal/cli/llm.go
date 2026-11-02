package cli

import (
	_ "embed"
	"io"

	"github.com/spf13/cobra"
)

//go:embed llm.md
var llmGuide string

func (a *app) llmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "llm",
		Short: "Print a guide on using haven, written for LLMs",
		Long: `Print documentation on how an LLM (or a script) should use haven: which
commands exist, how files on servers are addressed, which guards protect
production hosts, and the usual workflows.

Point your assistant at it, for example by adding "Run 'haven llm' to learn
how to use haven" to your CLAUDE.md or AGENTS.md.`,
		Args: cobra.NoArgs,
		// The guide is static text; don't touch the host database for it.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), llmGuide)
			return err
		},
	}
}
