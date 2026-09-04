package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/lint"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// Exit codes. These are part of the tool's contract with CI, so they are
// documented in the command help as well.
const (
	exitCourseHasErrors = 2
)

func newDoctorCmd() *cobra.Command {
	var (
		profileName  string
		loudness     bool
		asJSON, asMD bool
		outPath      string
		verbose      bool
		noFail       bool
	)

	cmd := &cobra.Command{
		Use:   "doctor [path]",
		Short: "Check a course against a platform's requirements",
		Long: `doctor reads full metadata for every lesson and checks it against a
publishing profile: shape, resolution, codec, frame rate, audio, container,
file size, name portability, and consistency across the course.

Nothing is modified. Each finding carries the ffmpeg command that would fix it,
for you to run yourself.

Loudness is measured only with --loudness, because it is the one genuinely slow
check: ffmpeg has to decode the audio of every file.

Exit codes: 0 clean, 1 the tool failed, 2 the course has errors.`,
		Example: `  coursekit doctor ~/Desktop/"Go Backend Course"
  coursekit doctor . --profile lms
  coursekit doctor . --loudness --profile youtube
  coursekit doctor . --json | jq '.findings[].rule'`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(args)
			if err != nil {
				return err
			}

			profile, err := lint.LoadProfile(profileName)
			if err != nil {
				return err
			}

			if outPath != "" && !asJSON && !asMD {
				switch strings.ToLower(filepath.Ext(outPath)) {
				case ".json":
					asJSON = true
				case ".md", ".markdown":
					asMD = true
				}
			}

			// doctor needs codec and audio detail, so it always reads full
			// metadata rather than the fast path.
			if err := scan.FFprobeAvailable(); err != nil {
				return fmt.Errorf("%w\n\ndoctor needs ffprobe to read codecs and audio detail.\nPlain scanning still works without it: try `coursekit scan`", err)
			}

			prog := newProgress("probing")
			res, err := scan.Scan(cmd.Context(), scanOptions(root, true, prog.update))
			prog.done()
			if err != nil {
				return err
			}

			if res.Course.LessonCount() == 0 {
				return reportEmpty(os.Stdout, res.Course, paletteFor(os.Stdout))
			}

			if loudness {
				lp := newProgress("measuring loudness")
				err := scan.MeasureCourseLoudness(cmd.Context(), res.Course, openCache(root), lp.update)
				lp.done()
				if err != nil && !errors.Is(err, scan.ErrNoAudio) {
					return err
				}
			}

			rep := lint.Check(res.Course, profile)

			out, closeOut, err := openOutput(outPath)
			if err != nil {
				return err
			}
			defer closeOut()

			maxFiles := 0
			if verbose {
				maxFiles = -1
			}

			switch {
			case asJSON:
				err = export.DoctorJSON(out, rep)
			case asMD:
				err = export.DoctorMarkdown(out, res.Course, rep)
			default:
				err = export.Doctor(out, res.Course, rep, export.DoctorOptions{
					Palette:  paletteFor(os.Stdout),
					Width:    export.TerminalWidth(os.Stdout),
					MaxFiles: maxFiles,
				})
			}
			if err != nil {
				return err
			}

			if !rep.OK() && !noFail {
				return withExitCode(exitCourseHasErrors, &quietError{})
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&profileName, "profile", "p", "udemy",
		"profile to check against: "+strings.Join(lint.BuiltinNames(), ", ")+", or a path to a YAML file")
	f.BoolVar(&loudness, "loudness", false, "measure EBU R128 loudness (slow: decodes the audio of every file)")
	f.BoolVar(&verbose, "verbose", false, "list every affected file instead of the first few")
	f.BoolVar(&noFail, "no-fail", false, "always exit 0, even when the course has errors")
	f.BoolVar(&asJSON, "json", false, "write JSON")
	f.BoolVar(&asMD, "md", false, "write Markdown")
	f.StringVarP(&outPath, "output", "o", "", "write to a file instead of stdout")

	return cmd
}
