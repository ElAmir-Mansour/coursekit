// Package rename builds, validates, applies and reverses batch renames of
// course folders and lesson files.
//
// Every mutation goes through a plan that can be inspected first, and every
// applied plan writes a journal that can be replayed backwards. Renaming
// someone's only copy of a recorded course is not something to do optimistically.
package rename

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// DefaultChapterTemplate zero-pads the number, which is what actually fixes
// ordering in Finder, in web uploaders, and in any plain alphabetical sort.
const DefaultChapterTemplate = "{n:02} - {title}"

// DefaultLessonTemplate keeps the chapter-relative numbering a creator already
// uses, rather than renumbering their course out from under them.
const DefaultLessonTemplate = "{ch}.{lesson:02} - {title}{ext}"

// Op is a single rename of one directory entry, always within its own parent.
type Op struct {
	From  string `json:"from"`
	To    string `json:"to"`
	IsDir bool   `json:"is_dir"`
}

// Base is the old name without its directory.
func (o Op) Base() string { return filepath.Base(o.From) }

// NewBase is the new name without its directory.
func (o Op) NewBase() string { return filepath.Base(o.To) }

// Skip records something deliberately left alone, and why. Surfacing these
// matters: a plan that silently ignores half a course looks like it worked.
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Options controls how a plan is built.
type Options struct {
	// ChapterTemplate names chapter folders. Empty disables chapter renaming.
	ChapterTemplate string
	// LessonTemplate names lesson files. Empty disables lesson renaming,
	// which is the default: fixing folder order is the safe, high-value half.
	LessonTemplate string
	// StripPrefix removes a shared leading tag such as "goCourse-" from
	// lesson titles.
	StripPrefix bool
}

// Plan is a proposed set of renames.
type Plan struct {
	Root  string `json:"root"`
	Ops   []Op   `json:"ops"`
	Skips []Skip `json:"skips,omitempty"`
}

// Empty reports whether the plan would change nothing.
func (p *Plan) Empty() bool { return len(p.Ops) == 0 }

// Build produces a rename plan for a scanned course.
//
// Ops are ordered deepest-path-first so that files are renamed before the
// folders containing them. Renaming a parent first would invalidate every
// child path still waiting to be processed.
func Build(course *model.Course, opts Options) (*Plan, error) {
	if opts.ChapterTemplate == "" && opts.LessonTemplate == "" {
		return nil, fmt.Errorf("nothing to do: no chapter or lesson template given")
	}

	plan := &Plan{Root: course.Root}

	for _, ch := range course.Chapters {
		if opts.LessonTemplate != "" {
			planLessons(plan, ch, opts)
		}
		if opts.ChapterTemplate != "" && !ch.IsRoot {
			planChapter(plan, ch, opts)
		}
	}

	// Deepest first: children before their parents.
	sort.SliceStable(plan.Ops, func(i, j int) bool {
		di, dj := depth(plan.Ops[i].From), depth(plan.Ops[j].From)
		if di != dj {
			return di > dj
		}
		return plan.Ops[i].From < plan.Ops[j].From
	})

	return plan, nil
}

func planChapter(plan *Plan, ch *model.Chapter, opts Options) {
	// A folder with no detectable number cannot be given one without
	// inventing a position for it, so leave it alone and say so.
	switch ch.Name.Confidence {
	case model.ConfNone:
		plan.Skips = append(plan.Skips, Skip{
			Path:   ch.Display,
			Reason: "no chapter number found in the folder name",
		})
		return
	case model.ConfWeak:
		plan.Skips = append(plan.Skips, Skip{
			Path: ch.Display,
			Reason: fmt.Sprintf("number sits behind an unrecognised word (%q), so it may not be a chapter number",
				ch.Name.Keyword),
		})
		return
	}

	// A folder like "Chap 1" carries no description, so the template's title
	// placeholder renders empty and tidyName trims the dangling separator,
	// leaving a clean "01". Substituting the keyword instead would produce
	// the nonsense "01 - chap".
	newName := render(opts.ChapterTemplate, vars{
		N:     ch.Name.Order,
		Ch:    ch.Name.Order,
		Title: ch.Name.Title,
		Raw:   ch.Name.Raw,
	})
	newName = tidyName(newName)
	if newName == "" {
		plan.Skips = append(plan.Skips, Skip{Path: ch.Display, Reason: "template produced an empty name"})
		return
	}

	if newName == ch.Name.Raw {
		return
	}

	parent := filepath.Dir(ch.Dir)
	plan.Ops = append(plan.Ops, Op{
		From:  ch.Dir,
		To:    filepath.Join(parent, newName),
		IsDir: true,
	})
}

