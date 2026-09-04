package scan

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	ffprobe "gopkg.in/vansante/go-ffprobe.v2"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// ErrNoFFprobe means ffprobe is not installed, so full metadata cannot be
// read. Scanning still works via the in-process fast path.
var ErrNoFFprobe = errors.New(
	"ffprobe not found on PATH: install ffmpeg (brew install ffmpeg) to read full media metadata")

// probeTimeout bounds a single ffprobe call. A healthy local file answers in
// milliseconds; anything near this limit is a damaged or stalled read.
const probeTimeout = 30 * time.Second

var (
	ffprobeOnce sync.Once
	ffprobePath string
	ffprobeErr  error
)

// FFprobeAvailable reports whether ffprobe can be used, resolving its location
// once per process.
func FFprobeAvailable() error {
	ffprobeOnce.Do(func() {
		p, err := exec.LookPath("ffprobe")
		if err != nil {
			ffprobeErr = ErrNoFFprobe
			return
		}
		ffprobePath = p
		ffprobe.SetFFProbeBinPath(p)
	})
	return ffprobeErr
}

// FFprobePath is the resolved ffprobe binary, empty when unavailable.
func FFprobePath() string {
	_ = FFprobeAvailable()
	return ffprobePath
}

// FullProbe reads complete stream metadata for one file: codecs, bitrates,
// frame rate and audio layout, in addition to duration and dimensions.
//
// This costs a subprocess per file, which is why it backs doctor rather than
// scan. Callers should run it through a worker pool.
func FullProbe(ctx context.Context, path string) (model.MediaInfo, error) {
	var info model.MediaInfo

	if err := FFprobeAvailable(); err != nil {
		return info, err
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	data, err := ffprobe.ProbeURL(ctx, path)
	if err != nil {
		return info, fmt.Errorf("ffprobe: %w", err)
	}
	return infoFromProbe(data, path), nil
}

// infoFromProbe translates ffprobe's loosely-typed JSON into a MediaInfo. It is
// separated out so it can be unit-tested against captured probe output.
func infoFromProbe(data *ffprobe.ProbeData, path string) model.MediaInfo {
	info := model.MediaInfo{Full: true}

	if data.Format != nil {
		info.Duration = data.Format.Duration()
		info.TotalBitrate = parseInt64(data.Format.BitRate)
		info.Container = normalizeContainer(data.Format.FormatName, path)
	}

	if v := data.FirstVideoStream(); v != nil {
		info.Width, info.Height = v.Width, v.Height
		info.VideoCodec = v.CodecName
		info.VideoBitrate = parseInt64(v.BitRate)
		info.FPS = parseFraction(v.RFrameRate, v.AvgFrameRate)

		// Rotation lives in stream side data; a quarter turn swaps the
		// dimensions a viewer actually sees.
		if quarterTurn(v.SideDataList) {
			info.Width, info.Height = info.Height, info.Width
		}

		// Some containers report bitrate only at the format level. Attributing
		// all of it to video is wrong, but leaving it at zero makes a lint
		// rule fire falsely, so fall back only when there is no audio to
		// account for.
		if info.VideoBitrate == 0 && data.FirstAudioStream() == nil {
			info.VideoBitrate = info.TotalBitrate
		}
	}

	if a := data.FirstAudioStream(); a != nil {
		info.HasAudio = true
		info.AudioCodec = a.CodecName
		info.AudioBitrate = parseInt64(a.BitRate)
		info.SampleRate = int(parseInt64(a.SampleRate))
		info.Channels = a.Channels

		// Duration can be missing from the format block for some MOV files
		// while the stream still knows it.
		if info.Duration == 0 {
			if d, err := strconv.ParseFloat(a.Duration, 64); err == nil {
				info.Duration = time.Duration(d * float64(time.Second))
			}
		}
	}

	return info
}

// quarterTurn reports a 90 or 270 degree display rotation recorded in stream
// side data, which is how phones and some screen recorders store orientation.
// A quarter turn means the dimensions a viewer sees are the reverse of the
// coded ones, and aspect-ratio linting has to judge what the viewer sees.
func quarterTurn(list ffprobe.SideDataList) bool {
	dm, err := list.GetDisplayMatrix()
	if err != nil || dm == nil {
		return false
	}
	deg := dm.Rotation
	if deg < 0 {
		deg = -deg
	}
	return deg%180 == 90
}

// normalizeContainer reduces ffprobe's comma-joined format list to the single
// name a person would recognise, preferring the file's own extension when it
// appears in the list.
func normalizeContainer(formatName, path string) string {
	if formatName == "" {
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	names := strings.Split(formatName, ",")
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	for _, n := range names {
		if n == ext {
			return n
		}
	}
	return names[0]
}

// parseFraction reads an ffprobe rate like "30/1" or "30000/1001", trying the
// real frame rate first and falling back to the average.
func parseFraction(candidates ...string) float64 {
	for _, s := range candidates {
		if s == "" || s == "0/0" {
			continue
		}
		num, den, found := strings.Cut(s, "/")
		if !found {
			if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
				return f
			}
			continue
		}
		n, err1 := strconv.ParseFloat(num, 64)
		d, err2 := strconv.ParseFloat(den, 64)
		if err1 != nil || err2 != nil || d == 0 || n <= 0 {
			continue
		}
		return n / d
	}
	return 0
}

func parseInt64(s string) int64 {
	if s == "" || s == "N/A" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
