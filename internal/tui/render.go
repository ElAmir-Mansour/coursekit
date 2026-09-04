package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/mattn/go-runewidth"

	"github.com/ElAmir-Mansour/coursekit/internal/lint"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
	"github.com/ElAmir-Mansour/coursekit/internal/rename"
)

func (m Model) render() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	var lines []string
	switch m.screen {
	case screenDoctor:
		lines = m.doctorLines()
	case screenRename:
		lines = m.renameLines()
	case screenHelp:
		lines = m.helpLines()
	default:
		lines = m.treeLines()
	}

	b.WriteString(m.window(lines))
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

// window slices the content to the visible region and pads it out, so the
// footer stays pinned to the bottom of the screen rather than drifting.
func (m Model) window(lines []string) string {
	h := m.bodyHeight()

	start := m.offset
	if start > len(lines) {
		start = len(lines)
	}
	end := start + h
	if end > len(lines) {
		end = len(lines)
	}

	visible := lines[start:end]
	out := strings.Join(visible, "\n")

	if pad := h - len(visible); pad > 0 {
		out += strings.Repeat("\n", pad)
	}
	return out
}

func (m Model) renderHeader() string {
	th := m.th

	if m.course == nil {
		title := th.Title.Render(" coursekit")
		return title + "\n" + th.Subtitle.Render(" "+m.cfg.Root) + "\n"
	}

	summary := fmt.Sprintf("%d lessons · %s · %s",
		m.course.LessonCount(),
		model.HumanDuration(m.course.Duration()),
		humanize.IBytes(uint64(m.course.Size())))

	left := th.Title.Render(" " + m.course.Title)
	right := th.Subtitle.Render(summary + " ")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	head := left + strings.Repeat(" ", gap) + right
	path := th.Subtitle.Render(" " + truncate(m.course.Root, m.width-2))

	return head + "\n" + path + "\n"
}

func (m Model) renderFooter() string {
	th := m.th
	var b strings.Builder

	// Progress or status line.
	switch {
	case m.filtering:
		b.WriteString(" " + m.filter.View())
	case m.confirming:
		b.WriteString(th.Warn.Render(fmt.Sprintf(
			" Apply %s? This changes files on disk.  [y/N]",
			renamePlanSummary(m.plan))))
	case m.busy != "":
		line := fmt.Sprintf(" %s %s", m.spin.View(), m.busy)
		if m.prog.Total > 0 && m.prog.Done < m.prog.Total {
			line += th.Faint.Render(fmt.Sprintf("  %d/%d  %s",
				m.prog.Done, m.prog.Total, truncate(m.prog.Current, 36)))
		}
		b.WriteString(line)
	case m.err != nil:
		b.WriteString(th.Error.Render(" " + truncate(m.err.Error(), m.width-2)))
	case m.status.text != "":
		style := th.Faint
		switch m.status.kind {
		case statusGood:
			style = th.StatusOK
		case statusBad:
			style = th.StatusNo
		}
		b.WriteString(style.Render(" " + truncate(m.status.text, m.width-2)))
	default:
		b.WriteString(" ")
	}
	b.WriteString("\n")

	b.WriteString(th.Faint.Render(" " + m.hints()))
	return b.String()
}

// hints is the key legend. It changes with the screen so it only advertises
// keys that currently do something, and it drops entries from the end until it
// fits the terminal: a legend that wraps pushes the layout off the screen.
func (m Model) hints() string {
	var items []string

	switch m.screen {
	case screenHelp:
		items = []string{"esc back"}
	case screenDoctor:
		items = []string{"↑↓ scroll", "p profile", "l loudness", "esc back"}
	case screenRename:
		if m.plan != nil && !m.plan.Empty() && !rename.HasFatal(m.problems) {
			items = []string{"c commit", "esc back"}
		} else {
			items = []string{"esc back"}
		}
	default:
		items = []string{
			"↑↓ move", "⏎ expand", "a all", "d doctor",
			"r rename", "s sort", "/ filter",
		}
	}

	// These two are always worth the space: one explains everything else, the
	// other is how you leave.
	tail := "? help · q quit"
	budget := m.width - 2 - runewidth.StringWidth(tail)

	var kept []string
	used := 0
	for _, it := range items {
		cost := runewidth.StringWidth(it) + 3 // the " · " separator
		if used+cost > budget {
			break
		}
		kept = append(kept, it)
		used += cost
	}

	kept = append(kept, tail)
	return strings.Join(kept, " · ")
}

