package rename_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ElAmir-Mansour/coursekit/internal/coursefixture"
	"github.com/ElAmir-Mansour/coursekit/internal/rename"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

func scanFixture(t *testing.T) (string, *scan.Result) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := coursefixture.Build(t)
	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return root, res
}

func TestBuild_ChapterNames(t *testing.T) {
	_, res := scanFixture(t)

	plan, err := rename.Build(res.Course, rename.Options{
		ChapterTemplate: rename.DefaultChapterTemplate,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := map[string]string{}
	for _, op := range plan.Ops {
		got[op.Base()] = op.NewBase()
	}

	want := map[string]string{
		"Chap 1":             "01",
		"Chap 2 Middleware":  "02 - Middleware",
		"Chpater 3 Postgres": "03 - Postgres",
		"Chapter 5 Deploys ": "05 - Deploys",
	}
	for from, to := range want {
		if got[from] != to {
			t.Errorf("%q -> %q, want %q", from, got[from], to)
		}
	}

	// An unnumbered folder must be left alone and reported, not guessed at.
	if _, planned := got["Scratch recordings"]; planned {
		t.Error("Scratch recordings has no number and must not be renamed")
	}
	var skipped bool
	for _, s := range plan.Skips {
		if s.Path == "Scratch recordings" {
			skipped = true
			if !strings.Contains(s.Reason, "number") {
				t.Errorf("skip reason = %q, want it to explain the missing number", s.Reason)
			}
		}
	}
	if !skipped {
		t.Error("Scratch recordings was neither renamed nor reported as skipped")
	}
}

// Zero padding is the entire point: it is what makes chapter 3 sort before
// chapter 9 in Finder and in every uploader.
func TestBuild_ZeroPaddingFixesSortOrder(t *testing.T) {
	_, res := scanFixture(t)
	plan, err := rename.Build(res.Course, rename.Options{ChapterTemplate: rename.DefaultChapterTemplate})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var newNames []string
	for _, op := range plan.Ops {
		if op.IsDir {
			newNames = append(newNames, op.NewBase())
		}
	}
	sorted := append([]string(nil), newNames...)
	sort.Strings(sorted)

	// Sorting the new names alphabetically must give chapter order.
	for i, n := range sorted {
		want := []string{"01", "02", "03", "05"}[i]
		if !strings.HasPrefix(n, want) {
			t.Errorf("alphabetical position %d is %q, want it to start with %q", i, n, want)
		}
	}
}

func TestApplyUndo_RoundTrip(t *testing.T) {
	root, res := scanFixture(t)

	before := snapshot(t, root)

	plan, err := rename.Build(res.Course, rename.Options{ChapterTemplate: rename.DefaultChapterTemplate})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Empty() {
		t.Fatal("plan is empty")
	}

	journal, err := rename.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	after := snapshot(t, root)
	if equalStrings(before, after) {
		t.Fatal("apply changed nothing")
	}
	if _, err := os.Lstat(filepath.Join(root, "03 - Postgres")); err != nil {
		t.Errorf("expected the renamed chapter to exist: %v", err)
	}

	// No temporary names may survive a successful apply.
	for _, p := range after {
		if strings.Contains(p, ".coursekit-tmp-") {
			t.Errorf("temporary name left behind: %s", p)
		}
	}

	reverted, err := rename.Undo(journal)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if reverted != len(plan.Ops) {
		t.Errorf("reverted %d ops, want %d", reverted, len(plan.Ops))
	}

	restored := snapshot(t, root)
	if !equalStrings(before, restored) {
		t.Errorf("undo did not restore the tree exactly\nbefore:   %v\nrestored: %v", before, restored)
	}
}

// A single-phase rename loop cannot express a swap: renaming A to B while B
// still exists either fails or destroys B.
func TestApply_SwapCycle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	a := filepath.Join(dir, "A")
	b := filepath.Join(dir, "B")
	writeFile(t, a, "content-a")
	writeFile(t, b, "content-b")

	plan := &rename.Plan{
		Root: dir,
		Ops: []rename.Op{
			{From: a, To: b},
			{From: b, To: a},
		},
	}
	if problems := rename.Validate(plan); rename.HasFatal(problems) {
		t.Fatalf("swap plan rejected: %v", problems[0])
	}

	journal, err := rename.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := readFile(t, a); got != "content-b" {
		t.Errorf("A now holds %q, want %q", got, "content-b")
	}
	if got := readFile(t, b); got != "content-a" {
		t.Errorf("B now holds %q, want %q", got, "content-a")
	}

	if _, err := rename.Undo(journal); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := readFile(t, a); got != "content-a" {
		t.Errorf("after undo A holds %q, want %q", got, "content-a")
	}
}

// On APFS and NTFS "chap 1" and "Chap 1" are the same directory entry, so a
// naive existence check reads a case fix as a collision.
func TestApply_CaseOnlyRename(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	from := filepath.Join(dir, "chap 1")
	to := filepath.Join(dir, "Chap 1")
	if err := os.Mkdir(from, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	plan := &rename.Plan{Root: dir, Ops: []rename.Op{{From: from, To: to, IsDir: true}}}

	if problems := rename.Validate(plan); rename.HasFatal(problems) {
		t.Fatalf("case-only rename rejected: %v", problems[0])
	}
	if _, err := rename.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want 1", len(entries))
	}
	if entries[0].Name() != "Chap 1" {
		t.Errorf("name = %q, want %q", entries[0].Name(), "Chap 1")
	}
}

func TestValidate_RejectsCollisions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.mp4"), "a")
	writeFile(t, filepath.Join(dir, "b.mp4"), "b")
	writeFile(t, filepath.Join(dir, "taken.mp4"), "t")

	t.Run("target already exists", func(t *testing.T) {
		plan := &rename.Plan{Root: dir, Ops: []rename.Op{
			{From: filepath.Join(dir, "a.mp4"), To: filepath.Join(dir, "taken.mp4")},
		}}
		if !rename.HasFatal(rename.Validate(plan)) {
			t.Error("expected a fatal problem for an existing target")
		}
	})

	t.Run("two sources one target", func(t *testing.T) {
		plan := &rename.Plan{Root: dir, Ops: []rename.Op{
			{From: filepath.Join(dir, "a.mp4"), To: filepath.Join(dir, "merged.mp4")},
			{From: filepath.Join(dir, "b.mp4"), To: filepath.Join(dir, "merged.mp4")},
		}}
		problems := rename.Validate(plan)
		if !rename.HasFatal(problems) {
			t.Fatal("expected a fatal problem for a two-into-one rename")
		}
		var mentioned bool
		for _, p := range problems {
			if strings.Contains(p.Message, "merged.mp4") {
				mentioned = true
			}
		}
		if !mentioned {
			t.Errorf("problem should name the colliding target: %v", problems)
		}
	})

	t.Run("targets differ only in case", func(t *testing.T) {
		plan := &rename.Plan{Root: dir, Ops: []rename.Op{
			{From: filepath.Join(dir, "a.mp4"), To: filepath.Join(dir, "Lesson.mp4")},
			{From: filepath.Join(dir, "b.mp4"), To: filepath.Join(dir, "lesson.mp4")},
		}}
		if !rename.HasFatal(rename.Validate(plan)) {
			t.Error("expected a fatal problem for case-only colliding targets")
		}
	})

	t.Run("missing source", func(t *testing.T) {
		plan := &rename.Plan{Root: dir, Ops: []rename.Op{
			{From: filepath.Join(dir, "ghost.mp4"), To: filepath.Join(dir, "x.mp4")},
		}}
		if !rename.HasFatal(rename.Validate(plan)) {
			t.Error("expected a fatal problem for a missing source")
		}
	})

	t.Run("would move between directories", func(t *testing.T) {
		other := t.TempDir()
		plan := &rename.Plan{Root: dir, Ops: []rename.Op{
			{From: filepath.Join(dir, "a.mp4"), To: filepath.Join(other, "a.mp4")},
		}}
		if !rename.HasFatal(rename.Validate(plan)) {
			t.Error("expected a fatal problem for a cross-directory move")
		}
	})
}

