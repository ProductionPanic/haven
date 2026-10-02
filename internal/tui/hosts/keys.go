package hosts

import "charm.land/bubbles/v2/key"

// KeyMap lists the hosts screen bindings.
type KeyMap struct {
	Up, Down, PageUp, PageDown, Home, End key.Binding

	Connect, Shell, Add, Edit, Delete, Copy key.Binding
	Tag, Group, Filter, ClearFilter         key.Binding
	Help, Quit                              key.Binding
}

// DefaultKeyMap returns the default bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k", "ctrl+p"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j", "ctrl+n"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first")),
		End:      key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last")),

		Connect: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "ssh")),
		Shell:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "ssh & return")),
		Add:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Copy:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy user@host")),

		Tag:         key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "cycle tag")),
		Group:       key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "group by server")),
		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ClearFilter: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),

		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		Quit: key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Connect, k.Filter, k.Add, k.Edit, k.Delete, k.Copy, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End},
		{k.Connect, k.Shell, k.Copy},
		{k.Add, k.Edit, k.Delete},
		{k.Filter, k.ClearFilter, k.Tag, k.Group},
		{k.Help, k.Quit},
	}
}
