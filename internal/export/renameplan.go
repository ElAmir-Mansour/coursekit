package export

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ElAmir-Mansour/coursekit/internal/rename"
)

// PlanOptions controls how a rename plan is displayed.
type PlanOptions struct {
	Palette Palette
	Width   int
	// Committed changes the wording from what would happen to what did.
	Committed bool
}

// RenamePlan writes a plan as a before/after list.
//
// Folders are listed before files even though they are applied last, because
// that is the order a person reads a course in.
func RenamePlan(w io.Writer, p *rename.Plan, problems []rename.Problem, opts PlanOptions) error {
	pal := opts.Palette
	total := opts.Width
	if total <= 0 {
		total = 100
	}

	bw := &errWriter{w: w}

	var dirs, files []rename.Op
	for _, op := range p.Ops {
		if op.IsDir {
			dirs = append(dirs, op)
		} else {
			files = append(files, op)
		}
	}

	if len(p.Ops) == 0 {
		bw.printf("  %sNothing to rename: every name already matches the template.%s\n", pal.Dim, pal.Reset)
	}

	nameW := (total - 12) / 2
	if nameW < 18 {
		nameW = 18
	}

	section := func(title string, ops []rename.Op, group func(rename.Op) string) {
		if len(ops) == 0 {
			return
		}
		bw.printf("  %s%s%s\n", pal.Bold, title, pal.Reset)

		lastGroup := ""
		for _, op := range ops {
			if group != nil {
				if g := group(op); g != lastGroup {
					bw.printf("    %s%s%s\n", pal.Dim, g, pal.Reset)
					lastGroup = g
				}
			}
			bw.printf("      %s%s%s  %s→%s  %s%s%s\n",
				pal.Red, pad(truncate(op.Base(), nameW), nameW), pal.Reset,
				pal.Dim, pal.Reset,
				pal.Green, truncate(op.NewBase(), nameW), pal.Reset,
			)
		}
		bw.printf("\n")
	}

	section("Chapter folders", dirs, nil)
	section("Lesson files", files, func(op rename.Op) string {
		return filepath.Base(filepath.Dir(op.From)) + "/"
	})

	if len(p.Skips) > 0 {
		bw.printf("  %sLeft alone%s\n", pal.Bold, pal.Reset)
		for _, s := range p.Skips {
			bw.printf("      %s%s%s  %s%s%s\n",
				pal.Dim, pad(truncate(s.Path, nameW), nameW), pal.Reset,
				pal.Dim, s.Reason, pal.Reset)
		}
		bw.printf("\n")
	}

	var fatal, warn []rename.Problem
	for _, pr := range problems {
		if pr.Severity == rename.Fatal {
			fatal = append(fatal, pr)
		} else {
			warn = append(warn, pr)
		}
	}

	for _, group := range []struct {
		list  []rename.Problem
		label string
		color string
	}{
		{fatal, "Blocking problems", pal.Red},
		{warn, "Warnings", pal.Yellow},
	} {
		if len(group.list) == 0 {
			continue
		}
		bw.printf("  %s%s%s\n", group.color, group.label, pal.Reset)
		for _, pr := range group.list {
			bw.printf("      %s✖%s %s\n", group.color, pal.Reset, pr.String())
		}
		bw.printf("\n")
	}

	// The closing line is the most important text on the screen: it has to be
	// unmistakable whether anything has actually been changed yet.
	switch {
	case len(fatal) > 0:
		bw.printf("  %sNothing was changed.%s This plan cannot be applied until the problems above are resolved.\n",
			pal.Bold+pal.Red, pal.Reset)
	case opts.Committed:
		bw.printf("  %sApplied %d rename%s.%s Reverse it with %scoursekit undo%s\n",
			pal.Green, len(p.Ops), plural(len(p.Ops)), pal.Reset, pal.Bold, pal.Reset)
	case len(p.Ops) > 0:
		bw.printf("  %sDry run — nothing has been changed.%s Apply with %scoursekit rename --commit%s\n",
			pal.Bold, pal.Reset, pal.Bold, pal.Reset)
	}

	return bw.err
}

// PlanSummaryLine is a one-line description of a plan, for the TUI status bar.
func PlanSummaryLine(p *rename.Plan) string {
	var dirs, files int
	for _, op := range p.Ops {
		if op.IsDir {
			dirs++
		} else {
			files++
		}
	}
	var parts []string
	if dirs > 0 {
		parts = append(parts, fmt.Sprintf("%d folder%s", dirs, plural(dirs)))
	}
	if files > 0 {
		parts = append(parts, fmt.Sprintf("%d file%s", files, plural(files)))
	}
	if len(parts) == 0 {
		return "nothing to rename"
	}
	return strings.Join(parts, ", ")
}
