package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/rename"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

func newRenameCmd() *cobra.Command {
	var (
		commit      bool
		chapterTmpl string
		lessonTmpl  string
		stripPrefix bool
		yes         bool
		noChapters  bool
	)

	cmd := &cobra.Command{
		Use:   "rename [path]",
		Short: "Normalise chapter and lesson names (dry run by default)",
		Long: `rename fixes the names that break ordering: missing zero padding, misspelled
chapter words, and leading or trailing spaces.

It is a dry run unless --commit is given. A committed rename writes an undo
journal, so it can be reversed exactly with 'coursekit undo'.

Only chapter folders are renamed by default. Lesson files are left alone unless
--files is given, because folder ordering is the high-value, low-risk half of
the job.

Template placeholders:
  {n}       chapter number, or lesson number within its chapter
  {n:02}    the same, zero-padded to two digits
  {ch}      chapter number
  {lesson}  lesson number parsed from an existing "3.2" style name
  {i}       position of the lesson within its chapter
  {title}   the descriptive part of the existing name
  {ext}     file extension, including the dot

Folders with no detectable number are never renamed: there is no way to give
one a position without inventing it.`,
		Example: `  coursekit rename ~/Desktop/"Go Backend Course"
  coursekit rename . --commit
  coursekit rename . --files --strip-prefix
  coursekit rename . --template "{n:02}. {title}" --commit`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(args)
			if err != nil {
				return err
			}

			opts := rename.Options{
				ChapterTemplate: chapterTmpl,
				LessonTemplate:  lessonTmpl,
				StripPrefix:     stripPrefix,
			}
			if noChapters {
				opts.ChapterTemplate = ""
			}
			if opts.ChapterTemplate == "" && opts.LessonTemplate == "" {
				return fmt.Errorf("nothing to rename: --no-chapters was given without --files")
			}

			prog := newProgress("reading")
			res, err := scan.Scan(cmd.Context(), scanOptions(root, false, prog.update))
			prog.done()
			if err != nil {
				return err
			}

			plan, err := rename.Build(res.Course, opts)
			if err != nil {
				return err
			}

			problems := rename.Validate(plan)
			pal := paletteFor(os.Stdout)
			width := export.TerminalWidth(os.Stdout)

			// Always show the plan before doing anything.
			if err := export.RenamePlan(os.Stdout, plan, problems, export.PlanOptions{
				Palette: pal, Width: width,
			}); err != nil {
				return err
			}

			if !commit {
				return nil
			}
			if rename.HasFatal(problems) {
				return withExitCode(exitCourseHasErrors, &quietError{})
			}
			if plan.Empty() {
				return nil
			}

			if !yes {
				ok, err := confirm(fmt.Sprintf("Apply %s?", export.PlanSummaryLine(plan)))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Printf("\n  %sCancelled. Nothing was changed.%s\n", pal.Dim, pal.Reset)
					return nil
				}
			}

			journal, err := rename.Apply(plan)
			if err != nil {
				return err
			}

			fmt.Printf("\n")
			if err := export.RenamePlan(os.Stdout, plan, nil, export.PlanOptions{
				Palette: pal, Width: width, Committed: true,
			}); err != nil {
				return err
			}
			fmt.Printf("  %sjournal: %s%s\n", pal.Dim, journal.Path(), pal.Reset)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&commit, "commit", false, "actually apply the plan")
	f.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	f.StringVar(&chapterTmpl, "template", rename.DefaultChapterTemplate, "naming template for chapter folders")
	f.Lookup("template").NoOptDefVal = rename.DefaultChapterTemplate
	f.StringVar(&lessonTmpl, "files", "", "also rename lesson files, using this template")
	f.Lookup("files").NoOptDefVal = rename.DefaultLessonTemplate
	f.BoolVar(&stripPrefix, "strip-prefix", false, "remove a tag shared by every lesson filename, such as \"goCourse-\"")
	f.BoolVar(&noChapters, "no-chapters", false, "leave chapter folders alone")

	// --plan is the default, and exists so the safe behaviour can be stated
	// explicitly in a script.
	var planFlag bool
	f.BoolVar(&planFlag, "plan", false, "show the plan without applying it (the default)")

	return cmd
}

// confirm asks a yes/no question. When stdin is not a terminal there is nobody
// to answer, so the answer is no: a piped invocation must not silently rename
// somebody's course.
func confirm(question string) (bool, error) {
	if !export.IsTerminal(os.Stdin) {
		return false, fmt.Errorf("refusing to rename without confirmation: stdin is not a terminal (pass --yes to proceed)")
	}

	fmt.Printf("\n  %s [y/N] ", question)

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
