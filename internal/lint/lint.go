package lint

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// Severity ranks how much a finding matters.
type Severity int

const (
	// SevInfo is worth knowing but blocks nothing.
	SevInfo Severity = iota
	// SevWarn will degrade the student's experience.
	SevWarn
	// SevError will be rejected by the platform or fail to play.
	SevError
)

// String renders a severity for output.
func (s Severity) String() string {
	switch s {
	case SevError:
		return "error"
	case SevWarn:
		return "warn"
	default:
		return "info"
	}
}

// FileNote is one affected file plus what was measured about it.
type FileNote struct {
	Rel  string `json:"file"`
	Note string `json:"note,omitempty"`
}

// Finding is one rule's verdict across every file that broke it. Findings are
// aggregated by rule rather than emitted per file, because "34 files are the
// wrong shape" is one problem to solve, not thirty-four.
type Finding struct {
	Rule     string     `json:"rule"`
	Severity Severity   `json:"severity"`
	Title    string     `json:"title"`
	Detail   string     `json:"detail,omitempty"`
	Files    []FileNote `json:"files,omitempty"`
	Fix      string     `json:"fix,omitempty"`
	Docs     string     `json:"docs,omitempty"`
}

// Count is how many files the finding covers.
func (f Finding) Count() int { return len(f.Files) }

// Report is the outcome of checking a course against a profile.
type Report struct {
	Profile       string    `json:"profile"`
	Description   string    `json:"description,omitempty"`
	Reference     string    `json:"reference,omitempty"`
	Findings      []Finding `json:"findings"`
	FilesChecked  int       `json:"files_checked"`
	LoudnessKnown int       `json:"loudness_measured"`
}

// Counts totals findings by severity.
func (r *Report) Counts() (errors, warns, infos int) {
	for _, f := range r.Findings {
		switch f.Severity {
		case SevError:
			errors++
		case SevWarn:
			warns++
		default:
			infos++
		}
	}
	return
}

// OK reports whether the course passed with no errors.
func (r *Report) OK() bool {
	e, _, _ := r.Counts()
	return e == 0
}

// Check evaluates a course against a profile.
//
// Rules that depend on data which was never read are skipped rather than
// guessed at: a fast scan knows duration and dimensions but not codecs, so
// running doctor without full metadata reports shape problems and stays quiet
// about audio.
func Check(course *model.Course, p *Profile) *Report {
	rep := &Report{
		Profile:     p.Name,
		Description: p.Description,
		Reference:   p.Reference,
	}

	c := newCollector()
	lessons := course.Lessons()
	rep.FilesChecked = len(lessons)

	for _, f := range lessons {
		if f.Info.Loudness != nil {
			rep.LoudnessKnown++
		}
	}

	checkProbeErrors(c, course)
	for _, f := range lessons {
		checkVideo(c, p, f)
		checkAudio(c, p, f)
		checkFile(c, p, f)
	}
	checkNames(c, p, course)
	checkConsistency(c, p, lessons)
	checkStructure(c, p, course)

	rep.Findings = c.finish()
	return rep
}

// ---------- per-file rules ----------

