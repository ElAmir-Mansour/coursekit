package cli

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
	"github.com/ElAmir-Mansour/coursekit/internal/tui"
)

// runBrowse launches the interactive browser, or prints a table when there is
// no terminal to draw on.
//
// The fallback is not a nicety: without it, `coursekit > report.txt` or
// `coursekit | head` would emit cursor-control sequences into the pipe. A tool
// that only works when watched is not much use in a script.
func runBrowse(cmd *cobra.Command, args []string) error {
	root, err := resolveRoot(args)
	if err != nil {
		return err
	}

	if !export.IsTerminal(os.Stdout) || !export.IsTerminal(os.Stdin) {
		return browseFallback(cmd.Context(), root)
	}

	program := tea.NewProgram(
		tui.New(cmd.Context(), tui.Config{
			Root:    root,
			Workers: global.workers,
			NoCache: global.noCache,
		}),
		tea.WithContext(cmd.Context()),
	)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("interactive mode failed: %w", err)
	}
	return nil
}

// browseFallback prints what the browser would have shown.
func browseFallback(ctx context.Context, root string) error {
	prog := newProgress("reading")
	res, err := scan.Scan(ctx, scanOptions(root, false, prog.update))
	prog.done()
	if err != nil {
		return err
	}

	pal := paletteFor(os.Stdout)
	if res.Course.LessonCount() == 0 {
		return reportEmpty(os.Stdout, res.Course, pal)
	}

	return export.Table(os.Stdout, res.Course, export.TableOptions{
		Palette:  pal,
		Width:    export.TerminalWidth(os.Stdout),
		ShowPath: true,
		Stats: &export.ScanStats{
			Took:      res.Took.Round(1e6).String(),
			CacheHits: res.CacheHits,
			FastPath:  res.FastPath,
			FFprobed:  res.FFprobed,
		},
	})
}
