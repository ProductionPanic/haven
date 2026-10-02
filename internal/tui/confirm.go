package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/theme"
)

// confirm is a small yes/no dialog. "No" is selected by default.
type confirm struct {
	theme             theme.Theme
	title, body, verb string
	yes               bool
}

func newConfirm(t theme.Theme, title, body, verb string) *confirm {
	return &confirm{theme: t, title: title, body: body, verb: verb}
}

// update returns done=true once the user decided, with their answer.
func (c *confirm) update(k tea.KeyPressMsg) (done, yes bool) {
	switch k.String() {
	case "left", "right", "h", "l", "tab", "shift+tab":
		c.yes = !c.yes
	case "y", "Y":
		return true, true
	case "n", "N", "esc", "q", "ctrl+c":
		return true, false
	case "enter":
		return true, c.yes
	}
	return false, false
}

func (c *confirm) view() string {
	t := c.theme
	btn := lipgloss.NewStyle().Padding(0, 2)
	active := btn.Foreground(lipgloss.Color("#FFFFFF")).Background(t.Danger).Bold(true)
	inactive := btn.Foreground(t.Muted).Background(t.Subtle)
	yes, no := inactive.Render(c.verb), active.Background(t.Accent).Render("Cancel")
	if c.yes {
		yes, no = active.Render(c.verb), inactive.Render("Cancel")
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render(c.title),
		"",
		c.body,
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, yes, "  ", no),
		"",
		t.Faint.Render("y/n · ←/→ · enter"),
	)
	return t.Border.BorderForeground(t.Danger).Padding(1, 2).Render(content)
}