// ---------- tree ----------

func (m Model) treeLines() []string {
	th := m.th
	if m.course == nil {
		if m.err != nil {
			return []string{th.Error.Render("  " + m.err.Error())}
		}
		return []string{th.Faint.Render("  reading…")}
	}
	if len(m.rows) == 0 {
		if m.filter.Value() != "" {
			return []string{th.Faint.Render("  nothing matches " + m.filter.Value())}
		}
		return []string{th.Faint.Render("  no video or audio files found here")}
	}

	// Column widths, derived from the terminal so the table never wraps.
	const numW, flagW, cntW, durW, sizeW = 3, 2, 5, 8, 10
	nameW := m.width - (numW + flagW + cntW + durW + sizeW + 12)
	if nameW < 14 {
		nameW = 14
	}

	out := make([]string, 0, len(m.rows))
	for i, row := range m.rows {
		var line string
		switch row.kind {
		case rowChapter:
			line = m.chapterLine(row.chapter, numW, nameW, flagW, cntW, durW, sizeW)
		case rowLesson:
			line = m.lessonLine(row.file, numW, nameW, flagW, cntW, durW, sizeW)
		default:
			line = m.attachmentLine(row.file, numW, nameW, flagW, cntW, durW, sizeW)
		}

		if i == m.cursor {
			line = th.Selected.Render(runewidth.Truncate(padRight(stripStyles(line), m.width-1), m.width-1, ""))
		}
		out = append(out, line)
	}
	return out
}

func (m Model) chapterLine(ch *model.Chapter, numW, nameW, flagW, cntW, durW, sizeW int) string {
	th := m.th

	marker := "▸"
	if m.expanded[ch.Dir] || m.filter.Value() != "" {
		marker = "▾"
	}
	if len(ch.Lessons) == 0 && len(ch.Attachments) == 0 {
		marker = " "
	}

	var num string
	switch {
	case ch.IsRoot:
		num = "  ·"
	case ch.Name.Confidence == model.ConfNone:
		num = " --"
	default:
		num = fmt.Sprintf("%3d", ch.Name.Order)
	}

	flag, flagStyle := "", th.Faint
	switch {
	case ch.Name.Untidy():
		flag, flagStyle = "␣", th.Warn
	case ch.Name.Misspelled():
		flag, flagStyle = "⚠", th.Warn
	case !ch.IsRoot && ch.Name.Confidence == model.ConfNone:
		flag, flagStyle = "?", th.Faint
	}

	name := ch.Display
	if ch.IsRoot {
		name = "(course root)"
	}

	return fmt.Sprintf(" %s %s  %s %s  %s  %s  %s",
		marker,
		th.Number.Render(num),
		padRight(truncate(name, nameW), nameW),
		flagStyle.Render(padRight(flag, flagW)),
		padLeft(fmt.Sprint(len(ch.Lessons)), cntW),
		padLeft(model.ShortDuration(ch.Duration()), durW),
		th.Faint.Render(padLeft(humanize.IBytes(uint64(ch.Size())), sizeW)),
	)
}

func (m Model) lessonLine(f *model.MediaFile, numW, nameW, flagW, cntW, durW, sizeW int) string {
	th := m.th

	label := f.Lesson.Number.String()
	if label == "" {
		label = "·"
	}

	flag, flagStyle := "", th.Faint
	if f.ProbeErr != "" {
		flag, flagStyle = "!", th.Error
	} else if f.Info.Loudness != nil {
		flag, flagStyle = "♪", th.Faint
	}

	return fmt.Sprintf("   %s  %s %s  %s  %s  %s",
		th.Faint.Render(padRight("", numW)),
		th.RowDim.Render(padRight("  "+truncate(f.Name, nameW-2), nameW)),
		flagStyle.Render(padRight(flag, flagW)),
		th.Faint.Render(padLeft(label, cntW)),
		padLeft(model.ShortDuration(f.Info.Duration), durW),
		th.Faint.Render(padLeft(humanize.IBytes(uint64(f.Size)), sizeW)),
	)
}

