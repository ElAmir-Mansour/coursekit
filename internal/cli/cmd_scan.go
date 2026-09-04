package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

func newScanCmd() *cobra.Command {
	var (
		asJSON, asCSV, asMD bool
		tree                bool
		full                bool
		outPath             string
	)

	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Count the lessons and total the runtime of a course folder",
		Long: `scan walks a course folder and reports how many lessons each chapter holds
and how long they run.

Duration for MP4 and MOV files is read in-process from the container header,
which is roughly 280 times faster than launching ffprobe for each file. Pass
--full to use ffprobe instead when codec and audio detail is wanted.`,
		Example: `  coursekit scan ~/Desktop/"Go Backend Course"
  coursekit scan . --tree
  coursekit scan . --json | jq '.duration_human'
  coursekit scan . -o course.md`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(args)
			if err != nil {
				return err
			}

			// An explicit output path implies a machine format, chosen from
			// the extension so the user does not have to say it twice.
			if outPath != "" && !asJSON && !asCSV && !asMD {
				switch strings.ToLower(filepath.Ext(outPath)) {
				case ".json":
					asJSON = true
				case ".csv":
					asCSV = true
				case ".md", ".markdown":
					asMD = true
				}
			}

			if n := countTrue(asJSON, asCSV, asMD); n > 1 {
				return fmt.Errorf("choose one output format, not several")
			}

			prog := newProgress("reading")
			res, err := scan.Scan(cmd.Context(), scanOptions(root, full, prog.update))
			prog.done()
			if err != nil {
				return err
			}

			out, closeOut, err := openOutput(outPath)
			if err != nil {
				return err
			}
			defer closeOut()

			switch {
			case asJSON:
				return export.JSON(out, res.Course)
			case asCSV:
				return export.CSV(out, res.Course)
			case asMD:
				return export.Markdown(out, res.Course)
			}

			if res.Course.LessonCount() == 0 {
				return reportEmpty(out, res.Course, paletteFor(os.Stdout))
			}

			return export.Table(out, res.Course, export.TableOptions{
				Palette:  paletteFor(os.Stdout),
				Width:    export.TerminalWidth(os.Stdout),
				Tree:     tree,
				ShowPath: true,
				Stats: &export.ScanStats{
					Took:      res.Took.Round(1e6).String(),
					CacheHits: res.CacheHits,
					FastPath:  res.FastPath,
					FFprobed:  res.FFprobed,
				},
			})
		},
	}

	f := cmd.Flags()
	f.BoolVar(&tree, "tree", false, "list every lesson under its chapter")
	f.BoolVar(&full, "full", false, "read complete metadata with ffprobe instead of the fast path")
	f.BoolVar(&asJSON, "json", false, "write JSON")
	f.BoolVar(&asCSV, "csv", false, "write CSV, one row per lesson")
	f.BoolVar(&asMD, "md", false, "write Markdown")
	f.StringVarP(&outPath, "output", "o", "", "write to a file instead of stdout (format inferred from the extension)")

	return cmd
}

// reportEmpty explains an empty result rather than printing a table of
// nothing, since the usual cause is pointing at the wrong folder.
func reportEmpty(w io.Writer, c *model.Course, p export.Palette) error {
	_, err := fmt.Fprintf(w,
		"%s%s%s\n%s%s%s\n\n"+
			"  No video or audio files found here.\n\n"+
			"  %scoursekit looks for playable media and ignores .DS_Store, AppleDouble\n"+
			"  sidecars and zero-byte files. Check that the path is the course folder\n"+
			"  itself, not the folder above it.%s\n",
		p.Bold, c.Title, p.Reset,
		p.Dim, c.Root, p.Reset,
		p.Dim, p.Reset)
	return err
}

// openOutput returns stdout, or a file when a path was given.
func openOutput(path string) (io.Writer, func(), error) {
	if path == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create %s: %w", path, err)
	}
	return f, func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "coursekit: closing %s: %v\n", path, err)
		}
	}, nil
}

func countTrue(vals ...bool) int {
	n := 0
	for _, v := range vals {
		if v {
			n++
		}
	}
	return n
}
