// Package cli wires coursekit's command tree.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// globalFlags are shared by every subcommand.
type globalFlags struct {
	noColor bool
	color   bool
	noCache bool
	workers int
}

var global globalFlags

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	root := newRootCmd()

	// Signals get their own context so a long scan or loudness pass can be
	// interrupted cleanly rather than leaving orphaned ffmpeg processes.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		// Some commands have already produced better output than a one-line
		// error would be; those return a quiet error carrying only a code.
		// Cobra prints usage errors itself. Everything else is ours to report.
		if !isQuiet(err) && !isUsageError(err) {
			fmt.Fprintf(os.Stderr, "coursekit: %v\n", err)
		}
		return exitCodeFor(err)
	}
	return 0
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coursekit [path]",
		Short: "Inspect, check and tidy folders of course recordings",
		Long: `coursekit answers the questions that come up when a course lives in a
folder of screen recordings: how many lessons are there, how long is the whole
thing, will it pass the platform's requirements, and why is chapter 3 sorted
after chapter 9.

Run with no arguments to browse the current folder interactively.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowse(cmd, args)
		},
	}

	pf := cmd.PersistentFlags()
	pf.BoolVar(&global.noColor, "no-color", false, "disable coloured output")
	pf.BoolVar(&global.color, "color", false, "force coloured output even when piped")
	pf.BoolVar(&global.noCache, "no-cache", false, "ignore and do not update the metadata cache")
	pf.IntVar(&global.workers, "workers", 0, "probe concurrency (default: one per CPU)")

	cmd.AddCommand(
		newScanCmd(),
		newDoctorCmd(),
		newRenameCmd(),
		newUndoCmd(),
		newExportCmd(),
		newProfilesCmd(),
		newVersionCmd(),
	)

	return cmd
}

// resolveRoot turns an optional positional argument into a directory to scan.
func resolveRoot(args []string) (string, error) {
	path := "."
	if len(args) > 0 && args[0] != "" {
		path = args[0]
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is a file; coursekit works on a course folder", path)
	}
	return path, nil
}

// paletteFor picks the palette for a stream, honouring the global flags.
func paletteFor(f *os.File) export.Palette {
	if global.noColor {
		return export.NoColor
	}
	return export.PaletteFor(f, global.color)
}

// openCache returns the cache to use, or a no-op cache when disabled.
func openCache(root string) *scan.Cache {
	if global.noCache {
		return scan.NoCache()
	}
	return scan.OpenCache(root)
}

// scanOptions builds the shared scan configuration.
func scanOptions(root string, full bool, progress func(scan.Progress)) scan.Options {
	return scan.Options{
		Root:     root,
		Full:     full,
		Workers:  global.workers,
		Cache:    openCache(root),
		Progress: progress,
	}
}