func checkVideo(c *collector, p *Profile, f *model.MediaFile) {
	if f.Kind != model.KindVideo {
		return
	}
	in := f.Info

	// Shape. This runs on any scan, because dimensions are cheap to read.
	if len(p.Video.AspectRatios) > 0 && in.Width > 0 && in.Height > 0 {
		if !aspectAllowed(in.AspectFloat(), p) {
			c.add(finding{
				rule:   "video.aspect",
				sev:    SevError,
				title:  fmt.Sprintf("Aspect ratio must be %s", strings.Join(p.Video.AspectRatios, " or ")),
				detail: "Mac screen recordings default to 16:10, which is letterboxed or rejected by platforms that require 16:9.",
				fix:    fixAspectCmd(p),
				docs:   p.Reference,
			}, f.Rel, fmt.Sprintf("%s (%s)", in.Resolution(), in.AspectName()))
		}
	}

	if in.Width > 0 && in.Height > 0 {
		if p.Video.MinWidth > 0 && (in.Width < p.Video.MinWidth || in.Height < p.Video.MinHeight) {
			c.add(finding{
				rule:   "video.resolution.min",
				sev:    SevError,
				title:  fmt.Sprintf("Resolution below the %dx%d minimum", p.Video.MinWidth, p.Video.MinHeight),
				detail: "Text and code in a recording become unreadable once the platform scales it down further.",
				fix:    fmt.Sprintf("ffmpeg -i IN -vf scale=%d:-2 -c:a copy OUT", p.Video.RecWidth),
				docs:   p.Reference,
			}, f.Rel, in.Resolution())
		} else if p.Video.RecWidth > 0 && in.Width < p.Video.RecWidth {
			c.add(finding{
				rule:   "video.resolution.recommended",
				sev:    SevInfo,
				title:  fmt.Sprintf("Below the recommended %dx%d", p.Video.RecWidth, p.Video.RecHeight),
				detail: "Acceptable, but a sharper master gives the platform more to work with.",
			}, f.Rel, in.Resolution())
		}
	}

	// Everything below needs ffprobe data.
	if !in.Full {
		return
	}

	if len(p.Video.Codecs) > 0 && in.VideoCodec != "" && !containsFold(p.Video.Codecs, in.VideoCodec) {
		c.add(finding{
			rule:   "video.codec",
			sev:    SevError,
			title:  fmt.Sprintf("Video codec must be one of: %s", strings.Join(p.Video.Codecs, ", ")),
			detail: codecDetail(p, in.VideoCodec),
			fix:    "ffmpeg -i IN -c:v libx264 -crf 20 -preset slow -pix_fmt yuv420p -c:a copy OUT",
			docs:   p.Reference,
		}, f.Rel, in.VideoCodec)
	}

	if in.FPS > 0 {
		if p.Video.MinFPS > 0 && in.FPS < p.Video.MinFPS-0.5 {
			c.add(finding{
				rule:  "video.fps.low",
				sev:   SevWarn,
				title: fmt.Sprintf("Frame rate below %.0f fps", p.Video.MinFPS),
				fix:   fmt.Sprintf("ffmpeg -i IN -r %.0f -c:a copy OUT", p.Video.MinFPS),
				docs:  p.Reference,
			}, f.Rel, fmt.Sprintf("%.2f fps", in.FPS))
		}
		if p.Video.MaxFPS > 0 && in.FPS > p.Video.MaxFPS+0.5 {
			c.add(finding{
				rule:  "video.fps.high",
				sev:   SevWarn,
				title: fmt.Sprintf("Frame rate above %.0f fps", p.Video.MaxFPS),
				fix:   fmt.Sprintf("ffmpeg -i IN -r %.0f -c:a copy OUT", p.Video.MaxFPS),
				docs:  p.Reference,
			}, f.Rel, fmt.Sprintf("%.2f fps", in.FPS))
		}
	}

	if p.Video.MinBitrateKbps > 0 && in.VideoBitrate > 0 {
		kbps := int(in.VideoBitrate / 1000)
		if kbps < p.Video.MinBitrateKbps {
			c.add(finding{
				rule:   "video.bitrate",
				sev:    SevWarn,
				title:  fmt.Sprintf("Video bitrate below %d kbps", p.Video.MinBitrateKbps),
				detail: "Low bitrate shows up as smeared text during scrolling and typing.",
				fix:    fmt.Sprintf("ffmpeg -i IN -c:v libx264 -b:v %dk -c:a copy OUT", p.Video.RecBitrateKbps),
				docs:   p.Reference,
			}, f.Rel, fmt.Sprintf("%d kbps", kbps))
		}
	}
}

