package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ElAmir-Mansour/coursekit/internal/coursefixture"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// newTestModel builds a model already holding a scanned fixture course, as if
// the opening scan had just finished.
func newTestModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	root := coursefixture.Build(t)
	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	m := New(context.Background(), Config{Root: root})

	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)

	next, _ = m.Update(scanDoneMsg{result: res})
	return next.(Model)
}

// press sends one key and returns the resulting model.
func press(t *testing.T, m Model, k string) Model {
	t.Helper()

	var msg tea.KeyPressMsg
	if code, named := keyCodeFor(k); named {
		msg = tea.KeyPressMsg{Code: code}
	} else {
		msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
	}

	next, _ := m.Update(msg)
	return next.(Model)
}

func keyCodeFor(name string) (rune, bool) {
	switch name {
	case "enter":
		return tea.KeyEnter, true
	case "esc":
		return tea.KeyEscape, true
	case "down":
		return tea.KeyDown, true
	case "up":
		return tea.KeyUp, true
	}
	return 0, false
}

func TestView_ShowsChaptersInSyllabusOrder(t *testing.T) {
	m := newTestModel(t)
	out := stripStyles(m.View().Content)

	// The list must read as a syllabus: the misspelled chapter 3 sits between
	// 2 and 5, not after them.
	order := []string{"Chap 1", "Chap 2 Middleware", "Chpater 3 Postgres", "Chapter 5 Deploys", "Scratch recordings"}
	last := -1
	for _, name := range order {
		idx := strings.Index(out, name)
		if idx < 0 {
			t.Fatalf("view does not mention %q\n%s", name, out)
		}
		if idx < last {
			t.Errorf("%q appears out of order", name)
		}
		last = idx
	}
}

func TestView_ShowsTotals(t *testing.T) {
	m := newTestModel(t)
	out := stripStyles(m.View().Content)

	if !strings.Contains(out, "9 lessons") {
		t.Errorf("header should state the lesson count\n%s", out)
	}
	if !strings.Contains(out, "Go Backend Course") {
		t.Error("header should name the course")
	}
}

// Flags are how a problem becomes visible without running doctor.
func TestView_FlagsProblemFolders(t *testing.T) {
	m := newTestModel(t)
	out := stripStyles(m.View().Content)

	for _, want := range []string{"⚠", "␣", "?"} {
		if !strings.Contains(out, want) {
			t.Errorf("view is missing the %q flag\n%s", want, out)
		}
	}
}

func TestExpand_RevealsLessons(t *testing.T) {
	m := newTestModel(t)

	if strings.Contains(stripStyles(m.View().Content), "goCourse-Intro chapter 1.mp4") {
		t.Fatal("lessons should be hidden until a chapter is expanded")
	}

	// The cursor starts on the course root, so move to Chap 1 first.
	m = press(t, m, "down")
	m = press(t, m, "enter")

	after := stripStyles(m.View().Content)
	if !strings.Contains(after, "goCourse-Intro chapter 1.mp4") {
		t.Errorf("expanding Chap 1 did not reveal its lessons\n%s", after)
	}

	m = press(t, m, "enter")
	if strings.Contains(stripStyles(m.View().Content), "goCourse-Intro chapter 1.mp4") {
		t.Error("collapsing did not hide the lessons again")
	}
}

func TestExpandAll(t *testing.T) {
	m := press(t, newTestModel(t), "a")
	out := stripStyles(m.View().Content)

	for _, want := range []string{
		"goCourse-Intro chapter 1.mp4",
		"goCourse-Postgres text to schema 3.1.mp4",
		"scratch clip.mp4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expand-all is missing %q", want)
		}
	}
}

func TestSort_CyclesAndReordersByDuration(t *testing.T) {
	m := press(t, newTestModel(t), "s")

	if m.sort != sortDuration {
		t.Fatalf("sort = %v, want duration", m.sort)
	}
	if !strings.Contains(m.status.text, "duration") {
		t.Errorf("status = %q, want it to mention the new order", m.status.text)
	}

	chapters := m.sortedChapters()
	for i := 1; i < len(chapters); i++ {
		if chapters[i-1].Duration() < chapters[i].Duration() {
			t.Errorf("chapter %d is shorter than the one after it", i-1)
		}
	}

	for i := 0; i < 3; i++ {
		m = press(t, m, "s")
	}
	if m.sort != sortSyllabus {
		t.Errorf("sort = %v after a full cycle, want syllabus", m.sort)
	}
}

