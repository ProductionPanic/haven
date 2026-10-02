package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/files"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/picker"
)

func (a *app) filesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "files [query]",
		Aliases: []string{"fm"},
		Short:   "Two-pane file manager: local on the left, the host on the right",
		Long: `Browse a host's files next to your local ones and copy between them.
The remote side starts in the host's remote path; both sides remember
where you left off. Files are transferred over ssh with the same config
as connecting.`,
		ValidArgsFunction: a.completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !interactive() {
				return errors.New("the file manager needs a terminal")
			}
			ctx := cmd.Context()
			h, err := a.resolve(ctx, strings.Join(args, " "))
			if errors.Is(err, picker.ErrCancelled) {
				return nil
			}
			if err != nil {
				return err
			}
			_ = a.store.MarkUsed(ctx, h.ID)
			res, err := files.Run(ctx, tui.FilesConfig(ctx, a.store, h))
			if err != nil {
				return err
			}
			return a.store.SaveDirs(ctx, h.ID, res.LocalDir, res.RemoteDir)
		},
	}
}
