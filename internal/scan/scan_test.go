package scan_test

import (
	"context"
	"testing"
	"time"

	"github.com/ElAmir-Mansour/coursekit/internal/coursefixture"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

func TestScan_FixtureCourse(t *testing.T) {
	root := coursefixture.Build(t)

	res, err := scan.Scan(context.Background(), scan.Options{
		Root:  root,
		Cache: scan.NoCache(),
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	c := res.Course

	if got, want := c.LessonCount(), len(coursefixture.Layout); got != want {
		t.Errorf("LessonCount = %d, want %d", got, want)
	}

	// The PDF is carried along, not timed.
	if got := c.AttachmentCount(); got != 1 {
		t.Errorf("AttachmentCount = %d, want 1", got)
	}

	// Durations come from a real container read, so allow a frame of slack
	// rather than demanding exact equality.
	want := time.Duration(coursefixture.TotalSeconds()) * time.Second
	if d := c.Duration(); d < want-500*time.Millisecond || d > want+500*time.Millisecond {
		t.Errorf("Duration = %v, want about %v", d, want)
	}

	for _, f := range c.ProbeErrors() {
		t.Errorf("unexpected probe error for %s: %s", f.Rel, f.ProbeErr)
	}
}

// The ordering assertion is the point of the whole parser: a lexical sort puts
// "Chpater 3 Postgres" after "Chapter 5", and "Chap 2" before "Chap 1" is never
// right either.
func TestScan_ChapterOrdering(t *testing.T) {
	root := coursefixture.Build(t)

	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var got []string
	for _, ch := range res.Course.Chapters {
		got = append(got, ch.Display)
	}

	want := []string{
		"(course root)",
		"Chap 1",
		"Chap 2 Middleware",
		"Chpater 3 Postgres",
		"Chapter 5 Deploys ",
		"Scratch recordings",
	}
	if len(got) != len(want) {
		t.Fatalf("chapters = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chapter[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Debris must never be counted: Finder metadata, AppleDouble sidecars and an
// aborted zero-byte export would each otherwise show up as a lesson.
func TestScan_IgnoresDebris(t *testing.T) {
	root := coursefixture.Build(t)

	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, ch := range res.Course.Chapters {
		for _, f := range append(append([]*model.MediaFile{}, ch.Lessons...), ch.Attachments...) {
			switch f.Name {
			case ".DS_Store", "._goCourse-Intro chapter 1.mp4", "interrupted-export.mp4":
				t.Errorf("scan included debris file %q in %q", f.Name, ch.Display)
			}
		}
	}
}

func TestScan_LessonOrderingWithinChapter(t *testing.T) {
	root := coursefixture.Build(t)

	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, ch := range res.Course.Chapters {
		if ch.Display != "Chap 1" {
			continue
		}
		if len(ch.Lessons) != 2 {
			t.Fatalf("Chap 1 lessons = %d, want 2", len(ch.Lessons))
		}
		// The unnumbered intro leads, then 1.1.
		if got := ch.Lessons[0].Name; got != "goCourse-Intro chapter 1.mp4" {
			t.Errorf("first lesson = %q, want the unnumbered intro", got)
		}
		if got := ch.Lessons[1].Lesson.Number.String(); got != "1.1" {
			t.Errorf("second lesson number = %q, want 1.1", got)
		}
		return
	}
	t.Fatal("Chap 1 not found")
}

// The fast path and ffprobe must agree on duration, or the speed gain is
// bought with wrong numbers.
func TestScan_FastPathMatchesFFprobe(t *testing.T) {
	if err := scan.FFprobeAvailable(); err != nil {
		t.Skipf("ffprobe unavailable: %v", err)
	}
	root := coursefixture.Build(t)

	fast, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("fast scan: %v", err)
	}
	full, err := scan.Scan(context.Background(), scan.Options{Root: root, Full: true, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("full scan: %v", err)
	}

	if fast.FastPath == 0 {
		t.Error("expected the in-process reader to handle at least one file")
	}
	if full.FFprobed == 0 {
		t.Error("expected ffprobe to handle every file in a full scan")
	}

	fastByRel := map[string]time.Duration{}
	for _, f := range fast.Course.Lessons() {
		fastByRel[f.Rel] = f.Info.Duration
	}
	for _, f := range full.Course.Lessons() {
		a, b := fastByRel[f.Rel], f.Info.Duration
		diff := a - b
		if diff < 0 {
			diff = -diff
		}
		if diff > 100*time.Millisecond {
			t.Errorf("%s: fast path %v vs ffprobe %v", f.Rel, a, b)
		}
	}
}

func TestScan_CacheRoundTrip(t *testing.T) {
	root := coursefixture.Build(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	first, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.OpenCache(root)})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.CacheHits != 0 {
		t.Errorf("CacheHits = %d on a cold cache, want 0", first.CacheHits)
	}

	second, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.OpenCache(root)})
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.CacheHits != len(coursefixture.Layout) {
		t.Errorf("CacheHits = %d, want %d", second.CacheHits, len(coursefixture.Layout))
	}
	if second.Course.Duration() != first.Course.Duration() {
		t.Errorf("cached duration %v != fresh %v", second.Course.Duration(), first.Course.Duration())
	}
}
