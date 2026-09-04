package scan_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElAmir-Mansour/coursekit/internal/coursefixture"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// A tone attenuated by a known amount must measure quieter by roughly that
// amount. Asserting a relative difference tests the measurement itself, where
// asserting one absolute LUFS figure would only pin down this ffmpeg build's
// AAC encoder.
func TestMeasureLoudness_TracksAttenuation(t *testing.T) {
	if err := scan.FFmpegAvailable(); err != nil {
		t.Skipf("ffmpeg unavailable: %v", err)
	}
	dir := t.TempDir()

	// Three seconds, because the integrated measurement is gated in 400ms
	// blocks and a one-second clip does not give it enough to work with.
	build := func(name, gain string) string {
		path := filepath.Join(dir, name)
		if err := runFFmpeg(t,
			"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=3",
			"-af", "volume="+gain,
			"-c:a", "aac", "-b:a", "160k", "-ac", "2", path,
		); err != nil {
			t.Fatalf("build %s: %v", name, err)
		}
		return path
	}

	loudPath := build("loud.m4a", "0dB")
	quietPath := build("quiet.m4a", "-20dB")

	loud, err := scan.MeasureLoudness(context.Background(), loudPath)
	if err != nil {
		t.Fatalf("measure loud: %v", err)
	}
	quiet, err := scan.MeasureLoudness(context.Background(), quietPath)
	if err != nil {
		t.Fatalf("measure quiet: %v", err)
	}

	if loud.IntegratedLUFS == 0 || quiet.IntegratedLUFS == 0 {
		t.Fatalf("integrated loudness not parsed: loud=%.1f quiet=%.1f",
			loud.IntegratedLUFS, quiet.IntegratedLUFS)
	}

	delta := loud.IntegratedLUFS - quiet.IntegratedLUFS
	if delta < 17 || delta > 23 {
		t.Errorf("attenuating by 20dB changed loudness by %.1f LU (loud %.1f, quiet %.1f), want about 20",
			delta, loud.IntegratedLUFS, quiet.IntegratedLUFS)
	}

	if loud.TruePeakDBTP == 0 {
		t.Error("TruePeakDBTP = 0, want a parsed true-peak value")
	}
	if quiet.TruePeakDBTP >= loud.TruePeakDBTP {
		t.Errorf("quiet true peak %.1f should sit below loud %.1f",
			quiet.TruePeakDBTP, loud.TruePeakDBTP)
	}
}

func TestMeasureLoudness_NoAudioTrack(t *testing.T) {
	if err := scan.FFmpegAvailable(); err != nil {
		t.Skipf("ffmpeg unavailable: %v", err)
	}

	// A silent-video-only file must be reported as having no audio rather
	// than as a parse failure.
	dir := t.TempDir()
	path := filepath.Join(dir, "video-only.mp4")
	if err := runFFmpeg(t,
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=15:duration=1",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", path,
	); err != nil {
		t.Fatalf("build video-only fixture: %v", err)
	}

	_, err := scan.MeasureLoudness(context.Background(), path)
	if err == nil {
		t.Fatal("expected an error for a file with no audio track")
	}
	if !strings.Contains(err.Error(), "no audio") {
		t.Errorf("error = %v, want it to mention the missing audio track", err)
	}
}

func TestMeasureCourseLoudness_CachesResults(t *testing.T) {
	if err := scan.FFmpegAvailable(); err != nil {
		t.Skipf("ffmpeg unavailable: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := coursefixture.Build(t)

	res, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.OpenCache(root)})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	cache := scan.OpenCache(root)
	if err := scan.MeasureCourseLoudness(context.Background(), res.Course, cache, nil); err != nil {
		t.Fatalf("MeasureCourseLoudness: %v", err)
	}

	measured := 0
	for _, f := range res.Course.Lessons() {
		if f.Info.Loudness != nil {
			measured++
		}
	}
	if measured != len(coursefixture.Layout) {
		t.Fatalf("measured %d of %d lessons", measured, len(coursefixture.Layout))
	}

	// A second pass must be served entirely from cache, since loudness is the
	// one operation expensive enough to make that worth verifying.
	res2, err := scan.Scan(context.Background(), scan.Options{Root: root, Cache: scan.OpenCache(root)})
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	cache2 := scan.OpenCache(root)
	for _, f := range res2.Course.Lessons() {
		fi, statErr := os.Stat(f.Path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", f.Rel, statErr)
		}
		if _, ok := cache2.GetLoudness(f.Path, fi); !ok {
			t.Errorf("loudness not cached for %s", f.Rel)
		}
	}
}

func runFFmpeg(t *testing.T, args ...string) error {
	t.Helper()
	full := append([]string{"-nostdin", "-v", "error", "-y"}, args...)
	out, err := execCommand(t, "ffmpeg", full...)
	if err != nil {
		t.Logf("ffmpeg output: %s", out)
	}
	return err
}