func checkAudio(c *collector, p *Profile, f *model.MediaFile) {
	in := f.Info

	// Loudness is measured independently of a full probe, so check it first.
	if in.Loudness != nil && p.Audio.TargetLUFS != 0 {
		off := in.Loudness.IntegratedLUFS - p.Audio.TargetLUFS
		tol := p.Audio.LUFSTolerance
		if tol <= 0 {
			tol = 2
		}
		if math.Abs(off) > tol {
			sev := SevWarn
			if math.Abs(off) > tol+6 {
				sev = SevError
			}
			direction := "quieter"
			if off > 0 {
				direction = "louder"
			}
			c.add(finding{
				rule:   "audio.loudness",
				sev:    sev,
				title:  fmt.Sprintf("Loudness off the %.0f LUFS target", p.Audio.TargetLUFS),
				detail: fmt.Sprintf("Students reach for the volume control on every lesson, and quiet audio is the most common complaint about self-recorded courses. Measured %s than target.", direction),
				fix: fmt.Sprintf("ffmpeg -i IN -af loudnorm=I=%.0f:TP=%.1f:LRA=11 -c:v copy OUT",
					p.Audio.TargetLUFS, p.Audio.MaxTruePeak),
				docs: "https://en.wikipedia.org/wiki/LUFS",
			}, f.Rel, fmt.Sprintf("%.1f LUFS (%+.1f LU)", in.Loudness.IntegratedLUFS, off))
		}

		if p.Audio.MaxTruePeak != 0 && in.Loudness.TruePeakDBTP > p.Audio.MaxTruePeak {
			c.add(finding{
				rule:   "audio.truepeak",
				sev:    SevWarn,
				title:  fmt.Sprintf("True peak above %.1f dBTP", p.Audio.MaxTruePeak),
				detail: "Peaks this close to full scale clip once the platform re-encodes.",
				fix: fmt.Sprintf("ffmpeg -i IN -af loudnorm=I=%.0f:TP=%.1f:LRA=11 -c:v copy OUT",
					p.Audio.TargetLUFS, p.Audio.MaxTruePeak),
			}, f.Rel, fmt.Sprintf("%.1f dBTP", in.Loudness.TruePeakDBTP))
		}
	}

	if !in.Full {
		return
	}

	if p.Audio.Required && !in.HasAudio {
		c.add(finding{
			rule:   "audio.missing",
			sev:    SevError,
			title:  "No audio track",
			detail: "A lesson with no sound is almost always a recording mistake.",
		}, f.Rel, "")
		return
	}
	if !in.HasAudio {
		return
	}

	if len(p.Audio.Codecs) > 0 && in.AudioCodec != "" && !containsFold(p.Audio.Codecs, in.AudioCodec) {
		c.add(finding{
			rule:  "audio.codec",
			sev:   SevError,
			title: fmt.Sprintf("Audio codec must be one of: %s", strings.Join(p.Audio.Codecs, ", ")),
			fix:   fmt.Sprintf("ffmpeg -i IN -c:v copy -c:a aac -b:a %dk OUT", maxInt(p.Audio.RecBitrateKbps, 256)),
			docs:  p.Reference,
		}, f.Rel, in.AudioCodec)
	}

	if p.Audio.MinBitrateKbps > 0 && in.AudioBitrate > 0 {
		kbps := int(in.AudioBitrate / 1000)
		if kbps < p.Audio.MinBitrateKbps {
			c.add(finding{
				rule:   "audio.bitrate",
				sev:    SevWarn,
				title:  fmt.Sprintf("Audio bitrate below %d kbps", p.Audio.MinBitrateKbps),
				detail: "Speech survives a low bitrate better than music, but the platform re-encodes on top of it and the loss compounds.",
				fix:    fmt.Sprintf("ffmpeg -i IN -c:v copy -c:a aac -b:a %dk OUT", maxInt(p.Audio.RecBitrateKbps, p.Audio.MinBitrateKbps)),
				docs:   p.Reference,
			}, f.Rel, fmt.Sprintf("%d kbps", kbps))
		}
	}

	if len(p.Audio.Channels) > 0 && in.Channels > 0 && !containsInt(p.Audio.Channels, in.Channels) {
		c.add(finding{
			rule:  "audio.channels",
			sev:   SevWarn,
			title: fmt.Sprintf("Channel count must be one of: %s", joinInts(p.Audio.Channels)),
			fix:   "ffmpeg -i IN -c:v copy -c:a aac -ac 2 OUT",
			docs:  p.Reference,
		}, f.Rel, fmt.Sprintf("%d ch", in.Channels))
	}

	if len(p.Audio.SampleRates) > 0 && in.SampleRate > 0 && !containsInt(p.Audio.SampleRates, in.SampleRate) {
		c.add(finding{
			rule:  "audio.samplerate",
			sev:   SevWarn,
			title: fmt.Sprintf("Sample rate must be one of: %s Hz", joinInts(p.Audio.SampleRates)),
			fix:   fmt.Sprintf("ffmpeg -i IN -c:v copy -c:a aac -ar %d OUT", p.Audio.SampleRates[0]),
			docs:  p.Reference,
		}, f.Rel, fmt.Sprintf("%d Hz", in.SampleRate))
	}
}