// A failure part-way through must leave the course exactly as it was, not
// half-renamed.
func TestApply_RollsBackOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions behave differently on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions would not block a rename")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	root := t.TempDir()
	okDir := filepath.Join(root, "ok")
	badDir := filepath.Join(root, "bad")
	for _, d := range []string{okDir, badDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	writeFile(t, filepath.Join(okDir, "first.mp4"), "1")
	writeFile(t, filepath.Join(badDir, "second.mp4"), "2")

	plan := &rename.Plan{Root: root, Ops: []rename.Op{
		{From: filepath.Join(okDir, "first.mp4"), To: filepath.Join(okDir, "01 - first.mp4")},
		{From: filepath.Join(badDir, "second.mp4"), To: filepath.Join(badDir, "01 - second.mp4")},
	}}

	// Make the second directory unwritable so its rename fails after the
	// first directory has already been dealt with.
	if err := os.Chmod(badDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(badDir, 0o755) })

	if _, err := rename.Apply(plan); err == nil {
		t.Fatal("expected Apply to fail")
	}

	// The first rename must have been undone.
	if _, err := os.Lstat(filepath.Join(okDir, "first.mp4")); err != nil {
		t.Errorf("first.mp4 was not restored: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(okDir, "01 - first.mp4")); err == nil {
		t.Error("the partially applied rename was left in place")
	}

	// And no journal may be left claiming work that no longer exists.
	journals, err := rename.ListJournals(root)
	if err != nil {
		t.Fatalf("ListJournals: %v", err)
	}
	if len(journals) != 0 {
		t.Errorf("a rolled-back apply left %d journal(s) behind", len(journals))
	}
}

