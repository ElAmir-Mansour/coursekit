package model

import "testing"

// The fixtures below reproduce the naming of a real recorded course, with the
// topic words changed. Every quirk is kept exactly: the misspelled chapter
// word, the trailing space, the unnumbered folders. Those are the cases this
// parser exists to handle, and inventing tidy names would test none of them.
func TestParseChapter_RealCourseFolders(t *testing.T) {
	tests := []struct {
		raw        string
		wantOrder  int
		wantTitle  string
		wantConf   Confidence
		wantTypo   bool
		wantUntidy bool
	}{
		{"Chap 1", 1, "", ConfStrong, false, false},
		{"Chap 2 Middleware", 2, "Middleware", ConfStrong, false, false},
		{"Chpater 3 Postgres", 3, "Postgres", ConfStrong, true, false},
		{"Chapter 4", 4, "", ConfStrong, false, false},
		{"Chapter 5 Deploys ", 5, "Deploys", ConfStrong, false, true},
		{"Chapter 6 Observability", 6, "Observability", ConfStrong, false, false},
		{"Chapter 7 Caching", 7, "Caching", ConfStrong, false, false},
		{"Chapter 8 - Local Dev Setup", 8, "Local Dev Setup", ConfStrong, false, false},
		{"Chapter 9 - profiling", 9, "profiling", ConfStrong, false, false},
		{"Scratch recordings", Unnumbered, "Scratch recordings", ConfNone, false, false},
		{"Intro takes and notes", Unnumbered, "Intro takes and notes", ConfNone, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseChapter(tc.raw)
			if got.Order != tc.wantOrder {
				t.Errorf("Order = %d, want %d", got.Order, tc.wantOrder)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Confidence != tc.wantConf {
				t.Errorf("Confidence = %d, want %d", got.Confidence, tc.wantConf)
			}
			if got.Misspelled() != tc.wantTypo {
				t.Errorf("Misspelled() = %v, want %v", got.Misspelled(), tc.wantTypo)
			}
			if got.Untidy() != tc.wantUntidy {
				t.Errorf("Untidy() = %v, want %v", got.Untidy(), tc.wantUntidy)
			}
		})
	}
}

func TestParseChapter_OtherConventions(t *testing.T) {
	tests := []struct {
		raw       string
		wantOrder int
		wantConf  Confidence
	}{
		{"03 - Postgres", 3, ConfStrong},
		{"07_advanced", 7, ConfStrong},
		{"Section 2", 2, ConfStrong},
		{"Module 11 Deployment", 11, ConfStrong},
		{"MODULE 4", 4, ConfStrong},
		{"Lecture 1", 1, ConfStrong},
		{"Part 2", 2, ConfStrong},
		{"Week 12", 12, ConfStrong},
		{"Secton 5", 5, ConfStrong},     // typo of "section"
		{"Modle 6", 6, ConfStrong},      // typo of "module"
		{"Lecutre 8", 8, ConfStrong},    // typo of "lecture"
		{"Swift 5 Basics", 5, ConfWeak}, // "Swift" is not a chapter word
		{"Bonus 2", 2, ConfWeak},
		{"Resources", Unnumbered, ConfNone},
		{"", Unnumbered, ConfNone},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseChapter(tc.raw)
			if got.Order != tc.wantOrder || got.Confidence != tc.wantConf {
				t.Errorf("ParseChapter(%q) = order %d conf %d, want order %d conf %d",
					tc.raw, got.Order, got.Confidence, tc.wantOrder, tc.wantConf)
			}
		})
	}
}

// A short unrelated word must not be dragged onto a short keyword by fuzzy
// matching, or every folder starting with two letters becomes a chapter.
func TestFuzzyKeyword_RejectsShortNoise(t *testing.T) {
	for _, w := range []string{"my", "an", "to", "cv", "ai", "db"} {
		if k, ok := fuzzyKeyword(w); ok {
			t.Errorf("fuzzyKeyword(%q) matched %q, want no match", w, k)
		}
	}
	for _, w := range []string{"chapter", "Chpater", "CHAPTER", "chap", "ch"} {
		if _, ok := fuzzyKeyword(w); !ok {
			t.Errorf("fuzzyKeyword(%q) did not match", w)
		}
	}
}

// Real lesson filenames. The numbering placement is wildly inconsistent and
// every one of these has to resolve to the same "x.y" shape.
func TestParseLesson_RealCourseFiles(t *testing.T) {
	tests := []struct {
		raw       string
		wantNum   string
		wantTitle string
	}{
		{"goCourse-why REST is dead ? -1.1.mp4", "1.1", "goCourse-why REST is dead ?"},
		{"goCourse-context pyramid . 1.2.mp4", "1.2", "goCourse-context pyramid"},
		{"goCourse-Tool selection 1.3.mp4", "1.3", "goCourse-Tool selection"},
		{"goCourse-Custmize your Handler - 1.4.mp4", "1.4", "goCourse-Custmize your Handler"},
		{"goCourse-Build Your Router -1.5.mp4", "1.5", "goCourse-Build Your Router"},
		{"goCourse- Middleware A Full Guide 2.1.mp4", "2.1", "goCourse- Middleware A Full Guide"},
		{"goCourse-middleware + routing 2.3 last.mp4", "2.3", "goCourse-middleware + routing last"},
		{"goCourse-POSTGRES to app - 3.2-.mp4", "3.2", "goCourse-POSTGRES to app"},
		{"goCourse-Postgres export to dump 3.4.mp4", "3.4", "goCourse-Postgres export to dump"},
		{"goCourse-Load Testing (k6:Vegeta)-5.2.mp4", "5.2", "goCourse-Load Testing (k6:Vegeta)"},
		{"gocourse-Tracing Requests And Making Diagram-7.2.mp4", "7.2", "gocourse-Tracing Requests And Making Diagram"},
		{"goCourse-Intro chapter 1.mp4", "", "goCourse-Intro chapter 1"},
		{"goCourse-Intro.mp4", "", "goCourse-Intro"},
		{"scratch clip.mp4", "", "scratch clip"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseLesson(tc.raw)
			if got.Number.String() != tc.wantNum {
				t.Errorf("Number = %q, want %q", got.Number.String(), tc.wantNum)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tc.wantTitle)
			}
		})
	}
}

func TestStripCommonPrefix(t *testing.T) {
	in := []string{
		"goCourse-why REST is dead ? -1.1.mp4",
		"goCourse-Tool selection 1.3.mp4",
		"goCourse-Intro chapter 1.mp4",
	}
	prefix, out := StripCommonPrefix(in)
	if prefix != "goCourse-" {
		t.Fatalf("prefix = %q, want %q", prefix, "goCourse-")
	}
	if out[0] != "why REST is dead ? -1.1.mp4" {
		t.Errorf("out[0] = %q", out[0])
	}

	// A prefix that does not end at a separator must be left alone, so
	// "Chapter1"/"Chapter2" never collapses to "1"/"2".
	if p, _ := StripCommonPrefix([]string{"Chapter1.mp4", "Chapter2.mp4"}); p != "" {
		t.Errorf("prefix = %q, want none for mid-word split", p)
	}

	// A single name has no shared prefix to speak of.
	if p, _ := StripCommonPrefix([]string{"only.mp4"}); p != "" {
		t.Errorf("prefix = %q, want none for single name", p)
	}

	// Case differences between files must defeat the shared prefix rather
	// than trimming a word off only some of them.
	if p, _ := StripCommonPrefix([]string{"goCourse-a.mp4", "gocourse-b.mp4"}); p != "" {
		t.Errorf("prefix = %q, want none across case mismatch", p)
	}
}
