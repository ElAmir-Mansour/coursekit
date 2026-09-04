package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/rename"
)

func newUndoCmd() *cobra.Command {
	var list bool

	cmd := &cobra.Command{
		Use:   "undo [path]",
		Short: "Reverse the last committed rename",
		Long: `undo replays the journal written by 'coursekit rename --commit', in reverse.

The journal records the exact moves that were made rather than re-deriving them
from the templates, so an undo does not depend on the naming rules behaving
identically later.`,
		Example: `  coursekit undo ~/Desktop/"Go Backend Course"
  coursekit undo . --list`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(args)
			if err != nil {
				return err
			}
			pal := paletteFor(os.Stdout)

			if list {
				journals, err := rename.ListJournals(root)
				if err != nil {
					return err
				}
				if len(journals) == 0 {
					fmt.Printf("  %sNo renames recorded for this folder.%s\n", pal.Dim, pal.Reset)
					return nil
				}
				fmt.Printf("  %sRename history%s\n\n", pal.Bold, pal.Reset)
				for _, j := range journals {
					state := ""
					switch {
					case j.Undone:
						state = pal.Dim + " (undone)" + pal.Reset
					case j.Pending:
						state = pal.Red + " (incomplete: the process was interrupted)" + pal.Reset
					}
					fmt.Printf("    %s  %d rename(s)%s\n",
						j.Created.Local().Format("2006-01-02 15:04:05"), len(j.Ops), state)
				}
				return nil
			}

			journal, err := rename.LatestJournal(root)
			if err != nil {
				return err
			}

			if journal.Pending {
				fmt.Printf("  %sThis journal is marked incomplete, which means a rename was interrupted.%s\n",
					pal.Yellow, pal.Reset)
				fmt.Printf("  %sReversing it will restore whichever of its %d moves actually happened.%s\n\n",
					pal.Dim, len(journal.Ops), pal.Reset)
			}

			reverted, err := rename.Undo(journal)
			if reverted > 0 {
				fmt.Printf("  %sReversed %d rename(s).%s\n", pal.Green, reverted, pal.Reset)
			}
			if err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&list, "list", false, "show the rename history for this folder")
	return cmd
}
