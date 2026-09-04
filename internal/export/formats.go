package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// Summary is the machine-readable shape of a scanned course.
type Summary struct {
	Title       string          `json:"title"`
	Root        string          `json:"root"`
	Lessons     int             `json:"lessons"`
	Attachments int             `json:"attachments"`
	Chapters    int             `json:"chapters"`
	DurationSec float64         `json:"duration_seconds"`
	Duration    string          `json:"duration_human"`
	Bytes       int64           `json:"bytes"`
	Size        string          `json:"size_human"`
	ScanTook    string          `json:"scan_took,omitempty"`
	Detail      []ChapterDetail `json:"chapters_detail"`
}

// ChapterDetail is one chapter in the machine-readable output.
type ChapterDetail struct {
	Name        string         `json:"name"`
	Order       int            `json:"order"`
	Numbered    bool           `json:"numbered"`
	Lessons     int            `json:"lessons"`
	DurationSec float64        `json:"duration_seconds"`
	Duration    string         `json:"duration_human"`
	Bytes       int64          `json:"bytes"`
	Files       []LessonDetail `json:"files"`
}

// LessonDetail is one lesson in the machine-readable output.
type LessonDetail struct {
	Name        string   `json:"name"`
	Rel         string   `json:"rel"`
	Lesson      string   `json:"lesson_number,omitempty"`
	DurationSec float64  `json:"duration_seconds"`
	Duration    string   `json:"duration_human"`
	Bytes       int64    `json:"bytes"`
	Resolution  string   `json:"resolution,omitempty"`
	Aspect      string   `json:"aspect,omitempty"`
	FPS         float64  `json:"fps,omitempty"`
	VideoCodec  string   `json:"video_codec,omitempty"`
	AudioCodec  string   `json:"audio_codec,omitempty"`
	AudioKbps   int64    `json:"audio_kbps,omitempty"`
	SampleRate  int      `json:"sample_rate,omitempty"`
	Channels    int      `json:"channels,omitempty"`
	LUFS        *float64 `json:"integrated_lufs,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// Summarize converts a course into its serializable form.
func Summarize(c *model.Course) Summary {
	s := Summary{
		Title:       c.Title,
		Root:        c.Root,
		Lessons:     c.LessonCount(),
		Attachments: c.AttachmentCount(),
		Chapters:    countChapters(c),
		DurationSec: c.Duration().Seconds(),
		Duration:    model.HumanDuration(c.Duration()),
		Bytes:       c.Size(),
		Size:        humanize.IBytes(uint64(c.Size())),
		ScanTook:    c.ScanTook,
	}

	for _, ch := range c.Chapters {
		cd := ChapterDetail{
			Name:        ch.Display,
			Order:       ch.Name.Order,
			Numbered:    ch.Name.Confidence != model.ConfNone,
			Lessons:     len(ch.Lessons),
			DurationSec: ch.Duration().Seconds(),
			Duration:    model.HumanDuration(ch.Duration()),
			Bytes:       ch.Size(),
		}
		for _, f := range ch.Lessons {
			cd.Files = append(cd.Files, lessonDetail(f))
		}
		s.Detail = append(s.Detail, cd)
	}
	return s
}

func lessonDetail(f *model.MediaFile) LessonDetail {
	d := LessonDetail{
		Name:        f.Name,
		Rel:         f.Rel,
		Lesson:      f.Lesson.Number.String(),
		DurationSec: f.Info.Duration.Seconds(),
		Duration:    model.HumanDuration(f.Info.Duration),
		Bytes:       f.Size,
		Resolution:  f.Info.Resolution(),
		Aspect:      f.Info.AspectName(),
		FPS:         f.Info.FPS,
		VideoCodec:  f.Info.VideoCodec,
		AudioCodec:  f.Info.AudioCodec,
		SampleRate:  f.Info.SampleRate,
		Channels:    f.Info.Channels,
		Error:       f.ProbeErr,
	}
	if f.Info.AudioBitrate > 0 {
		d.AudioKbps = f.Info.AudioBitrate / 1000
	}
	if f.Info.Loudness != nil {
		v := f.Info.Loudness.IntegratedLUFS
		d.LUFS = &v
	}
	return d
}

// JSON writes the course as indented JSON.
func JSON(w io.Writer, c *model.Course) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(Summarize(c))
}

// CSV writes one row per lesson, which is the shape people want when they
// paste a course into a spreadsheet to plan or price it.
func CSV(w io.Writer, c *model.Course) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{
		"chapter", "chapter_order", "lesson_number", "file", "relative_path",
		"duration_seconds", "duration", "bytes", "size",
		"resolution", "aspect", "fps", "video_codec",
		"audio_codec", "audio_kbps", "sample_rate", "channels", "integrated_lufs", "error",
	}
	if err := cw.Write(header); err != nil {
		return err
	}

	for _, ch := range c.Chapters {
		order := ""
		if ch.Name.Confidence != model.ConfNone {
			order = strconv.Itoa(ch.Name.Order)
		}
		for _, f := range ch.Lessons {
			d := lessonDetail(f)
			lufs := ""
			if d.LUFS != nil {
				lufs = strconv.FormatFloat(*d.LUFS, 'f', 1, 64)
			}
			fps := ""
			if d.FPS > 0 {
				fps = strconv.FormatFloat(d.FPS, 'f', 2, 64)
			}
			row := []string{
				ch.Display, order, d.Lesson, d.Name, d.Rel,
				strconv.FormatFloat(d.DurationSec, 'f', 3, 64), d.Duration,
				strconv.FormatInt(d.Bytes, 10), humanize.IBytes(uint64(d.Bytes)),
				d.Resolution, d.Aspect, fps, d.VideoCodec,
				d.AudioCodec, intOrEmpty(d.AudioKbps), intOrEmpty(int64(d.SampleRate)),
				intOrEmpty(int64(d.Channels)), lufs, d.Error,
			}
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	return cw.Error()
}

// Markdown writes a report suitable for pasting into a repo, a course plan, or
// a message to a collaborator.
func Markdown(w io.Writer, c *model.Course) error {
	bw := &errWriter{w: w}

	bw.printf("# %s\n\n", c.Title)
	bw.printf("| | |\n|---|---|\n")
	bw.printf("| Lessons | %d |\n", c.LessonCount())
	bw.printf("| Total runtime | %s |\n", model.HumanDuration(c.Duration()))
	bw.printf("| Chapters | %d |\n", countChapters(c))
	bw.printf("| Size on disk | %s |\n", humanize.IBytes(uint64(c.Size())))
	if n := c.AttachmentCount(); n > 0 {
		bw.printf("| Attachments | %d |\n", n)
	}
	bw.printf("\n## Chapters\n\n")
	bw.printf("| # | Chapter | Lessons | Runtime | Size |\n|---:|---|---:|---:|---:|\n")

	for _, ch := range c.Chapters {
		num := "–"
		if !ch.IsRoot && ch.Name.Confidence != model.ConfNone {
			num = fmt.Sprintf("%02d", ch.Name.Order)
		}
		name := ch.Display
		if ch.IsRoot {
			name = "_(course root)_"
		}
		bw.printf("| %s | %s | %d | %s | %s |\n",
			num, escapePipes(name), len(ch.Lessons),
			model.HumanDuration(ch.Duration()),
			humanize.IBytes(uint64(ch.Size())))
	}

	bw.printf("\n## Lessons\n\n")
	for _, ch := range c.Chapters {
		if len(ch.Lessons) == 0 {
			continue
		}
		name := ch.Display
		if ch.IsRoot {
			name = "(course root)"
		}
		bw.printf("### %s\n\n", escapePipes(name))
		bw.printf("| Lesson | File | Runtime |\n|---|---|---:|\n")
		for _, f := range ch.Lessons {
			no := f.Lesson.Number.String()
			if no == "" {
				no = "–"
			}
			bw.printf("| %s | %s | %s |\n",
				no, escapePipes(f.Name), model.HumanDuration(f.Info.Duration))
		}
		bw.printf("\n")
	}

	bw.printf("---\n\n")
	bw.printf("_Generated by [coursekit](https://github.com/ElAmir-Mansour/coursekit) on %s._\n",
		time.Now().Format("2 January 2006"))

	return bw.err
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func intOrEmpty(n int64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}
