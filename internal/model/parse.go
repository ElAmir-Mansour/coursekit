package model

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Unnumbered marks a chapter whose folder name carries no usable number. These
// sort after every numbered chapter and are surfaced rather than dropped.
const Unnumbered = -1

// chapterWords are the words creators put in front of a chapter number.
// Matching is fuzzy (see fuzzyKeyword), so "Chpater" still resolves to a
// chapter without anyone hardcoding that particular typo.
var chapterWords = []string{
	"chapter", "chap", "ch",
	"section", "sec",
	"module", "mod",
	"lecture", "lec",
	"episode", "ep",
	"part", "unit", "day", "week", "lesson", "topic",
}

var (
	// An optional leading word, then a number: "Chapter 5", "Chpater 3",
	// "03 - Intro", "7". The word is captured so it can be fuzzy-matched.
	chapterRe = regexp.MustCompile(`^(\p{L}+)?[\s\-_.]*(\d{1,4})(?:\b|_)`)

	// "1.2" style lesson numbering appearing anywhere in a filename.
	lessonRe = regexp.MustCompile(`(\d{1,3})\.(\d{1,3})`)

	// Separator debris left behind once a number is removed: " - ", "._", etc.
	leadSepRe  = regexp.MustCompile(`^[\s\-_.:|]+`)
	trailSepRe = regexp.MustCompile(`[\s\-_.:|]+$`)
)

// Chapter numbering confidence, so the UI can mark guesses honestly.
type Confidence uint8

const (
	// ConfNone means no number was found at all.
	ConfNone Confidence = iota
	// ConfWeak means a number was found behind an unrecognized word
	// ("Bonus 2"), so it is reported but not trusted for ordering.
	ConfWeak
	// ConfStrong means a bare number or a recognized chapter word.
	ConfStrong
)

// ChapterName is the parsed form of a chapter directory name.
type ChapterName struct {
	Raw        string // exactly as it sits on disk, including any stray spaces
	Order      int    // extracted number, or Unnumbered
	Title      string // the descriptive remainder, separators stripped
	Keyword    string // the leading word as written, e.g. "Chpater"
	Canonical  string // the keyword it fuzzy-matched, e.g. "chapter"
	Confidence Confidence
}

// Untidy reports whether the raw name would cause trouble outside macOS —
// leading/trailing whitespace survives on APFS but breaks web uploaders and
// Windows, and is invisible in Finder.
func (c ChapterName) Untidy() bool {
	return c.Raw != strings.TrimSpace(c.Raw)
}

// Misspelled reports whether the chapter word was recognized only after fuzzy
// matching, i.e. it is a typo of a real keyword.
func (c ChapterName) Misspelled() bool {
	if c.Keyword == "" || c.Canonical == "" {
		return false
	}
	return !strings.EqualFold(c.Keyword, c.Canonical)
}

// ParseChapter extracts ordering information from a chapter directory name.
//
// Whitespace is trimmed first, so "Chapter 5 Deploys " parses identically to
// its tidy form while Untidy still reports the difference.
func ParseChapter(raw string) ChapterName {
	out := ChapterName{Raw: raw, Order: Unnumbered, Confidence: ConfNone}

	name := strings.TrimSpace(raw)
	if name == "" {
		return out
	}

	m := chapterRe.FindStringSubmatch(name)
	if m == nil {
		out.Title = name
		return out
	}

	n, err := strconv.Atoi(m[2])
	if err != nil {
		out.Title = name
		return out
	}

	out.Keyword = m[1]
	rest := trimSep(name[len(m[0]):])

	if out.Keyword == "" {
		// Bare leading number: "03 - Basics". Unambiguous.
		out.Order, out.Confidence = n, ConfStrong
	} else if canon, ok := fuzzyKeyword(out.Keyword); ok {
		out.Canonical = canon
		out.Order, out.Confidence = n, ConfStrong
	} else {
		// An unrecognized word in front of a number ("Swift 5 Basics").
		// Record it, but do not let it drive ordering.
		out.Order, out.Confidence = n, ConfWeak
	}

	if rest == "" && out.Keyword != "" && out.Confidence == ConfWeak {
		// "Bonus 2" — the word is the only description there is.
		rest = out.Keyword
	}
	out.Title = rest
	return out
}