func checkFile(c *collector, p *Profile, f *model.MediaFile) {
	in := f.Info

	if p.File.MaxSizeBytes > 0 && f.Size > p.File.MaxSizeBytes {
		c.add(finding{
			rule:  "file.size",
			sev:   SevError,
			title: fmt.Sprintf("Larger than the %s limit", humanize.IBytes(uint64(p.File.MaxSizeBytes))),
			fix:   "ffmpeg -i IN -c:v libx264 -crf 23 -preset slow -c:a aac -b:a 192k OUT",
			docs:  p.Reference,
		}, f.Rel, humanize.IBytes(uint64(f.Size)))
	}

	if p.File.MaxSeconds > 0 && in.Duration.Seconds() > p.File.MaxSeconds {
		c.add(finding{
			rule:   "file.duration",
			sev:    SevError,
			title:  fmt.Sprintf("Longer than the %s limit", model.HumanDuration(time.Duration(p.File.MaxSeconds)*time.Second)),
			detail: "Split the recording into shorter lessons; students also finish short lessons far more often.",
			docs:   p.Reference,
		}, f.Rel, model.HumanDuration(in.Duration))
	}

	if len(p.File.Containers) > 0 && in.Full && in.Container != "" {
		if !containsFold(p.File.Containers, in.Container) {
			c.add(finding{
				rule:  "file.container",
				sev:   SevError,
				title: fmt.Sprintf("Container must be one of: %s", strings.Join(p.File.Containers, ", ")),
				fix:   "ffmpeg -i IN -c copy -movflags +faststart OUT.mp4",
				docs:  p.Reference,
			}, f.Rel, in.Container)
		}
	}
}

// ---------- name and structure rules ----------

func checkNames(c *collector, p *Profile, course *model.Course) {
	if !p.File.PortableNames && !p.File.ASCIIOnly {
		return
	}

	type target struct {
		rel  string
		name string
	}
	var targets []target

	for _, ch := range course.Chapters {
		if !ch.IsRoot && ch.Display != "" {
			targets = append(targets, target{rel: ch.Display + "/", name: ch.Name.Raw})
		}
		for _, f := range ch.Lessons {
			targets = append(targets, target{rel: f.Rel, name: f.Name})
		}
		for _, f := range ch.Attachments {
			targets = append(targets, target{rel: f.Rel, name: f.Name})
		}
	}

	for _, t := range targets {
		if p.File.PortableNames {
			if problems := portabilityProblems(t.name); len(problems) > 0 {
				c.add(finding{
					rule:   "name.portable",
					sev:    SevWarn,
					title:  "Name will not survive every platform",
					detail: "These are legal on macOS but rejected by Windows, mangled in URLs, or silently altered by zip and upload tools. Trailing spaces are also invisible in Finder.",
					fix:    "coursekit rename --commit    # normalises names",
					docs:   "https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file",
				}, t.rel, strings.Join(problems, ", "))
			}
		}
		if p.File.ASCIIOnly && !isASCII(t.name) {
			c.add(finding{
				rule:   "name.ascii",
				sev:    SevInfo,
				title:  "Name contains non-ASCII characters",
				detail: "Usually fine on modern platforms, but some uploaders and archive tools still corrupt these.",
			}, t.rel, "")
		}
	}
}

