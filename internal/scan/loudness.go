package scan

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// ErrNoFFmpeg means ffmpeg is missing, so loudness cannot be measured.
var ErrNoFFmpeg = errors.New(
	"ffmpeg not found on PATH: install it (brew install ffmpeg) to measure loudness")

// ErrNoAudio means the file has no audio track to measure.
var ErrNoAudio = errors.New("file has no audio track")

// maxLoudnessWorkers caps loudness concurrency.
//
// Measured on an 8-core machine with 4 performance cores, over 8 real course
// files: 1 worker 3.32s, 2 workers 1.92s, 4 workers 1.57s, 8 workers 1.59s.
// Throughput plateaus at 4 because the ebur128 filter is largely
// single-threaded, so more workers only add contention.
const maxLoudnessWorkers = 4

// LoudnessWorkers is the concurrency used for loudness measurement.
func LoudnessWorkers() int {
	if n := runtime.NumCPU(); n < maxLoudnessWorkers {
		return n
	}
	return maxLoudnessWorkers
}

// FFmpegAvailable reports whether ffmpeg can be used.
func FFmpegAvailable() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ErrNoFFmpeg
	}
	return nil
}

// MeasureLoudness runs an EBU R128 analysis on one file.
//
// Two choices here matter a great deal for speed, both measured on an 18m53s
// HEVC screen recording:
//
//   - Passing -vn so the video stream is never decoded: 24.2s becomes 5.2s.
//     Without it, ffmpeg burns five cores decoding pictures nobody looks at.
//   - Using the ebur128 filter rather than loudnorm's JSON output: 5.2s
//     against 34.5s, because loudnorm performs a full two-pass analysis.
//
// The summary is written to stderr at info level, so the log level cannot be
// lowered to error even though nothing else is wanted from it.
func MeasureLoudness(ctx context.Context, path string) (*model.Loudness, error) {
	if err := FFmpegAvailable(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin",
		"-v", "info",
		"-nostats",
		"-vn",
		"-i", path,
		"-af", "ebur128=peak=true",
		"-f", "null", "-",
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	loud, sawAudio, parseErr := parseEBUR128(stderr)

	// Drain and reap regardless, so a failed parse cannot leak the process.
	waitErr := cmd.Wait()

	// Because -vn discards the video stream, a file with no audio leaves
	// ffmpeg with nothing to output and it exits non-zero before the filter
	// ever runs. That is a silent file, not a broken tool, so report it as
	// such rather than surfacing an opaque exit status.
	if !sawAudio {
		return nil, ErrNoAudio
	}

	if parseErr != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("ffmpeg: %w", waitErr)
		}
		return nil, parseErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("ffmpeg: %w", waitErr)
	}
	return loud, nil
}

// parseEBUR128 reads the filter's summary block out of ffmpeg's log, and also
// reports whether an audio stream was seen at all.
//
// The block is indented plain text rather than anything machine-readable, and
// "Peak:" appears under more than one heading, so the current section has to
// be tracked while scanning.
func parseEBUR128(r io.Reader) (loud *model.Loudness, sawAudio bool, err error) {
	var (
		out       model.Loudness
		section   string
		sawSumm   bool
		gotI      bool
		sawStream bool
	)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		raw := sc.Text()
		line := strings.TrimSpace(raw)

		if strings.Contains(line, "Audio:") {
			sawStream = true
		}
		if strings.HasSuffix(line, "Summary:") {
			sawSumm = true
			section = ""
			continue
		}
		if !sawSumm {
			continue
		}

		switch {
		case strings.HasPrefix(line, "Integrated loudness"):
			section = "integrated"
			continue
		case strings.HasPrefix(line, "Loudness range"):
			section = "range"
			continue
		case strings.HasPrefix(line, "True peak"):
			section = "peak"
			continue
		case strings.HasPrefix(line, "Sample peak"):
			section = "samplepeak"
			continue
		}

		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		f, err := parseLeadingFloat(value)
		if err != nil {
			continue
		}

		switch {
		case section == "integrated" && key == "I":
			out.IntegratedLUFS, gotI = f, true
		case section == "range" && key == "LRA":
			out.RangeLU = f
		case section == "peak" && key == "Peak":
			out.TruePeakDBTP = f
		}
	}
	if err := sc.Err(); err != nil {
		return nil, sawStream, err
	}

	if !gotI {
		if !sawStream {
			return nil, false, ErrNoAudio
		}
		return nil, sawStream, errors.New("could not read loudness from ffmpeg output")
	}
	return &out, sawStream, nil
}

// parseLeadingFloat reads the number at the start of a value like
// "  -22.5 LUFS", ignoring the unit that follows.
func parseLeadingFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty value")
	}
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, fmt.Errorf("no number in %q", s)
	}
	return strconv.ParseFloat(s[:end], 64)
}

// MeasureCourseLoudness fills in loudness for every lesson in a course,
// reusing cached measurements for files that have not changed.
//
// This is opt-in because it is the one genuinely slow operation in the tool:
// roughly 200x realtime per worker, so a five-hour course takes tens of
// seconds even fully parallel, against milliseconds for a plain scan.
func MeasureCourseLoudness(ctx context.Context, course *model.Course, cache *Cache, progress func(Progress)) error {
	if err := FFmpegAvailable(); err != nil {
		return err
	}

	lessons := course.Lessons()
	var pending []*model.MediaFile

	for _, f := range lessons {
		fi, err := os.Stat(f.Path)
		if err != nil {
			continue
		}
		if l, ok := cache.GetLoudness(f.Path, fi); ok {
			f.Info.Loudness = l
			continue
		}
		pending = append(pending, f)
	}

	if len(pending) == 0 {
		return nil
	}

	workers := LoudnessWorkers()
	if workers > len(pending) {
		workers = len(pending)
	}

	var (
		done  atomic.Int64
		total = len(pending)
		jobs  = make(chan *model.MediaFile)
		wg    sync.WaitGroup
	)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if ctx.Err() != nil {
					return
				}

				loud, err := MeasureLoudness(ctx, f.Path)
				if err == nil {
					f.Info.Loudness = loud
					if fi, statErr := os.Stat(f.Path); statErr == nil {
						cache.Put(f.Path, fi, f.Info)
					}
				} else if !errors.Is(err, ErrNoAudio) && ctx.Err() == nil {
					f.ProbeErr = "loudness: " + err.Error()
				}

				n := int(done.Add(1))
				if progress != nil {
					progress(Progress{
						Phase:   "loudness",
						Done:    n,
						Total:   total,
						Current: f.Name,
					})
				}
			}
		}()
	}

	for _, f := range pending {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- f:
		}
	}
	close(jobs)
	wg.Wait()

	_ = cache.Save()
	return ctx.Err()
}