func TestBuild_LessonRenaming(t *testing.T) {
	_, res := scanFixture(t)

	plan, err := rename.Build(res.Course, rename.Options{
		LessonTemplate: rename.DefaultLessonTemplate,
		StripPrefix:    true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := map[string]string{}
	for _, op := range plan.Ops {
		got[op.Base()] = op.NewBase()
	}

	// The shared "goCourse-" tag is stripped, the "?" is removed for
	// portability, and the existing 1.1 numbering is preserved rather than
	// renumbered.
	if want, have := "1.01 - why REST is dead.mp4", got["goCourse-why REST is dead ? -1.1.mp4"]; have != want {
		t.Errorf("lesson rename = %q, want %q", have, want)
	}
	if want, have := "3.02 - POSTGRES to app.mp4", got["goCourse-POSTGRES to app - 3.2-.mp4"]; have != want {
		t.Errorf("lesson rename = %q, want %q", have, want)
	}

	for _, op := range plan.Ops {
		if strings.ContainsAny(op.NewBase(), `?:*|"<>`) {
			t.Errorf("new name still holds a reserved character: %q", op.NewBase())
		}
	}
}

// Contents must be renamed before the folder containing them, or the child
// paths stop resolving half-way through.
func TestBuild_OrdersChildrenBeforeParents(t *testing.T) {
	_, res := scanFixture(t)

	plan, err := rename.Build(res.Course, rename.Options{
		ChapterTemplate: rename.DefaultChapterTemplate,
		LessonTemplate:  rename.DefaultLessonTemplate,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rename.HasFatal(rename.Validate(plan)) {
		t.Fatalf("combined plan is invalid: %v", rename.Validate(plan))
	}

	seenDir := map[string]int{}
	for i, op := range plan.Ops {
		if op.IsDir {
			seenDir[op.From] = i
		}
	}
	for i, op := range plan.Ops {
		if op.IsDir {
			continue
		}
		for dir, dirIdx := range seenDir {
			if strings.HasPrefix(op.From, dir+string(filepath.Separator)) && dirIdx < i {
				t.Errorf("directory %q is renamed at %d, before its file %q at %d",
					filepath.Base(dir), dirIdx, filepath.Base(op.From), i)
			}
		}
	}
}

func TestApplyUndo_FullCourseIncludingLessons(t *testing.T) {
	root, res := scanFixture(t)
	before := snapshot(t, root)

	plan, err := rename.Build(res.Course, rename.Options{
		ChapterTemplate: rename.DefaultChapterTemplate,
		LessonTemplate:  rename.DefaultLessonTemplate,
		StripPrefix:     true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	journal, err := rename.Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if journal.Pending {
		t.Error("a committed journal must not still be marked pending")
	}

	if _, err := rename.Undo(journal); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if restored := snapshot(t, root); !equalStrings(before, restored) {
		t.Errorf("undo of a chapters-and-lessons rename was not exact\nbefore:   %v\nrestored: %v",
			before, restored)
	}
}

func TestLatestJournal(t *testing.T) {
	root, res := scanFixture(t)

	if _, err := rename.LatestJournal(root); err == nil {
		t.Error("expected an error when nothing has been renamed yet")
	}

	plan, err := rename.Build(res.Course, rename.Options{ChapterTemplate: rename.DefaultChapterTemplate})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := rename.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	j, err := rename.LatestJournal(root)
	if err != nil {
		t.Fatalf("LatestJournal: %v", err)
	}
	if len(j.Ops) != len(plan.Ops) {
		t.Errorf("journal holds %d ops, want %d", len(j.Ops), len(plan.Ops))
	}
	if j.Root != root {
		t.Errorf("journal root = %q, want %q", j.Root, root)
	}
}

// ---------- helpers ----------

// snapshot lists every path under root, relative and sorted, so two trees can
// be compared exactly.
func snapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