func planLessons(plan *Plan, ch *model.Chapter, opts Options) {
	if len(ch.Lessons) == 0 {
		return
	}

	titles := make([]string, len(ch.Lessons))
	for i, f := range ch.Lessons {
		titles[i] = f.Lesson.Title
	}
	if opts.StripPrefix {
		if _, stripped := model.StripCommonPrefix(titles); stripped != nil {
			titles = stripped
		}
	}

	for i, f := range ch.Lessons {
		lessonNo := f.Lesson.Number.Index
		if !f.Lesson.Number.Found {
			// Without existing numbering there is nothing to preserve, so
			// fall back to the file's sorted position.
			lessonNo = i + 1
		}
		chapterNo := ch.Name.Order
		if f.Lesson.Number.Found {
			chapterNo = f.Lesson.Number.Chapter
		}
		if chapterNo == model.Unnumbered {
			plan.Skips = append(plan.Skips, Skip{
				Path:   f.Rel,
				Reason: "no chapter number available for this lesson",
			})
			continue
		}

		newName := render(opts.LessonTemplate, vars{
			N:      lessonNo,
			I:      i + 1,
			Ch:     chapterNo,
			Lesson: lessonNo,
			Title:  titles[i],
			Ext:    strings.ToLower(filepath.Ext(f.Name)),
			Raw:    f.Name,
		})
		newName = tidyName(newName)
		if newName == "" || newName == f.Name {
			continue
		}

		plan.Ops = append(plan.Ops, Op{
			From: f.Path,
			To:   filepath.Join(filepath.Dir(f.Path), newName),
		})
	}
}

// vars are the values a template can reference.
type vars struct {
	N      int
	I      int
	Ch     int
	Lesson int
	Title  string
	Ext    string
	Raw    string
}

// placeholderRe matches {name} and {name:02} / {name:02d}.
var placeholderRe = regexp.MustCompile(`\{(\w+)(?::0?(\d+)d?)?\}`)

// render substitutes template placeholders. An unknown placeholder is left
// untouched rather than silently deleted, so a typo in a template is visible
// in the plan instead of quietly producing wrong names.
func render(tmpl string, v vars) string {
	return placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		groups := placeholderRe.FindStringSubmatch(m)
		name, widthStr := groups[1], groups[2]

		width := 0
		if widthStr != "" {
			width, _ = strconv.Atoi(widthStr)
		}

		pad := func(n int) string {
			s := strconv.Itoa(n)
			for len(s) < width {
				s = "0" + s
			}
			return s
		}

		switch strings.ToLower(name) {
		case "n":
			return pad(v.N)
		case "i":
			return pad(v.I)
		case "ch", "chapter":
			return pad(v.Ch)
		case "lesson":
			return pad(v.Lesson)
		case "title":
			return v.Title
		case "ext":
			return v.Ext
		case "raw":
			return v.Raw
		default:
			return m
		}
	})
}

var (
	// Characters that are legal on macOS but break on Windows, in URLs, or in
	// zip archives.
	reservedRe = regexp.MustCompile(`[<>:"|?*\\/]`)
	spacesRe   = regexp.MustCompile(`\s+`)
	dashRunRe  = regexp.MustCompile(`(?:\s*-\s*){2,}`)
)

// tidyName makes a rendered name safe and tidy: reserved characters removed,
// whitespace collapsed, and the separator debris left behind by empty
// placeholders cleaned up, so an untitled chapter becomes "03" rather than
// "03 - ".
func tidyName(s string) string {
	ext := filepath.Ext(s)
	stem := strings.TrimSuffix(s, ext)

	stem = reservedRe.ReplaceAllString(stem, "")
	stem = spacesRe.ReplaceAllString(stem, " ")
	stem = dashRunRe.ReplaceAllString(stem, " - ")
	stem = strings.Trim(stem, " -_.")

	if stem == "" {
		return ""
	}
	return stem + ext
}

func depth(path string) int {
	return strings.Count(filepath.Clean(path), string(filepath.Separator))
}
