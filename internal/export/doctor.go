package export

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ElAmir-Mansour/coursekit/internal/lint"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// DoctorOptions controls how a lint report is rendered.
type DoctorOptions struct {
	Palette Palette
	Width   int
	// MaxFiles caps how many affected files are listed under each finding.
	// Zero uses a sensible default; a negative value lists all of them.
	MaxFiles int
}

const defaultMaxFiles = 6

// Doctor writes a lint report for a terminal.
//
// Findings are already aggregated by rule and sorted worst-first, so this
// leads with whatever actually blocks an upload.
func Doctor(w io.Writer, c *model.Course, rep *lint.Report, opts DoctorOptions) error {
	p := opts.Palette
	total := opts.Width
	if total <= 0 {
		total = 100
	}
	maxFiles := opts.MaxFiles
	if maxFiles == 0 {
		maxFiles = defaultMaxFiles
	}

	bw := &errWriter{w: w}

	bw.printf("%s%s%s %s— checked against %s%s%s\n",
		p.Bold, c.Title, p.Reset, p.Dim, p.Reset+p.Bold, rep.Profile, p.Reset)
	if rep.Description != "" {
		bw.printf("%s%s%s\n", p.Dim, rep.Description, p.Reset)
	}
	bw.printf("\n")

	if len(rep.Findings) == 0 {
		bw.printf("  %s✔%s Nothing to fix. %d file%s checked against %s.\n\n",
			p.Green, p.Reset, rep.FilesChecked, plural(rep.FilesChecked), rep.Profile)
		return bw.err
	}

	for _, f := range rep.Findings {
		glyph, label, color := severityStyle(f.Severity, p)

		count := fmt.Sprintf("%d file%s", f.Count(), plural(f.Count()))
		head := fmt.Sprintf("  %s%s %s%s  %s", color, glyph, pad(label, 6), p.Reset, f.Title)
		bw.printf("%s%s%s%s%s\n",
			head,
			strings.Repeat(" ", maxInt(1, total-width(stripANSI(head))-width(count)-2)),
			p.Dim, count, p.Reset)

		if f.Detail != "" {
			for _, line := range wrapText(f.Detail, total-8) {
				bw.printf("      %s%s%s\n", p.Dim, line, p.Reset)
			}
		}

		shown := f.Files
		hidden := 0
		if maxFiles > 0 && len(shown) > maxFiles {
			hidden = len(shown) - maxFiles
			shown = shown[:maxFiles]
		}
		for _, fn := range shown {
			note := ""
			if fn.Note != "" {
				note = fmt.Sprintf("  %s%s%s", p.Cyan, fn.Note, p.Reset)
			}
			bw.printf("      %s·%s %s%s\n", p.Dim, p.Reset, truncate(fn.Rel, total-24), note)
		}
		if hidden > 0 {
			bw.printf("      %s· and %d more (use --verbose to list them)%s\n", p.Dim, hidden, p.Reset)
		}

		if f.Fix != "" {
			bw.printf("      %sfix%s %s\n", p.Green, p.Reset, f.Fix)
		}
		bw.printf("\n")
	}

	errs, warns, infos := rep.Counts()
	parts := []string{}
	if errs > 0 {
		parts = append(parts, fmt.Sprintf("%s%d error%s%s", p.Red, errs, plural(errs), p.Reset))
	}
	if warns > 0 {
		parts = append(parts, fmt.Sprintf("%s%d warning%s%s", p.Yellow, warns, plural(warns), p.Reset))
	}
	if infos > 0 {
		parts = append(parts, fmt.Sprintf("%s%d note%s%s", p.Dim, infos, plural(infos), p.Reset))
	}

	bw.printf("  %s\n", strings.Join(parts, p.Dim+" · "+p.Reset))
	bw.printf("  %s%d file%s checked", p.Dim, rep.FilesChecked, plural(rep.FilesChecked))
	if rep.LoudnessKnown > 0 {
		bw.printf(", %d with loudness measured", rep.LoudnessKnown)
	} else {
		bw.printf("; loudness not measured (add --loudness)")
	}
	bw.printf("%s\n", p.Reset)

	if rep.Reference != "" {
		bw.printf("  %s%s%s\n", p.Dim, rep.Reference, p.Reset)
	}

	return bw.err
}

// DoctorJSON writes a lint report as JSON for scripts and CI.
func DoctorJSON(w io.Writer, rep *lint.Report) error {
	type jsonFinding struct {
		lint.Finding
		SeverityName string `json:"severity_name"`
		FileCount    int    `json:"file_count"`
	}
	type out struct {
		Profile   string        `json:"profile"`
		Reference string        `json:"reference,omitempty"`
		OK        bool          `json:"ok"`
		Errors    int           `json:"errors"`
		Warnings  int           `json:"warnings"`
		Notes     int           `json:"notes"`
		Checked   int           `json:"files_checked"`
		Findings  []jsonFinding `json:"findings"`
	}

	e, wn, i := rep.Counts()
	res := out{
		Profile:   rep.Profile,
		Reference: rep.Reference,
		OK:        rep.OK(),
		Errors:    e,
		Warnings:  wn,
		Notes:     i,
		Checked:   rep.FilesChecked,
	}
	for _, f := range rep.Findings {
		res.Findings = append(res.Findings, jsonFinding{
			Finding:      f,
			SeverityName: f.Severity.String(),
			FileCount:    f.Count(),
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// DoctorMarkdown writes a lint report as Markdown.
func DoctorMarkdown(w io.Writer, c *model.Course, rep *lint.Report) error {
	bw := &errWriter{w: w}

	bw.printf("# %s — %s check\n\n", c.Title, rep.Profile)
	if rep.Description != "" {
		bw.printf("%s\n\n", rep.Description)
	}

	e, wn, i := rep.Counts()
	bw.printf("**%d error(s), %d warning(s), %d note(s)** across %d file(s).\n\n",
		e, wn, i, rep.FilesChecked)

	if len(rep.Findings) == 0 {
		bw.printf("Nothing to fix.\n")
		return bw.err
	}

	for _, f := range rep.Findings {
		bw.printf("## %s %s\n\n", severityBadge(f.Severity), f.Title)
		if f.Detail != "" {
			bw.printf("%s\n\n", f.Detail)
		}
		bw.printf("Affects **%d file(s)**:\n\n", f.Count())
		for _, fn := range f.Files {
			if fn.Note != "" {
				bw.printf("- `%s` — %s\n", fn.Rel, fn.Note)
			} else {
				bw.printf("- `%s`\n", fn.Rel)
			}
		}
		if f.Fix != "" {
			bw.printf("\n```sh\n%s\n```\n", f.Fix)
		}
		if f.Docs != "" {
			bw.printf("\nReference: %s\n", f.Docs)
		}
		bw.printf("\n")
	}

	return bw.err
}

func severityStyle(s lint.Severity, p Palette) (glyph, label, color string) {
	switch s {
	case lint.SevError:
		return "✖", "ERROR", p.Red
	case lint.SevWarn:
		return "▲", "WARN", p.Yellow
	default:
		return "•", "NOTE", p.Blue
	}
}

func severityBadge(s lint.Severity) string {
	switch s {
	case lint.SevError:
		return "❌"
	case lint.SevWarn:
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// wrapText breaks a paragraph to a column width on word boundaries.
func wrapText(s string, limit int) []string {
	if limit < 20 {
		limit = 20
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}

	var (
		lines []string
		cur   strings.Builder
	)
	for _, word := range words {
		if cur.Len() > 0 && width(cur.String())+1+width(word) > limit {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// stripANSI removes escape sequences so a styled string can be measured for
// alignment.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
