package files

import "charm.land/bubbles/v2/key"

// KeyMap lists the file manager bindings.
type KeyMap struct {
	Up, Down, PageUp, PageDown, Home, End key.Binding
	Switch, Open, Parent, GoHome          key.Binding
	Mark, MarkAll                         key.Binding
	Copy, Mkdir, Rename, Delete           key.Binding
	Hidden, Sort, Filter, Refresh         key.Binding
	CancelTransfers, Help, Quit           key.Binding
}

// DefaultKeyMap returns the default bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),

		Switch: key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "switch")),
		Open:   key.NewBinding(key.WithKeys("enter", "right", "l"), key.WithHelp("enter", "open")),
		Parent: key.NewBinding(key.WithKeys("backspace", "left", "h"), key.WithHelp("⌫", "up a dir")),
		GoHome: key.NewBinding(key.WithKeys("~"), key.WithHelp("~", "home dir")),

		Mark:    key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "mark")),
		MarkAll: key.NewBinding(key.WithKeys("*", "ctrl+a"), key.WithHelp("*", "mark all")),

		Copy:   key.NewBinding(key.WithKeys("c", "f5"), key.WithHelp("c", "copy →")),
		Mkdir:  key.NewBinding(key.WithKeys("m", "f7"), key.WithHelp("m", "mkdir")),
		Rename: key.NewBinding(key.WithKeys("r", "f6"), key.WithHelp("r", "rename")),
		Delete: key.NewBinding(key.WithKeys("d", "delete", "f8"), key.WithHelp("d", "delete")),

		Hidden:  key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
		Sort:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Refresh: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),

		CancelTransfers: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "cancel transfers")),
		Help:            key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		Quit:            key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "back")),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Switch, k.Mark, k.Copy, k.Mkdir, k.Rename, k.Delete, k.Hidden, k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End},
		{k.Switch, k.Open, k.Parent, k.GoHome, k.Refresh},
		{k.Mark, k.MarkAll, k.Copy, k.Mkdir, k.Rename, k.Delete},
		{k.Hidden, k.Sort, k.Filter, k.CancelTransfers, k.Help, k.Quit},
	}
}
