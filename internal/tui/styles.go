package tui

import "charm.land/lipgloss/v2"

// Theme holds every style the interface uses.
//
// Colours are resolved through lipgloss.LightDark against the terminal's
// actual background, which Bubble Tea reports at startup. Hardcoding one
// palette makes a tool unreadable for half its users.
type Theme struct {
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Header   lipgloss.Style
	Row      lipgloss.Style
	RowDim   lipgloss.Style
	Selected lipgloss.Style
	Number   lipgloss.Style
	Warn     lipgloss.Style
	Error    lipgloss.Style
	Good     lipgloss.Style
	Accent   lipgloss.Style
	Faint    lipgloss.Style
	StatusOK lipgloss.Style
	StatusNo lipgloss.Style
	Bar      lipgloss.Style
	Key      lipgloss.Style
	Add      lipgloss.Style
	Del      lipgloss.Style
}

// NewTheme builds the palette for a light or dark terminal.
func NewTheme(isDark bool) Theme {
	c := lipgloss.LightDark(isDark)

	// Each pair is (light-terminal colour, dark-terminal colour); c picks one.
	var (
		fg     = c(lipgloss.Color("#1f2328"), lipgloss.Color("#e6edf3"))
		faint  = c(lipgloss.Color("#6e7781"), lipgloss.Color("#8b949e"))
		accent = c(lipgloss.Color("#0969da"), lipgloss.Color("#58a6ff"))
		warn   = c(lipgloss.Color("#9a6700"), lipgloss.Color("#d29922"))
		danger = c(lipgloss.Color("#cf222e"), lipgloss.Color("#f85149"))
		good   = c(lipgloss.Color("#1a7f37"), lipgloss.Color("#3fb950"))
		selBg  = c(lipgloss.Color("#ddf4ff"), lipgloss.Color("#163356"))
	)

	base := lipgloss.NewStyle().Foreground(fg)

	return Theme{
		Title:    lipgloss.NewStyle().Foreground(fg).Bold(true),
		Subtitle: lipgloss.NewStyle().Foreground(faint),
		Header:   lipgloss.NewStyle().Foreground(faint).Bold(true),
		Row:      base,
		RowDim:   lipgloss.NewStyle().Foreground(faint),
		Selected: lipgloss.NewStyle().Foreground(fg).Background(selBg).Bold(true),
		Number:   lipgloss.NewStyle().Foreground(accent),
		Warn:     lipgloss.NewStyle().Foreground(warn),
		Error:    lipgloss.NewStyle().Foreground(danger),
		Good:     lipgloss.NewStyle().Foreground(good),
		Accent:   lipgloss.NewStyle().Foreground(accent),
		Faint:    lipgloss.NewStyle().Foreground(faint),
		StatusOK: lipgloss.NewStyle().Foreground(good),
		StatusNo: lipgloss.NewStyle().Foreground(danger),
		Bar:      lipgloss.NewStyle().Foreground(accent),
		Key:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		Add:      lipgloss.NewStyle().Foreground(good),
		Del:      lipgloss.NewStyle().Foreground(danger),
	}
}
