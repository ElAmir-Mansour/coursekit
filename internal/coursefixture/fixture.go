// Package coursefixture builds a throwaway course folder on disk for tests.
//
// The layout it produces is modelled on a real recorded course, typos and all:
// a misspelled chapter folder, a folder with a trailing space, unnumbered
// folders, inconsistent lesson numbering, a sample-rate outlier and a mix of
// aspect ratios. Testing against invented tidy names would prove nothing.
package coursefixture

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// Clip describes one generated video file.
type Clip struct {
	// Rel is the path relative to the course root, using forward slashes.
	Rel string
	// Seconds is the clip duration.
	Seconds int
	// Width and Height set the frame size, and therefore the aspect ratio.
	Width, Height int
	// SampleRate is the audio sample rate in Hz.
	SampleRate int
}

// Layout is the default fixture course. Durations are deliberately uneven so
// that a total can only be right if every file was actually read.
//
// Total: 3+1+2 + 2+1 + 1+2 + 1 + 1 = 14 seconds across 9 clips.
var Layout = []Clip{
	// Root-level files. The intro is the sample-rate outlier, matching the
	// real course where one re-exported file drifted to 44.1kHz.
	{Rel: "Edited-Intro-short.mp4", Seconds: 3, Width: 320, Height: 200, SampleRate: 44100},

	{Rel: "Chap 1/goCourse-Intro chapter 1.mp4", Seconds: 1, Width: 320, Height: 200, SampleRate: 48000},
	{Rel: "Chap 1/goCourse-why REST is dead ? -1.1.mp4", Seconds: 2, Width: 320, Height: 200, SampleRate: 48000},

	{Rel: "Chap 2 Middleware/goCourse- Middleware A Full Guide 2.1.mp4", Seconds: 2, Width: 320, Height: 200, SampleRate: 48000},
	// 2.2 is missing on purpose, so gap detection has something to find.
	{Rel: "Chap 2 Middleware/goCourse-middleware + routing 2.3 last.mp4", Seconds: 1, Width: 320, Height: 200, SampleRate: 48000},

	{Rel: "Chpater 3 Postgres/goCourse-Postgres text to schema 3.1.mp4", Seconds: 1, Width: 320, Height: 200, SampleRate: 48000},
	{Rel: "Chpater 3 Postgres/goCourse-POSTGRES to app - 3.2-.mp4", Seconds: 2, Width: 320, Height: 200, SampleRate: 48000},

	// The one correctly-shaped clip, so an aspect rule has to discriminate
	// rather than simply failing everything.
	{Rel: "Chapter 5 Deploys /goCourse-Deploy Automation-5.3.mp4", Seconds: 1, Width: 320, Height: 180, SampleRate: 48000},

	{Rel: "Scratch recordings/scratch clip.mp4", Seconds: 1, Width: 320, Height: 200, SampleRate: 48000},
}

// TotalSeconds is the summed duration of Layout.
func TotalSeconds() int {
	n := 0
	for _, c := range Layout {
		n += c.Seconds
	}
	return n
}

// Build writes the fixture course into a temporary directory and returns its
// root. The test is skipped when ffmpeg is unavailable, since there is no way
// to synthesise real media without it.
func Build(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH: skipping fixture-backed test")
	}

	root := filepath.Join(t.TempDir(), "Go Backend Course")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	for _, c := range Layout {
		rel := filepath.FromSlash(c.Rel)

		// A directory whose name ends in a space is legal on APFS and ext4 but
		// rejected by Windows, so the fixture drops those entries there
		// instead of failing the whole run.
		if runtime.GOOS == "windows" && hasTrailingSpaceSegment(rel) {
			continue
		}

		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		generate(t, full, c)
	}

	writeExtras(t, root)
	return root
}

// generate renders one short clip with a colour-bar video track and a tone
// audio track. ultrafast x264 keeps the whole fixture under a second.
func generate(t *testing.T, path string, c Clip) {
	t.Helper()

	size := strconv.Itoa(c.Width) + "x" + strconv.Itoa(c.Height)
	dur := strconv.Itoa(c.Seconds)

	cmd := exec.Command("ffmpeg",
		"-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size="+size+":rate=30:duration="+dur,
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate="+strconv.Itoa(c.SampleRate)+":duration="+dur,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "160k", "-ac", "2",
		"-shortest", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %s: %v\n%s", path, err, out)
	}
}

// writeExtras adds the debris a real course folder carries, all of which must
// be ignored by a scan: Finder metadata, AppleDouble sidecars, an aborted
// zero-byte recording, and a genuine attachment that is counted but not timed.
func writeExtras(t *testing.T, root string) {
	t.Helper()

	files := map[string][]byte{
		".DS_Store":                             []byte("\x00\x00\x00\x01Bud1"),
		"Chap 1/.DS_Store":                      []byte("\x00\x00\x00\x01Bud1"),
		"Chap 1/._goCourse-Intro chapter 1.mp4": []byte("\x00\x05\x16\x07AppleDouble"),
		"Chap 1/interrupted-export.mp4":         {},
		"Course_Handbook.pdf":                   []byte("%PDF-1.4\n% fixture\n"),
	}

	for rel, data := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func hasTrailingSpaceSegment(rel string) bool {
	dir := filepath.Dir(rel)
	for dir != "." && dir != string(filepath.Separator) {
		base := filepath.Base(dir)
		if len(base) > 0 && base[len(base)-1] == ' ' {
			return true
		}
		dir = filepath.Dir(dir)
	}
	return false
}
