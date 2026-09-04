// Package export renders a scanned course and its findings for humans and for
// other programs.
package export

import (
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Palette holds the escape sequences used for terminal output. Every field is
// empty when colour is disabled, so the same code path produces clean text
// when output is piped to a file.
type Palette struct {
	Reset  string
	Bold   string
	Dim    string
	Red    string
	Yellow string
	Green  string
	Blue   string
	Cyan   string
}

// NoColor is the palette used when writing to a pipe or when the user has
// asked for plain output.
var NoColor = Palette{}

// Color is the palette used on a terminal.
var Color = Palette{
	Reset:  "\x1b[0m",
	Bold:   "\x1b[1m",
	Dim:    "\x1b[2m",
	Red:    "\x1b[31m",
	Yellow: "\x1b[33m",
	Green:  "\x1b[32m",
	Blue:   "\x1b[34m",
	Cyan:   "\x1b[36m",
}

// PaletteFor decides whether to colour output.
//
// The rule is deliberately conservative: colour only when writing to a real
// terminal. Piping into a file or another program must produce plain text,
// because escape sequences in a report or a diff are worse than useless.
//
// NO_COLOR is honoured because it is the cross-tool convention, and
// CLICOLOR_FORCE covers the case where someone genuinely wants colour through
// a pipe.
func PaletteFor(f *os.File, forced bool) Palette {
	if forced || os.Getenv("CLICOLOR_FORCE") != "" {
		return Color
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return NoColor
	}
	if os.Getenv("TERM") == "dumb" {
		return NoColor
	}
	if !IsTerminal(f) {
		return NoColor
	}
	return Color
}

// width measures how many terminal columns a string occupies, which is not the
// same as its length in runes. Course filenames are routinely non-ASCII, and
// misjudging their width breaks every column in the table.
func width(s string) int { return runewidth.StringWidth(s) }

// pad right-pads a string to a column width.
func pad(s string, n int) string {
	w := width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// padLeft left-pads a string to a column width, for numeric columns.
func padLeft(s string, n int) string {
	w := width(s)
	if w >= n {
		return s
	}
	return strings.Repeat(" ", n-w) + s
}

// truncate shortens a string to a display width, marking the cut with an
// ellipsis so it is obvious something was removed.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return runewidth.Truncate(s, n-1, "") + "…"
}
