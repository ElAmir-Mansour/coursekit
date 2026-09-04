package tui

import (
	"sort"
	"strings"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// rowKind distinguishes what a line in the tree represents.
type rowKind int

const (
	rowChapter rowKind = iota
	rowLesson
	rowAttachment
)

// treeRow is one visible line of the course browser.
type treeRow struct {
	kind    rowKind
	chapter *model.Chapter
	file    *model.MediaFile
}

// rebuildRows flattens the course into the list of lines currently visible,
// applying the sort order and the filter.
func (m *Model) rebuildRows() {
	m.rows = nil
	if m.course == nil {
		return
	}

	chapters := m.sortedChapters()
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))

	for _, ch := range chapters {
		lessons, attachments, matched := filterChapter(ch, query)
		if query != "" && !matched {
			continue
		}

		m.rows = append(m.rows, treeRow{kind: rowChapter, chapter: ch})

		// A filtered view expands automatically: hiding the matches behind a
		// collapsed folder would defeat the point of searching.
		open := m.expanded[ch.Dir] || query != ""
		if !open {
			continue
		}
		for _, f := range lessons {
			m.rows = append(m.rows, treeRow{kind: rowLesson, chapter: ch, file: f})
		}
		for _, f := range attachments {
			m.rows = append(m.rows, treeRow{kind: rowAttachment, chapter: ch, file: f})
		}
	}

	m.clampCursor()
}

// sortedChapters returns the chapters in the currently selected order,
// without disturbing the course's own syllabus ordering.
func (m *Model) sortedChapters() []*model.Chapter {
	out := make([]*model.Chapter, len(m.course.Chapters))
	copy(out, m.course.Chapters)

	switch m.sort {
	case sortDuration:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Duration() > out[j].Duration()
		})
	case sortSize:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Size() > out[j].Size()
		})
	case sortName:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Display) < strings.ToLower(out[j].Display)
		})
	}
	return out
}

// filterChapter narrows a chapter to the entries matching a query. A chapter
// whose own name matches keeps all of its contents.
func filterChapter(ch *model.Chapter, query string) (lessons, attachments []*model.MediaFile, matched bool) {
	if query == "" {
		return ch.Lessons, ch.Attachments, true
	}

	if strings.Contains(strings.ToLower(ch.Display), query) {
		return ch.Lessons, ch.Attachments, true
	}

	for _, f := range ch.Lessons {
		if strings.Contains(strings.ToLower(f.Name), query) {
			lessons = append(lessons, f)
		}
	}
	for _, f := range ch.Attachments {
		if strings.Contains(strings.ToLower(f.Name), query) {
			attachments = append(attachments, f)
		}
	}
	return lessons, attachments, len(lessons)+len(attachments) > 0
}

// toggleExpand opens or closes the chapter under the cursor. With the cursor
// on a lesson, it closes the chapter that lesson belongs to, which is what
// people reach for when they want to collapse what they are looking at.
func (m Model) toggleExpand() Model {
	if len(m.rows) == 0 || m.cursor >= len(m.rows) {
		return m
	}

	row := m.rows[m.cursor]
	if row.chapter == nil {
		return m
	}

	if row.kind != rowChapter {
		m.expanded[row.chapter.Dir] = false
		m.rebuildRows()
		m.selectChapter(row.chapter)
		return m
	}

	m.expanded[row.chapter.Dir] = !m.expanded[row.chapter.Dir]
	m.rebuildRows()
	m.selectChapter(row.chapter)
	return m
}

// expandAll opens every chapter, or closes them all if they are already open.
func (m Model) expandAll() Model {
	if m.course == nil {
		return m
	}

	anyClosed := false
	for _, ch := range m.course.Chapters {
		if !m.expanded[ch.Dir] {
			anyClosed = true
			break
		}
	}

	for _, ch := range m.course.Chapters {
		m.expanded[ch.Dir] = anyClosed
	}
	m.rebuildRows()
	return m
}

// selectChapter puts the cursor back on a chapter after the row list changed
// underneath it, so expanding a folder does not lose the user's place.
func (m *Model) selectChapter(ch *model.Chapter) {
	for i, r := range m.rows {
		if r.kind == rowChapter && r.chapter == ch {
			m.cursor = i
			m.clampCursor()
			return
		}
	}
}

// contentLines is how many lines the current screen can scroll through.
func (m Model) contentLines() int {
	switch m.screen {
	case screenDoctor:
		return len(m.doctorLines())
	case screenRename:
		return len(m.renameLines())
	case screenHelp:
		return len(m.helpLines())
	default:
		return len(m.rows)
	}
}

// clampCursor keeps the cursor inside the content and scrolls the window to
// follow it.
func (m *Model) clampCursor() {
	n := m.contentLines()
	if n == 0 {
		m.cursor, m.offset = 0, 0
		return
	}

	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}

	h := m.bodyHeight()
	if h <= 0 {
		m.offset = 0
		return
	}

	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if max := n - h; m.offset > max {
		if max < 0 {
			max = 0
		}
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// bodyHeight is the number of content lines that fit between the header and
// the footer.
func (m Model) bodyHeight() int {
	const chrome = 7 // title, path, blank, column header, blank, status, help
	h := m.height - chrome
	if h < 3 {
		return 3
	}
	return h
}