// LessonNumber is an "x.y" reference parsed out of a lesson filename.
type LessonNumber struct {
	Chapter int
	Index   int
	Found   bool
}

// String renders the number as it is normally written, e.g. "3.2".
func (l LessonNumber) String() string {
	if !l.Found {
		return ""
	}
	return strconv.Itoa(l.Chapter) + "." + strconv.Itoa(l.Index)
}

// LessonName is the parsed form of a lesson filename.
type LessonName struct {
	Raw    string
	Number LessonNumber
	Title  string
}

// ParseLesson pulls "x.y" numbering and a clean title out of a filename.
//
// The last "x.y" in the name wins, because creators put the numbering at the
// end ("...Build Your Router -1.5.mp4") far more often than the start.
func ParseLesson(raw string) LessonName {
	out := LessonName{Raw: raw}

	stem := strings.TrimSuffix(raw, filepath.Ext(raw))
	ms := lessonRe.FindAllStringSubmatchIndex(stem, -1)
	if len(ms) == 0 {
		out.Title = trimSep(stem)
		return out
	}

	last := ms[len(ms)-1]
	ch, err1 := strconv.Atoi(stem[last[2]:last[3]])
	ix, err2 := strconv.Atoi(stem[last[4]:last[5]])
	if err1 != nil || err2 != nil {
		out.Title = trimSep(stem)
		return out
	}
	out.Number = LessonNumber{Chapter: ch, Index: ix, Found: true}

	// Splice the number out and reunite whatever text sat either side of it.
	before := trimSep(stem[:last[0]])
	after := trimSep(stem[last[1]:])
	switch {
	case before != "" && after != "":
		out.Title = before + " " + after
	case before != "":
		out.Title = before
	default:
		out.Title = after
	}
	return out
}

// StripCommonPrefix removes a shared leading string from every name, which is
// how a course-wide tag like "goCourse-" gets cleaned off in bulk. It returns
// the prefix it found and the shortened names, and declines to act when doing
// so would leave any name empty.
func StripCommonPrefix(names []string) (prefix string, out []string) {
	if len(names) < 2 {
		return "", names
	}

	prefix = names[0]
	for _, n := range names[1:] {
		prefix = commonPrefix(prefix, n)
		if prefix == "" {
			return "", names
		}
	}

	// Only cut at a separator, so "Chapter1"/"Chapter2" does not become "1"/"2"
	// with the meaningful word amputated.
	if i := strings.LastIndexAny(prefix, " -_.:|"); i >= 0 {
		prefix = prefix[:i+1]
	} else {
		return "", names
	}

	out = make([]string, len(names))
	for i, n := range names {
		trimmed := trimSep(strings.TrimPrefix(n, prefix))
		if trimmed == "" {
			return "", names
		}
		out[i] = trimmed
	}
	return prefix, out
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

func trimSep(s string) string {
	s = leadSepRe.ReplaceAllString(s, "")
	return trailSepRe.ReplaceAllString(s, "")
}

// fuzzyKeyword resolves a written word to a known chapter keyword, tolerating
// small misspellings. The allowance scales with word length so that short
// keywords ("ch", "ep") are not matched by unrelated two-letter words.
func fuzzyKeyword(word string) (string, bool) {
	w := strings.ToLower(word)

	for _, k := range chapterWords {
		if k == w {
			return k, true
		}
	}

	best, bestDist := "", 1<<30
	for _, k := range chapterWords {
		budget := allowedEdits(k)
		if budget == 0 {
			continue
		}
		if abs(len(k)-len(w)) > budget {
			continue
		}
		if d := levenshtein(w, k); d <= budget && d < bestDist {
			best, bestDist = k, d
		}
	}
	return best, best != ""
}

// allowedEdits is the edit budget for a keyword: none for very short words,
// one for medium, two for long ones like "chapter".
func allowedEdits(keyword string) int {
	switch {
	case len(keyword) <= 3:
		return 0
	case len(keyword) <= 5:
		return 1
	default:
		return 2
	}
}

// levenshtein returns the edit distance between two ASCII-lowercased words
// using a single rolling row.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