func (m Model) attachmentLine(f *model.MediaFile, numW, nameW, flagW, cntW, durW, sizeW int) string {
	th := m.th
	return fmt.Sprintf("   %s  %s %s  %s  %s  %s",
		th.Faint.Render(padRight("", numW)),
		th.Faint.Render(padRight("  "+truncate(f.Name, nameW-2), nameW)),
		padRight("", flagW),
		th.Faint.Render(padLeft("file", cntW)),
		padLeft("", durW),
		th.Faint.Render(padLeft(humanize.IBytes(uint64(f.Size)), sizeW)),
	)
}

// ---------- doctor ----------

func (m Model) doctorLines() []string {
	th := m.th
	if m.report == nil {
		return []string{th.Faint.Render("  press d to check this course")}
	}

	rep := m.report
	var out []string

	add := func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format, args...))
	}

	e, w, i := rep.Counts()
	add("  %s  %s", th.Title.Render(rep.Profile), th.Faint.Render(rep.Description))
	add("  %s", th.Faint.Render(fmt.Sprintf(
		"%d error(s) · %d warning(s) · %d note(s) · %d files checked", e, w, i, rep.FilesChecked)))
	if rep.LoudnessKnown == 0 {
		add("  %s", th.Faint.Render("loudness not measured — press l"))
	} else {
		add("  %s", th.Faint.Render(fmt.Sprintf("loudness measured for %d file(s)", rep.LoudnessKnown)))
	}
	add("")

	if len(rep.Findings) == 0 {
		add("  %s  Nothing to fix.", th.Good.Render("✔"))
		return out
	}

	for _, f := range rep.Findings {
		glyph, style := m.severityStyle(f.Severity)
		add("  %s %s  %s",
			style.Render(glyph),
			th.Row.Render(truncate(f.Title, m.width-30)),
			th.Faint.Render(fmt.Sprintf("%d file(s)", f.Count())))

		if f.Detail != "" {
			for _, line := range wrap(f.Detail, m.width-8) {
				add("      %s", th.Faint.Render(line))
			}
		}

		limit := 5
		for idx, fn := range f.Files {
			if idx >= limit {
				add("      %s", th.Faint.Render(fmt.Sprintf("· and %d more", len(f.Files)-limit)))
				break
			}
			note := ""
			if fn.Note != "" {
				note = "  " + th.Accent.Render(fn.Note)
			}
			add("      %s %s%s", th.Faint.Render("·"), truncate(fn.Rel, m.width-32), note)
		}

		if f.Fix != "" {
			for _, line := range wrap(f.Fix, m.width-12) {
				add("      %s %s", th.Good.Render("fix"), th.RowDim.Render(line))
			}
		}
		add("")
	}

	return out
}

func (m Model) severityStyle(s lint.Severity) (string, lipgloss.Style) {
	switch s {
	case lint.SevError:
		return "✖", m.th.Error
	case lint.SevWarn:
		return "▲", m.th.Warn
	default:
		return "•", m.th.Accent
	}
}

// ---------- rename ----------

