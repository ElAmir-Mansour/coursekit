package export

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether the file is an interactive terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// TerminalWidth returns the usable width of the terminal, falling back to a
// conventional 100 columns when the size cannot be determined, as happens when
// output is piped.
func TerminalWidth(f *os.File) int {
	if f == nil {
		return 100
	}
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 {
		return 100
	}
	if w < 40 {
		return 40
	}
	return w
}