func checkStructure(c *collector, p *Profile, course *model.Course) {
	if !p.Consistency.ChapterOrdering {
		return
	}

	for _, ch := range course.Chapters {
		if ch.IsRoot {
			if len(ch.Lessons) > 0 {
				c.add(finding{
					rule:   "structure.loose_lessons",
					sev:    SevInfo,
					title:  "Lessons sitting at the course root",
					detail: "Files outside any chapter folder are easy to forget when uploading.",
				}, "(course root)", fmt.Sprintf("%d file(s)", len(ch.Lessons)))
			}
			continue
		}

		if ch.Name.Misspelled() {
			c.add(finding{
				rule:   "structure.chapter_typo",
				sev:    SevWarn,
				title:  "Misspelled chapter keyword",
				detail: "coursekit reads through the typo, but Finder, your platform's uploader, and any plain alphabetical sort will not. This is why chapter 3 lands after chapter 9.",
				fix:    "coursekit rename --commit",
			}, ch.Display, fmt.Sprintf("%q should be %q", ch.Name.Keyword, ch.Name.Canonical))
		}

		if ch.Name.Untidy() {
			c.add(finding{
				rule:   "structure.chapter_whitespace",
				sev:    SevWarn,
				title:  "Chapter folder has leading or trailing whitespace",
				detail: "Invisible in Finder, and a common cause of broken paths in scripts and uploads.",
				fix:    "coursekit rename --commit",
			}, ch.Display, fmt.Sprintf("%q", ch.Name.Raw))
		}

		if ch.Name.Confidence == model.ConfNone {
			c.add(finding{
				rule:   "structure.chapter_unnumbered",
				sev:    SevInfo,
				title:  "Chapter folder has no number",
				detail: "Unnumbered folders have no defined position, so their order depends on whatever tool is reading them.",
				fix:    "coursekit rename --plan",
			}, ch.Display, "")
		}
	}
}

func checkConsistency(c *collector, p *Profile, lessons []*model.MediaFile) {
	cons := p.Consistency

	if cons.UniformResolution {
		groups := map[string][]*model.MediaFile{}
		for _, f := range lessons {
			if r := f.Info.Resolution(); r != "" {
				groups[r] = append(groups[r], f)
			}
		}
		if len(groups) > 1 {
			addOdd(c, groups, finding{
				rule:   "consistency.resolution",
				sev:    SevWarn,
				title:  fmt.Sprintf("Course mixes %d resolutions", len(groups)),
				detail: "A course that changes size mid-way looks unfinished, and the odd files out are usually a re-export that used different settings.",
				fix:    "ffmpeg -i IN -vf scale=1920:-2 -c:a copy OUT",
			})
		}
	}

	if cons.UniformSampleRate {
		groups := map[string][]*model.MediaFile{}
		for _, f := range lessons {
			if f.Info.Full && f.Info.SampleRate > 0 {
				groups[fmt.Sprintf("%d Hz", f.Info.SampleRate)] = append(groups[fmt.Sprintf("%d Hz", f.Info.SampleRate)], f)
			}
		}
		if len(groups) > 1 {
			addOdd(c, groups, finding{
				rule:   "consistency.samplerate",
				sev:    SevWarn,
				title:  fmt.Sprintf("Course mixes %d audio sample rates", len(groups)),
				detail: "Mixed rates cause resampling artefacts and clicks when lessons are joined or an intro is prepended.",
				fix:    "ffmpeg -i IN -c:v copy -c:a aac -ar 48000 OUT",
			})
		}
	}

	if cons.UniformFrameRate {
		groups := map[string][]*model.MediaFile{}
		for _, f := range lessons {
			if f.Info.Full && f.Info.FPS > 0 {
				groups[fmt.Sprintf("%.0f fps", f.Info.FPS)] = append(groups[fmt.Sprintf("%.0f fps", f.Info.FPS)], f)
			}
		}
		if len(groups) > 1 {
			addOdd(c, groups, finding{
				rule:  "consistency.framerate",
				sev:   SevInfo,
				title: fmt.Sprintf("Course mixes %d frame rates", len(groups)),
			})
		}
	}

	if cons.UniformCodec {
		groups := map[string][]*model.MediaFile{}
		for _, f := range lessons {
			if f.Info.Full && f.Info.VideoCodec != "" {
				groups[f.Info.VideoCodec] = append(groups[f.Info.VideoCodec], f)
			}
		}
		if len(groups) > 1 {
			addOdd(c, groups, finding{
				rule:   "consistency.codec",
				sev:    SevWarn,
				title:  fmt.Sprintf("Course mixes %d video codecs", len(groups)),
				detail: "Every codec needs its own browser support story; one odd file out is one lesson that will not play for someone.",
			})
		}
	}

	if cons.MaxLoudnessSpread > 0 {
		var measured []*model.MediaFile
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, f := range lessons {
			if f.Info.Loudness == nil {
				continue
			}
			measured = append(measured, f)
			lo = math.Min(lo, f.Info.Loudness.IntegratedLUFS)
			hi = math.Max(hi, f.Info.Loudness.IntegratedLUFS)
		}
		if len(measured) > 1 && hi-lo > cons.MaxLoudnessSpread {
			fnd := finding{
				rule:   "consistency.loudness_spread",
				sev:    SevWarn,
				title:  fmt.Sprintf("Loudness varies by %.1f LU across the course", hi-lo),
				detail: fmt.Sprintf("Quietest %.1f LUFS, loudest %.1f LUFS. Students notice the jump between lessons more than the absolute level.", lo, hi),
				fix:    "ffmpeg -i IN -af loudnorm=I=-16:TP=-1.5:LRA=11 -c:v copy OUT",
			}
			// Name the extremes rather than every file: those are the two the
			// creator actually needs to look at.
			for _, f := range measured {
				v := f.Info.Loudness.IntegratedLUFS
				if v == lo || v == hi {
					c.add(fnd, f.Rel, fmt.Sprintf("%.1f LUFS", v))
				}
			}
		}
	}

	if cons.NumberingGaps {
		checkNumbering(c, lessons)
	}
}

