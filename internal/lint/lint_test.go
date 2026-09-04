package lint_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ElAmir-Mansour/coursekit/internal/coursefixture"
	"github.com/ElAmir-Mansour/coursekit/internal/lint"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

func TestBuiltinProfilesLoad(t *testing.T) {
	names := lint.BuiltinNames()
	if len(names) < 4 {
		t.Fatalf("BuiltinNames() = %v, want at least udemy, youtube, lms, strict", names)
	}
	for _, n := range names {
		p, err := lint.LoadProfile(n)
		if err != nil {
			t.Errorf("LoadProfile(%q): %v", n, err)
			continue
		}
		if p.Name != n {
			t.Errorf("profile %q reports name %q", n, p.Name)
		}
		if p.Description == "" {
			t.Errorf("profile %q has no description", n)
		}
	}
}

func TestLoadProfile_UnknownNameListsOptions(t *testing.T) {
	_, err := lint.LoadProfile("teachable")
	if err == nil {
		t.Fatal("expected an error for an unknown profile")
	}
	// The message has to be actionable, or the user is left guessing.
	if !strings.Contains(err.Error(), "udemy") {
		t.Errorf("error = %v, want it to list the available profiles", err)
	}
}

func TestCheck_UdemyOnFixture(t *testing.T) {
	root := coursefixture.Build(t)

	res, err := scan.Scan(context.Background(), scan.Options{
		Root: root, Full: true, Cache: scan.NoCache(),
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	profile, err := lint.LoadProfile("udemy")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	rep := lint.Check(res.Course, profile)

	byRule := map[string]lint.Finding{}
	for _, f := range rep.Findings {
		byRule[f.Rule] = f
	}

	// The fixture is 16:10 apart from one deliberately correct 16:9 clip, so
	// the aspect rule has to fire on exactly the wrong ones.
	aspect, ok := byRule["video.aspect"]
	if !ok {
		t.Fatal("expected a video.aspect finding")
	}
	if got, want := aspect.Count(), len(coursefixture.Layout)-1; got != want {
		t.Errorf("video.aspect covers %d files, want %d (all but the one 16:9 clip)", got, want)
	}
	if aspect.Severity != lint.SevError {
		t.Errorf("video.aspect severity = %v, want error", aspect.Severity)
	}
	if !strings.Contains(aspect.Fix, "pad=") {
		t.Errorf("aspect fix should letterbox rather than crop, got %q", aspect.Fix)
	}
	for _, fn := range aspect.Files {
		if strings.Contains(fn.Rel, "Deploy Automation") {
			t.Errorf("the 16:9 clip %q was wrongly flagged", fn.Rel)
		}
	}

	// One file was authored at 44.1kHz while the rest are 48kHz.
	sr, ok := byRule["consistency.samplerate"]
	if !ok {
		t.Fatal("expected a consistency.samplerate finding")
	}
	if sr.Count() != 1 {
		t.Errorf("samplerate outliers = %d, want 1", sr.Count())
	}
	if !strings.Contains(sr.Files[0].Rel, "Edited-Intro-short") {
		t.Errorf("outlier = %q, want the intro file", sr.Files[0].Rel)
	}

	// Audio is authored at 160kbps against Udemy's 256kbps floor.
	if br, ok := byRule["audio.bitrate"]; !ok {
		t.Error("expected an audio.bitrate finding")
	} else if br.Count() != len(coursefixture.Layout) {
		t.Errorf("audio.bitrate covers %d files, want all %d", br.Count(), len(coursefixture.Layout))
	}

	// Structure problems the parser noticed.
	if _, ok := byRule["structure.chapter_typo"]; !ok {
		t.Error("expected the Chpater typo to be reported")
	}
	if _, ok := byRule["structure.chapter_whitespace"]; !ok {
		t.Error("expected the trailing-space chapter folder to be reported")
	}

	// Lesson 2.2 is missing from the fixture.
	gap, ok := byRule["numbering.gap"]
	if !ok {
		t.Fatal("expected a numbering.gap finding")
	}
	if !strings.Contains(gap.Files[0].Note, "2.2") {
		t.Errorf("gap note = %q, want it to name 2.2", gap.Files[0].Note)
	}

	// Names with "?" must be flagged as unportable.
	if np, ok := byRule["name.portable"]; !ok {
		t.Error("expected a name.portable finding")
	} else {
		var sawQuestion bool
		for _, fn := range np.Files {
			if strings.Contains(fn.Rel, "why REST is dead") {
				sawQuestion = true
			}
		}
		if !sawQuestion {
			t.Error("expected the filename containing '?' to be flagged")
		}
	}

	if rep.OK() {
		t.Error("report says OK, but the fixture has hard errors")
	}
	if rep.FilesChecked != len(coursefixture.Layout) {
		t.Errorf("FilesChecked = %d, want %d", rep.FilesChecked, len(coursefixture.Layout))
	}
}

// Findings must be ordered worst-first so the terminal output leads with what
// actually blocks an upload.
func TestCheck_FindingsSortedBySeverity(t *testing.T) {
	root := coursefixture.Build(t)
	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Full: true, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	profile, _ := lint.LoadProfile("udemy")
	rep := lint.Check(res.Course, profile)

	for i := 1; i < len(rep.Findings); i++ {
		if rep.Findings[i-1].Severity < rep.Findings[i].Severity {
			t.Fatalf("finding %d (%v) outranks its predecessor (%v)",
				i, rep.Findings[i].Severity, rep.Findings[i-1].Severity)
		}
	}
}

// The lms profile exists to catch exactly one thing that no other profile
// does: HEVC does not play reliably in Chrome or Firefox.
func TestCheck_LMSRejectsHEVC(t *testing.T) {
	course := &model.Course{
		Root:  "/course",
		Title: "course",
		Chapters: []*model.Chapter{{
			Display: "Chap 1",
			Lessons: []*model.MediaFile{{
				Rel:  "Chap 1/lesson.mp4",
				Name: "lesson.mp4",
				Kind: model.KindVideo,
				Info: model.MediaInfo{
					Full: true, Duration: 60_000_000_000,
					Width: 1920, Height: 1080, FPS: 30,
					VideoCodec: "hevc", VideoBitrate: 2_000_000,
					AudioCodec: "aac", AudioBitrate: 192_000,
					SampleRate: 48000, Channels: 2, HasAudio: true,
					Container: "mp4",
				},
			}},
		}},
	}

	lms, err := lint.LoadProfile("lms")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	rep := lint.Check(course, lms)

	var found bool
	for _, f := range rep.Findings {
		if f.Rule == "video.codec" {
			found = true
			if !strings.Contains(f.Detail, "Chrome") {
				t.Errorf("codec detail should explain the browser problem, got %q", f.Detail)
			}
		}
	}
	if !found {
		t.Error("lms profile did not flag HEVC")
	}

	// The same file must pass Udemy, which accepts HEVC.
	udemy, _ := lint.LoadProfile("udemy")
	for _, f := range lint.Check(course, udemy).Findings {
		if f.Rule == "video.codec" {
			t.Error("udemy should accept HEVC")
		}
	}
}

func TestCheck_LoudnessRules(t *testing.T) {
	mk := func(lufs, peak float64) *model.Course {
		return &model.Course{
			Chapters: []*model.Chapter{{
				Display: "Chap 1",
				Lessons: []*model.MediaFile{{
					Rel: "Chap 1/a.mp4", Name: "a.mp4", Kind: model.KindVideo,
					Info: model.MediaInfo{
						Full: true, Width: 1920, Height: 1080, FPS: 30,
						VideoCodec: "h264", VideoBitrate: 5_000_000,
						AudioCodec: "aac", AudioBitrate: 256_000,
						SampleRate: 48000, Channels: 2, HasAudio: true, Container: "mp4",
						Loudness: &model.Loudness{IntegratedLUFS: lufs, TruePeakDBTP: peak},
					},
				}},
			}},
		}
	}

	udemy, _ := lint.LoadProfile("udemy")

	// On target: no loudness finding at all.
	for _, f := range lint.Check(mk(-16, -3), udemy).Findings {
		if strings.HasPrefix(f.Rule, "audio.loudness") {
			t.Errorf("on-target audio produced %s", f.Rule)
		}
	}

	// Far off target escalates to an error rather than a warning.
	var sev lint.Severity = -1
	for _, f := range lint.Check(mk(-32.7, -14), udemy).Findings {
		if f.Rule == "audio.loudness" {
			sev = f.Severity
		}
	}
	if sev != lint.SevError {
		t.Errorf("-32.7 LUFS against a -16 target gave severity %v, want error", sev)
	}

	// A hot true peak is its own finding.
	var sawPeak bool
	for _, f := range lint.Check(mk(-16, 0.5), udemy).Findings {
		if f.Rule == "audio.truepeak" {
			sawPeak = true
		}
	}
	if !sawPeak {
		t.Error("expected a true-peak finding for a peak above -1 dBTP")
	}
}

func TestPortabilityRules(t *testing.T) {
	root := coursefixture.Build(t)
	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Full: true, Cache: scan.NoCache()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// The strict profile turns on the ASCII check, which the others leave off.
	strict, err := lint.LoadProfile("strict")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	rep := lint.Check(res.Course, strict)

	var sawPortable bool
	for _, f := range rep.Findings {
		if f.Rule == "name.portable" {
			sawPortable = true
		}
	}
	if !sawPortable {
		t.Error("strict profile did not flag unportable names")
	}
}
