package model

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Kind separates the things that contribute runtime from the things that ride
// along with a course.
type Kind uint8

const (
	// KindAttachment is a slide deck, PDF, or worksheet: counted, never timed.
	KindAttachment Kind = iota
	// KindVideo is a lesson recording.
	KindVideo
	// KindAudio is an audio-only lesson.
	KindAudio
)

// Timed reports whether files of this kind contribute to course runtime.
func (k Kind) Timed() bool { return k == KindVideo || k == KindAudio }

var (
	videoExts = map[string]bool{
		".mp4": true, ".mov": true, ".mkv": true, ".webm": true, ".avi": true,
		".m4v": true, ".mpg": true, ".mpeg": true, ".wmv": true, ".flv": true,
		".mts": true, ".m2ts": true, ".ts": true, ".mxf": true, ".prores": true,
	}
	audioExts = map[string]bool{
		".mp3": true, ".m4a": true, ".wav": true, ".aac": true, ".flac": true,
		".aiff": true, ".aif": true, ".ogg": true, ".opus": true, ".caf": true,
	}
)

// KindFor classifies a path by extension. Anything not recognized as playable
// media is an attachment.
func KindFor(path string) Kind {
	switch ext := strings.ToLower(filepath.Ext(path)); {
	case videoExts[ext]:
		return KindVideo
	case audioExts[ext]:
		return KindAudio
	default:
		return KindAttachment
	}
}

// FastPathEligible reports whether a file's duration can be read in-process by
// parsing ISO-BMFF boxes, skipping the ffprobe subprocess entirely.
func FastPathEligible(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mov", ".m4v", ".m4a":
		return true
	}
	return false
}

// Loudness holds an EBU R128 measurement. It is expensive to produce (seconds
// per file), so it is only ever populated on request.
type Loudness struct {
	IntegratedLUFS float64 `json:"integrated_lufs"`
	RangeLU        float64 `json:"range_lu"`
	TruePeakDBTP   float64 `json:"true_peak_dbtp"`
}

// MediaInfo is the technical detail of one file. Zero values mean "not read":
// a scan fills in only Duration and dimensions, doctor fills in everything.
type MediaInfo struct {
	Duration     time.Duration `json:"duration"`
	Width        int           `json:"width,omitempty"`
	Height       int           `json:"height,omitempty"`
	FPS          float64       `json:"fps,omitempty"`
	VideoCodec   string        `json:"video_codec,omitempty"`
	AudioCodec   string        `json:"audio_codec,omitempty"`
	VideoBitrate int64         `json:"video_bitrate,omitempty"`
	AudioBitrate int64         `json:"audio_bitrate,omitempty"`
	TotalBitrate int64         `json:"total_bitrate,omitempty"`
	SampleRate   int           `json:"sample_rate,omitempty"`
	Channels     int           `json:"channels,omitempty"`
	Container    string        `json:"container,omitempty"`
	HasAudio     bool          `json:"has_audio"`
	Loudness     *Loudness     `json:"loudness,omitempty"`

	// Full is set when the record came from ffprobe rather than the fast path,
	// meaning codec and audio fields are trustworthy.
	Full bool `json:"full"`
}

// Resolution renders dimensions as "1920x1200", or an empty string if unknown.
func (m MediaInfo) Resolution() string {
	if m.Width == 0 || m.Height == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", m.Width, m.Height)
}

// AspectFloat is width divided by height, or 0 when dimensions are unknown.
func (m MediaInfo) AspectFloat() float64 {
	if m.Width == 0 || m.Height == 0 {
		return 0
	}
	return float64(m.Width) / float64(m.Height)
}

var namedAspects = []struct {
	name  string
	ratio float64
}{
	{"16:9", 16.0 / 9.0},
	{"16:10", 16.0 / 10.0},
	{"4:3", 4.0 / 3.0},
	{"3:2", 3.0 / 2.0},
	{"5:4", 5.0 / 4.0},
	{"21:9", 21.0 / 9.0},
	{"2:1", 2.0},
	{"1:1", 1.0},
	{"9:16", 9.0 / 16.0},
	{"4:5", 4.0 / 5.0},
	{"3:4", 3.0 / 4.0},
}

// AspectName gives the conventional name for the aspect ratio ("16:10" rather
// than the arithmetically-reduced "8:5"), falling back to a decimal form.
func (m MediaInfo) AspectName() string {
	a := m.AspectFloat()
	if a == 0 {
		return ""
	}
	for _, na := range namedAspects {
		if abs64(a-na.ratio) < 0.012 {
			return na.name
		}
	}
	return fmt.Sprintf("%.2f:1", a)
}

// MediaFile is one file on disk plus whatever has been learned about it.
type MediaFile struct {
	Path    string     `json:"path"`
	Rel     string     `json:"rel"`
	Name    string     `json:"name"`
	Size    int64      `json:"size"`
	ModTime time.Time  `json:"mod_time"`
	Kind    Kind       `json:"kind"`
	Lesson  LessonName `json:"-"`
	Info    MediaInfo  `json:"info"`

	// ProbeErr records why metadata is missing, so a broken file is reported
	// rather than silently counted as zero-length.
	ProbeErr string `json:"probe_err,omitempty"`
}