// checkNumbering looks for holes and repeats in "x.y" lesson numbering, which
// normally mean a lesson was never exported or was exported twice.
func checkNumbering(c *collector, lessons []*model.MediaFile) {
	byChapter := map[int]map[int][]*model.MediaFile{}
	for _, f := range lessons {
		n := f.Lesson.Number
		if !n.Found {
			continue
		}
		if byChapter[n.Chapter] == nil {
			byChapter[n.Chapter] = map[int][]*model.MediaFile{}
		}
		byChapter[n.Chapter][n.Index] = append(byChapter[n.Chapter][n.Index], f)
	}

	chapters := make([]int, 0, len(byChapter))
	for ch := range byChapter {
		chapters = append(chapters, ch)
	}
	sort.Ints(chapters)

	for _, ch := range chapters {
		idx := byChapter[ch]

		for i, files := range idx {
			if len(files) > 1 {
				for _, f := range files {
					c.add(finding{
						rule:   "numbering.duplicate",
						sev:    SevWarn,
						title:  "Duplicate lesson numbers",
						detail: "Two files claim the same position, so their order is undefined.",
					}, f.Rel, fmt.Sprintf("%d.%d", ch, i))
				}
			}
		}

		maxIdx := 0
		for i := range idx {
			if i > maxIdx {
				maxIdx = i
			}
		}
		var missing []string
		for i := 1; i < maxIdx; i++ {
			if _, ok := idx[i]; !ok {
				missing = append(missing, fmt.Sprintf("%d.%d", ch, i))
			}
		}
		if len(missing) > 0 {
			c.add(finding{
				rule:   "numbering.gap",
				sev:    SevInfo,
				title:  "Gaps in lesson numbering",
				detail: "Either a lesson was never exported, or the numbering simply skips. Worth confirming which.",
			}, fmt.Sprintf("chapter %d", ch), "missing "+strings.Join(missing, ", "))
		}
	}
}

func checkProbeErrors(c *collector, course *model.Course) {
	for _, f := range course.ProbeErrors() {
		c.add(finding{
			rule:   "probe.failed",
			sev:    SevError,
			title:  "Could not read media metadata",
			detail: "The file may be truncated, still being written, or not actually video.",
		}, f.Rel, f.ProbeErr)
	}
}

