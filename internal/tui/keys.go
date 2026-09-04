package tui

import "charm.land/bubbles/v2/key"

// keyMap is every binding the interface understands. Keeping them in one
// place means the help screen cannot drift out of step with the behaviour.
type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Toggle    key.Binding
	ExpandAll key.Binding
	Doctor    key.Binding
	Loudness  key.Binding
	Rename    key.Binding
	Apply     key.Binding
	Undo      key.Binding
	Sort      key.Binding
	Profile   key.Binding
	Filter    key.Binding
	Export    key.Binding
	Rescan    key.Binding
	Help      key.Binding
	Back      key.Binding
	Quit      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+b"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+f"),
			key.WithHelp("pgdn", "page down"),
		),
		Top: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("G", "bottom"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("enter", " "),
			key.WithHelp("⏎", "expand"),
		),
		ExpandAll: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "expand all"),
		),
		Doctor: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "doctor"),
		),
		Loudness: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "loudness"),
		),
		Rename: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "rename plan"),
		),
		Apply: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "commit"),
		),
		Undo: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "undo"),
		),
		Sort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "sort"),
		),
		Profile: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "profile"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Export: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "export"),
		),
		Rescan: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "rescan"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}
