package export

import (
	"fmt"
	"io"
	"strings"

	"github.com/dustin/go-humanize"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// TableOptions controls the terminal rendering of a course.
type TableOptions struct {
	Palette Palette
	// Width is the total terminal width available.
	Width int
	// Tree expands each chapter to show its lessons.
	Tree bool
	// ShowPath prints the absolute course root under the title.
	ShowPath bool
	// Stats appends the scan timing and how the metadata was obtained.
	Stats *ScanStats
}

// ScanStats is the optional performance footer, which exists so the speed
// difference between the in-process reader and ffprobe is visible rather than
// something the user has to take on faith.
type ScanStats struct {
	Took      string
	CacheHits int
	FastPath  int
	FFprobed  int
}

// layout is the table's column plan. It measures itself, so the rendering
// code and the width budget cannot drift apart — which is exactly how a table
// ends up one column too wide.
type layout struct {
	num      int
	name     int
	flag     int
	lessons  int
	duration int
	size     int

	showSize bool
	showDur  bool
}

// gap is the separator between every pair of columns.
const gap = 2

// total is the exact number of columns a rendered row occupies.
func (l layout) total() int {
	n := gap + l.num + gap + l.name + gap + l.flag + gap + l.lessons
	if l.showDur {
		n += gap + l.duration
	}
	if l.showSize {
		n += gap + l.size
	}
	return n
}

// newLayout fits the columns into the available width, dropping the least
// important ones rather than overflowing. A table that wraps is unreadable,
// so size goes first and then duration.
func newLayout(width int) layout {
	l := layout{
		num: 4, flag: 2, lessons: 8, duration: 9, size: 10,
		showSize: true, showDur: true,
	}

	fit := func() int { return width - (l.total() - l.name) }

	if l.name = fit(); l.name < 12 {
		l.showSize = false
		if l.name = fit(); l.name < 10 {
			l.showDur = false
			l.name = fit()
		}
	}
	if l.name < 6 {
		l.name = 6
	}
	return l
}

// row renders one line of the table from already-formatted cells.
func (l layout) row(num, name, flag, count, dur, size string, p Palette, flagColor string) string {
	flagReset := p.Reset
	if flagColor == "" {
		flagReset = ""
	}

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(padLeft(num, l.num))
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(pad(truncate(name, l.name), l.name))
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(flagColor)
	b.WriteString(pad(flag, l.flag))
	b.WriteString(flagReset)
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(padLeft(count, l.lessons))
	if l.showDur {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(padLeft(dur, l.duration))
	}
	if l.showSize {
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteString(padLeft(size, l.size))
	}
	return b.String()
}

// Table writes a course summary as an aligned table.
func Table(w io.Writer, c *model.Course, opts TableOptions) error {
	p := opts.Palette
	total := opts.Width
	if total <= 0 {
		total = 100
	}
	l := newLayout(total)

	bw := &errWriter{w: w}

	bw.printf("%s%s%s\n", p.Bold, truncate(c.Title, total), p.Reset)
	if opts.ShowPath {
		bw.printf("%s%s%s\n", p.Dim, truncate(c.Root, total), p.Reset)
	}
	bw.printf("\n")

	bw.printf("%s%s%s\n", p.Dim,
		l.row("#", "CHAPTER", "", "LESSONS", "DURATION", "SIZE", NoColor, ""), p.Reset)

	for _, ch := range c.Chapters {
		flag, flagColor := chapterFlag(ch, p)

		name := ch.Display
		if ch.IsRoot {
			name = "(course root)"
		}

		bw.printf("%s\n", l.row(
			chapterLabel(ch), name, flag,
			fmt.Sprint(len(ch.Lessons)),
			model.ShortDuration(ch.Duration()),
			humanize.IBytes(uint64(ch.Size())),
			p, flagColor,
		))

		if opts.Tree {
			writeLessons(bw, ch, opts, l)
		}
	}

	bw.printf("%s%s%s\n", p.Dim, strings.Repeat("─", l.total()), p.Reset)
	bw.printf("%s%s%s\n", p.Bold, l.row(
		"", fmt.Sprintf("%d chapters", countChapters(c)), "",
		fmt.Sprint(c.LessonCount()),
		model.ShortDuration(c.Duration()),
		humanize.IBytes(uint64(c.Size())),
		NoColor, "",
	), p.Reset)

	bw.printf("\n")
	writeFooterNotes(bw, c, opts, total)

	return bw.err
}

func writeLessons(bw *errWriter, ch *model.Chapter, opts TableOptions, l layout) {
	p := opts.Palette

	for _, f := range ch.Lessons {
		label := f.Lesson.Number.String()
		if label == "" {
			label = "·"
		}
		flag, flagColor := "", ""
		if f.ProbeErr != "" {
			flag, flagColor = "!", p.Red
		}

		bw.printf("%s%s%s\n", p.Dim, l.row(
			"", "  "+f.Name, flag, label,
			model.ShortDuration(f.Info.Duration),
			humanize.IBytes(uint64(f.Size)),
			p, flagColor,
		), p.Reset)
	}

	for _, f := range ch.Attachments {
		bw.printf("%s%s%s\n", p.Dim, l.row(
			"", "  "+f.Name, "", "file", "",
			humanize.IBytes(uint64(f.Size)),
			NoColor, "",
		), p.Reset)
	}
}

func writeFooterNotes(bw *errWriter, c *model.Course, opts TableOptions, width int) {
	p := opts.Palette
	var notes []string

	if n := c.AttachmentCount(); n > 0 {
		var bytes int64
		for _, ch := range c.Chapters {
			for _, f := range ch.Attachments {
				bytes += f.Size
			}
		}
		notes = append(notes, fmt.Sprintf("%d attachment%s (%s)",
			n, plural(n), humanize.IBytes(uint64(bytes))))
	}

	if s := opts.Stats; s != nil {
		notes = append(notes, "scanned in "+s.Took)
		var how []string
		if s.FastPath > 0 {
			how = append(how, fmt.Sprintf("%d read in-process", s.FastPath))
		}
		if s.FFprobed > 0 {
			how = append(how, fmt.Sprintf("%d via ffprobe", s.FFprobed))
		}
		if s.CacheHits > 0 {
			how = append(how, fmt.Sprintf("%d cached", s.CacheHits))
		}
		if len(how) > 0 {
			notes = append(notes, strings.Join(how, ", "))
		}
	}

	if len(notes) > 0 {
		for _, line := range wrapText(strings.Join(notes, " · "), width-2) {
			bw.printf("  %s%s%s\n", p.Dim, line, p.Reset)
		}
	}

	if errs := c.ProbeErrors(); len(errs) > 0 {
		bw.printf("\n  %s%d file%s could not be read:%s\n",
			p.Red, len(errs), plural(len(errs)), p.Reset)
		for _, f := range errs {
			bw.printf("    %s%s%s\n", p.Red, truncate(f.Rel, width-4), p.Reset)
			for _, line := range wrapText(f.ProbeErr, width-6) {
				bw.printf("      %s%s%s\n", p.Dim, line, p.Reset)
			}
		}
	}

	// Unnumbered chapters have no defined position, which is worth pointing
	// out because it is invisible in the table itself.
	var unnumbered []string
	for _, ch := range c.Chapters {
		if !ch.IsRoot && ch.Name.Confidence == model.ConfNone {
			unnumbered = append(unnumbered, ch.Display)
		}
	}
	if len(unnumbered) > 0 {
		msg := fmt.Sprintf("? %d folder%s have no chapter number: %s",
			len(unnumbered), plural(len(unnumbered)), strings.Join(unnumbered, ", "))
		for _, line := range wrapText(msg, width-2) {
			bw.printf("  %s%s%s\n", p.Dim, line, p.Reset)
		}
	}
}

// chapterLabel is the number column: a zero-padded chapter number, or a marker
// for the root and for folders with no number.
func chapterLabel(ch *model.Chapter) string {
	switch {
	case ch.IsRoot:
		return "·"
	case ch.Name.Confidence == model.ConfNone:
		return "--"
	default:
		return fmt.Sprint(ch.Name.Order)
	}
}

// chapterFlag marks folders whose name needs attention.
func chapterFlag(ch *model.Chapter, p Palette) (string, string) {
	switch {
	case ch.Name.Untidy():
		return "␣", p.Yellow
	case ch.Name.Misspelled():
		return "⚠", p.Yellow
	case !ch.IsRoot && ch.Name.Confidence == model.ConfNone:
		return "?", p.Dim
	default:
		return "", ""
	}
}

func countChapters(c *model.Course) int {
	n := 0
	for _, ch := range c.Chapters {
		if !ch.IsRoot {
			n++
		}
	}
	return n
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// errWriter collects the first write error so the render code does not need to
// check every single Fprintf.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