func TestFilter_NarrowsAndAutoExpands(t *testing.T) {
	m := press(t, newTestModel(t), "/")
	if !m.filtering {
		t.Fatal("pressing / did not open the filter")
	}

	for _, r := range "postgres" {
		m = press(t, m, string(r))
	}

	out := stripStyles(m.View().Content)
	if !strings.Contains(out, "Chpater 3 Postgres") {
		t.Errorf("filter lost the matching chapter\n%s", out)
	}
	if strings.Contains(out, "Chap 2 Middleware") {
		t.Errorf("filter kept a non-matching chapter\n%s", out)
	}
	// Matches must be visible rather than hidden behind a collapsed folder.
	if !strings.Contains(out, "goCourse-Postgres text to schema 3.1.mp4") {
		t.Errorf("filtered view did not auto-expand the match\n%s", out)
	}

	m = press(t, m, "esc")
	if m.filtering {
		t.Error("escape did not close the filter")
	}
	if !strings.Contains(stripStyles(m.View().Content), "Chap 2 Middleware") {
		t.Error("clearing the filter did not restore the full list")
	}
}

func TestHelp_TogglesAndListsKeys(t *testing.T) {
	m := press(t, newTestModel(t), "?")
	if m.screen != screenHelp {
		t.Fatal("? did not open help")
	}

	out := stripStyles(m.View().Content)
	for _, want := range []string{"expand or collapse a chapter", "measure loudness", "reverse the last applied rename"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %q", want)
		}
	}

	if m2 := press(t, m, "esc"); m2.screen != screenTree {
		t.Error("escape did not leave help")
	}
}

func TestProfile_Cycles(t *testing.T) {
	m := newTestModel(t)
	for _, want := range []string{"youtube", "lms", "strict", "udemy"} {
		m = press(t, m, "p")
		if m.profile != want {
			t.Fatalf("profile = %q, want %q", m.profile, want)
		}
	}
}

// Applying a rename must always pass through a confirmation, so a stray
// keypress cannot modify someone's course.
func TestApply_RequiresConfirmation(t *testing.T) {
	m := newTestModel(t)

	next, _ := m.Update(buildPlan(m.course, m.cfg)())
	m = next.(Model)

	if m.screen != screenRename {
		t.Fatalf("screen = %v, want the rename view", m.screen)
	}
	if m.plan == nil || m.plan.Empty() {
		t.Fatal("expected a non-empty plan for the fixture course")
	}

	out := stripStyles(m.View().Content)
	if !strings.Contains(out, "Nothing has been changed yet") {
		t.Errorf("rename view must say nothing has changed yet\n%s", out)
	}
	if !strings.Contains(out, "03 - Postgres") {
		t.Errorf("rename view should show the proposed name\n%s", out)
	}

	m = press(t, m, "c")
	if !m.confirming {
		t.Fatal("c should ask for confirmation rather than applying immediately")
	}
	if !strings.Contains(stripStyles(m.View().Content), "[y/N]") {
		t.Error("confirmation prompt is not visible")
	}

	m = press(t, m, "n")
	if m.confirming {
		t.Error("n did not dismiss the confirmation")
	}
	if !strings.Contains(m.status.text, "cancelled") {
		t.Errorf("status = %q, want it to say the action was cancelled", m.status.text)
	}
}

func TestDoctorView_RendersFindings(t *testing.T) {
	m := newTestModel(t)

	msg := fullScanThenDoctor(context.Background(), m.cfg, "udemy")()
	dm, ok := msg.(doctorDoneMsg)
	if !ok {
		t.Fatalf("unexpected message %T", msg)
	}
	if dm.err != nil {
		t.Skipf("doctor needs ffprobe: %v", dm.err)
	}

	next, _ := m.Update(dm)
	m = next.(Model)

	if m.screen != screenDoctor {
		t.Fatalf("screen = %v, want the doctor view", m.screen)
	}
	out := stripStyles(m.View().Content)
	if !strings.Contains(out, "Aspect ratio must be 16:9") {
		t.Errorf("doctor view is missing the aspect finding\n%s", out)
	}
	if !strings.Contains(out, "loudness not measured") {
		t.Error("doctor view should say loudness has not been measured")
	}
}

// The view must never be wider than the terminal, or every line wraps and the
// layout collapses.
func TestView_RespectsWidth(t *testing.T) {
	for _, w := range []int{60, 80, 100, 200} {
		m := newTestModel(t)
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		m = next.(Model)
		m = press(t, m, "a")

		for i, line := range strings.Split(stripStyles(m.View().Content), "\n") {
			if got := lineWidth(line); got > w {
				t.Errorf("width %d: line %d is %d columns:\n%q", w, i, got, line)
			}
		}
	}
}

// A narrow or short terminal must not panic or slice out of range.
func TestView_TinyTerminal(t *testing.T) {
	m := newTestModel(t)
	for _, size := range [][2]int{{20, 5}, {1, 1}, {40, 3}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		_ = m.View().Content // must not panic
		m = press(t, m, "a")
		_ = m.View().Content
	}
}