// Duration is the file's runtime, or zero if it was never probed.
func (f *MediaFile) Duration() time.Duration { return f.Info.Duration }

// Chapter is one directory of lessons.
type Chapter struct {
	Dir         string       `json:"dir"`
	Name        ChapterName  `json:"-"`
	Display     string       `json:"name"`
	IsRoot      bool         `json:"is_root"`
	Lessons     []*MediaFile `json:"lessons"`
	Attachments []*MediaFile `json:"attachments,omitempty"`
}

// Duration sums the runtime of every timed file in the chapter.
func (c *Chapter) Duration() time.Duration {
	var total time.Duration
	for _, f := range c.Lessons {
		total += f.Info.Duration
	}
	return total
}

// Size sums the bytes of lessons and attachments alike.
func (c *Chapter) Size() int64 {
	var total int64
	for _, f := range c.Lessons {
		total += f.Size
	}
	for _, f := range c.Attachments {
		total += f.Size
	}
	return total
}

// Course is a whole recorded course rooted at one directory.
type Course struct {
	Root     string     `json:"root"`
	Title    string     `json:"title"`
	Chapters []*Chapter `json:"chapters"`
	ScanTook string     `json:"scan_took,omitempty"`
}

// Duration is the total runtime of the course.
func (c *Course) Duration() time.Duration {
	var total time.Duration
	for _, ch := range c.Chapters {
		total += ch.Duration()
	}
	return total
}

// Size is the total bytes on disk.
func (c *Course) Size() int64 {
	var total int64
	for _, ch := range c.Chapters {
		total += ch.Size()
	}
	return total
}

// LessonCount is the number of timed files across the course.
func (c *Course) LessonCount() int {
	n := 0
	for _, ch := range c.Chapters {
		n += len(ch.Lessons)
	}
	return n
}

// AttachmentCount is the number of non-media files carried alongside.
func (c *Course) AttachmentCount() int {
	n := 0
	for _, ch := range c.Chapters {
		n += len(ch.Attachments)
	}
	return n
}

// Lessons flattens every timed file in chapter order.
func (c *Course) Lessons() []*MediaFile {
	out := make([]*MediaFile, 0, c.LessonCount())
	for _, ch := range c.Chapters {
		out = append(out, ch.Lessons...)
	}
	return out
}

// ProbeErrors collects files whose metadata could not be read.
func (c *Course) ProbeErrors() []*MediaFile {
	var out []*MediaFile
	for _, ch := range c.Chapters {
		for _, f := range ch.Lessons {
			if f.ProbeErr != "" {
				out = append(out, f)
			}
		}
	}
	return out
}

// SortChapters orders chapters the way a human reads a syllabus: root-level
// files first, then confidently-numbered chapters in numeric order, then
// weakly-numbered ones, then unnumbered folders alphabetically.
//
// This is the whole point of the parser — a lexical sort puts "Chpater 3"
// after "Chapter 9".
func (c *Course) SortChapters() {
	sort.SliceStable(c.Chapters, func(i, j int) bool {
		a, b := c.Chapters[i], c.Chapters[j]
		if a.IsRoot != b.IsRoot {
			return a.IsRoot
		}
		if a.Name.Confidence != b.Name.Confidence {
			return a.Name.Confidence > b.Name.Confidence
		}
		if a.Name.Confidence != ConfNone && a.Name.Order != b.Name.Order {
			return a.Name.Order < b.Name.Order
		}
		return strings.ToLower(a.Display) < strings.ToLower(b.Display)
	})
}

// SortLessons orders lessons within each chapter by their "x.y" numbering when
// present, and by name otherwise. Unnumbered lessons lead, since an untagged
// file in a numbered chapter is almost always the intro.
func (c *Course) SortLessons() {
	for _, ch := range c.Chapters {
		sort.SliceStable(ch.Lessons, func(i, j int) bool {
			a, b := ch.Lessons[i].Lesson.Number, ch.Lessons[j].Lesson.Number
			if a.Found != b.Found {
				return !a.Found
			}
			if a.Found {
				if a.Chapter != b.Chapter {
					return a.Chapter < b.Chapter
				}
				if a.Index != b.Index {
					return a.Index < b.Index
				}
			}
			return strings.ToLower(ch.Lessons[i].Name) < strings.ToLower(ch.Lessons[j].Name)
		})
	}
}

// HumanDuration renders a runtime the way a course page states it: "5h 25m",
// "52m 48s", "0s".
func HumanDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// ShortDuration renders a compact runtime for table columns: "5h25m", "52.8m".
func ShortDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	switch {
	case d >= time.Hour:
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		return fmt.Sprintf("%dh%02dm", h, m)
	case d >= time.Minute:
		return fmt.Sprintf("%.1fm", d.Minutes())
	default:
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
}

func abs64(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