// addOdd reports a consistency finding, naming the files in the minority
// groups. Listing the majority would bury the two files that are actually
// wrong under thirty that are fine.
func addOdd(c *collector, groups map[string][]*model.MediaFile, f finding) {
	type g struct {
		key   string
		files []*model.MediaFile
	}
	all := make([]g, 0, len(groups))
	for k, v := range groups {
		all = append(all, g{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if len(all[i].files) != len(all[j].files) {
			return len(all[i].files) > len(all[j].files)
		}
		return all[i].key < all[j].key
	})

	majority := all[0]
	f.detail = strings.TrimSpace(f.detail + fmt.Sprintf(" Most of the course is %s (%d files); the following differ.",
		majority.key, len(majority.files)))

	for _, grp := range all[1:] {
		for _, file := range grp.files {
			c.add(f, file.Rel, grp.key)
		}
	}
}

// ---------- helpers ----------

// finding is the immutable description of a rule, separate from the files that
// tripped it.
type finding struct {
	rule   string
	sev    Severity
	title  string
	detail string
	fix    string
	docs   string
}

type collector struct {
	order []string
	byKey map[string]*Finding
}

func newCollector() *collector {
	return &collector{byKey: map[string]*Finding{}}
}

// add records that one file broke one rule, creating the aggregate finding on
// first sight.
func (c *collector) add(f finding, rel, note string) {
	existing, ok := c.byKey[f.rule]
	if !ok {
		existing = &Finding{
			Rule:     f.rule,
			Severity: f.sev,
			Title:    f.title,
			Detail:   f.detail,
			Fix:      f.fix,
			Docs:     f.docs,
		}
		c.byKey[f.rule] = existing
		c.order = append(c.order, f.rule)
	}
	// A rule can fire at different severities for different files; the worst
	// one governs how the aggregate is reported.
	if f.sev > existing.Severity {
		existing.Severity = f.sev
	}
	existing.Files = append(existing.Files, FileNote{Rel: rel, Note: note})
}

// finish returns findings sorted worst-first, then by how many files each
// affects, so the biggest problem is always at the top.
func (c *collector) finish() []Finding {
	out := make([]Finding, 0, len(c.order))
	for _, k := range c.order {
		f := c.byKey[k]
		sort.SliceStable(f.Files, func(i, j int) bool { return f.Files[i].Rel < f.Files[j].Rel })
		out = append(out, *f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		if len(out[i].Files) != len(out[j].Files) {
			return len(out[i].Files) > len(out[j].Files)
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}

func aspectAllowed(a float64, p *Profile) bool {
	tol := p.aspectTolerance()
	for _, s := range p.Video.AspectRatios {
		want, err := parseAspect(s)
		if err != nil {
			continue
		}
		if math.Abs(a-want) <= tol {
			return true
		}
	}
	return false
}

// fixAspectCmd builds a letterbox-fit command rather than a crop, because
// cropping a screen recording cuts off a menu bar or a dock and the creator
// will not notice until a student does.
func fixAspectCmd(p *Profile) string {
	w, h := p.Video.RecWidth, p.Video.RecHeight
	if w == 0 || h == 0 {
		w, h = 1920, 1080
	}
	return fmt.Sprintf(
		"ffmpeg -i IN -vf \"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black\" -c:a copy OUT",
		w, h, w, h)
}

func codecDetail(p *Profile, got string) string {
	if p.Name == "lms" && strings.EqualFold(got, "hevc") {
		return "HEVC plays in Safari but not reliably in Chrome or Firefox, so a self-hosted course encoded this way will fail for some students with no error message."
	}
	return "The platform will either reject this or re-encode it, and re-encoding costs a generation of quality."
}

func portabilityProblems(name string) []string {
	var out []string

	if strings.TrimSpace(name) != name {
		out = append(out, "leading or trailing space")
	}
	if strings.HasSuffix(name, ".") {
		out = append(out, "trailing dot")
	}
	if bad := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"|?*`, r) {
			return r
		}
		return -1
	}, name); bad != "" {
		out = append(out, fmt.Sprintf("reserved character(s) %q", dedupeRunes(bad)))
	}
	if len(name) > 200 {
		out = append(out, fmt.Sprintf("very long name (%d bytes)", len(name)))
	}

	stem := name
	if i := strings.IndexByte(stem, '.'); i > 0 {
		stem = stem[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		out = append(out, "reserved device name on Windows")
	}
	return out
}

func dedupeRunes(s string) string {
	seen := map[rune]bool{}
	var b strings.Builder
	for _, r := range s {
		if !seen[r] {
			seen[r] = true
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

func containsInt(list []int, want int) bool {
	for _, n := range list {
		if n == want {
			return true
		}
	}
	return false
}

func joinInts(list []int) string {
	parts := make([]string, len(list))
	for i, n := range list {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