func (m Model) renameLines() []string {
	th := m.th
	if m.plan == nil {
		return []string{th.Faint.Render("  press r to build a rename plan")}
	}

	var out []string
	add := func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format, args...))
	}

	nameW := (m.width - 16) / 2
	if nameW < 16 {
		nameW = 16
	}

	if m.plan.Empty() {
		add("  %s", th.Good.Render("Every name already matches the template."))
	} else {
		add("  %s", th.Title.Render("Proposed renames"))
		add("")
		for _, op := range m.plan.Ops {
			kind := " "
			if op.IsDir {
				kind = "▸"
			}
			add("    %s %s  %s  %s",
				th.Faint.Render(kind),
				th.Del.Render(padRight(truncate(op.Base(), nameW), nameW)),
				th.Faint.Render("→"),
				th.Add.Render(truncate(op.NewBase(), nameW)))
		}
		add("")
	}

	if len(m.plan.Skips) > 0 {
		add("  %s", th.Title.Render("Left alone"))
		add("")
		for _, s := range m.plan.Skips {
			add("      %s  %s",
				th.RowDim.Render(padRight(truncate(s.Path, nameW), nameW)),
				th.Faint.Render(truncate(s.Reason, m.width-nameW-12)))
		}
		add("")
	}

	var fatal []rename.Problem
	for _, p := range m.problems {
		if p.Severity == rename.Fatal {
			fatal = append(fatal, p)
		}
	}
	if len(fatal) > 0 {
		add("  %s", th.Error.Render("Blocking problems"))
		add("")
		for _, p := range fatal {
			add("      %s %s", th.Error.Render("✖"), truncate(p.String(), m.width-10))
		}
		add("")
		add("  %s", th.Error.Render("This plan cannot be applied until these are resolved."))
	} else if !m.plan.Empty() {
		add("  %s", th.Warn.Render("Nothing has been changed yet. Press c to apply."))
	}

	return out
}

// ---------- help ----------

func (m Model) helpLines() []string {
	th := m.th

	section := func(title string) string { return "  " + th.Title.Render(title) }
	entry := func(k, desc string) string {
		return fmt.Sprintf("    %s  %s", th.Key.Render(padRight(k, 10)), th.Row.Render(desc))
	}

	return []string{
		section("Moving around"),
		entry("↑ ↓ j k", "move the cursor"),
		entry("pgup pgdn", "page up and down"),
		entry("g G", "jump to top or bottom"),
		entry("⏎ space", "expand or collapse a chapter"),
		entry("a", "expand or collapse everything"),
		entry("/", "filter by name"),
		entry("s", "change sort order: syllabus, duration, size, name"),
		"",
		section("Checking"),
		entry("d", "check against the current profile (reads full metadata)"),
		entry("p", "switch profile: udemy, youtube, lms, strict"),
		entry("l", "measure loudness (slow: decodes every file's audio)"),
		"",
		section("Changing names"),
		entry("r", "build a rename plan (a dry run)"),
		entry("c", "apply the plan, after confirming"),
		entry("u", "reverse the last applied rename"),
		"",
		section("Other"),
		entry("e", "write a Markdown report next to the course"),
		entry("R", "rescan from disk"),
		entry("esc", "back to the course list"),
		entry("q ctrl+c", "quit"),
		"",
		"  " + th.Faint.Render("Flags in the list: ⚠ misspelled chapter word · ␣ stray whitespace"),
		"  " + th.Faint.Render("                   ? no chapter number · ! unreadable · ♪ loudness known"),
	}
}

// ---------- text helpers ----------

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return runewidth.Truncate(s, n-1, "") + "…"
}

func padRight(s string, n int) string {
	if w := runewidth.StringWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func padLeft(s string, n int) string {
	if w := runewidth.StringWidth(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}

func wrap(s string, limit int) []string {
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
		if cur.Len() > 0 && runewidth.StringWidth(cur.String())+1+runewidth.StringWidth(word) > limit {
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

// stripStyles removes escape sequences so a line can be re-styled as a
// selected row without the old colours bleeding through.
func stripStyles(s string) string {
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

// renamePlanSummary describes a plan in one phrase for the confirmation
// prompt, so the user is told what they are agreeing to.
func renamePlanSummary(p *rename.Plan) string {
	if p == nil {
		return "nothing"
	}
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
		parts = append(parts, fmt.Sprintf("%d folder(s)", dirs))
	}
	if files > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s)", files))
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, " and ")
}

// lineWidth measures a rendered line in terminal columns, for layout tests.
func lineWidth(s string) int { return runewidth.StringWidth(stripStyles(s)) }
