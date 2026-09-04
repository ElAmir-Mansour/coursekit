package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

func newExportCmd() *cobra.Command {
	var (
		outPath string
		full    bool
	)

	cmd := &cobra.Command{
		Use:   "export [path]",
		Short: "Write a course report to a file",
		Long: `export writes a course summary in a format chosen from the output file's
extension: .md for Markdown, .csv for a spreadsheet, .json for other tools.

This is the same data as 'scan', in a form you can hand to somebody else.`,
		Example: `  coursekit export . -o course.md
  coursekit export . -o lessons.csv
  coursekit export ~/Desktop/"Go Backend Course" -o report.json --full`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(args)
			if err != nil {
				return err
			}
			if outPath == "" {
				return fmt.Errorf("export needs an output file: pass -o report.md")
			}

			ext := strings.ToLower(filepath.Ext(outPath))
			switch ext {
			case ".md", ".markdown", ".csv", ".json":
			default:
				return fmt.Errorf("cannot tell the format from %q: use .md, .csv or .json", filepath.Base(outPath))
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

			switch ext {
			case ".csv":
				err = export.CSV(out, res.Course)
			case ".json":
				err = export.JSON(out, res.Course)
			default:
				err = export.Markdown(out, res.Course)
			}
			if err != nil {
				return err
			}

			fmt.Printf("  Wrote %s (%d lessons, %s)\n",
				outPath, res.Course.LessonCount(), res.Course.Duration().Round(1e9))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&outPath, "output", "o", "", "output file (.md, .csv or .json)")
	f.BoolVar(&full, "full", false, "include codec and audio detail, read with ffprobe")

	return cmd
}
