// Package theme holds the Lip Gloss styles shared by haven's screens.
package theme

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

// Theme is a set of styles adapted to a light or dark terminal background.
type Theme struct {
	IsDark bool

	Accent  color.Color
	Muted   color.Color
	Soft    color.Color // secondary text that must stay readable
	Subtle  color.Color
	Text    color.Color
	Danger  color.Color
	Warning color.Color
	Success color.Color

	Title    lipgloss.Style
	Faint    lipgloss.Style
	Label    lipgloss.Style
	Selected lipgloss.Style
	Border   lipgloss.Style
	Tag      lipgloss.Style
	Error    lipgloss.Style
	Status   lipgloss.Style
}

// New builds a theme for a dark or light background.
func New(isDark bool) Theme {
	ld := lipgloss.LightDark(isDark)
	t := Theme{
		IsDark:  isDark,
		Accent:  ld(lipgloss.Color("#5A4FCF"), lipgloss.Color("#9D8CFF")),
		Muted:   ld(lipgloss.Color("#6B6B6B"), lipgloss.Color("#8A8A8A")),
		Soft:    ld(lipgloss.Color("#3F3F46"), lipgloss.Color("#C4C4CC")),
		Subtle:  ld(lipgloss.Color("#D0D0D0"), lipgloss.Color("#3A3A3A")),
		Text:    ld(lipgloss.Color("#1A1A1A"), lipgloss.Color("#E6E6E6")),
		Danger:  ld(lipgloss.Color("#C62828"), lipgloss.Color("#FF5F5F")),
		Warning: ld(lipgloss.Color("#B26A00"), lipgloss.Color("#FFB347")),
		Success: ld(lipgloss.Color("#2E7D32"), lipgloss.Color("#5FD787")),
	}
	t.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(t.Accent).Padding(0, 1)
	t.Faint = lipgloss.NewStyle().Foreground(t.Muted)
	t.Label = lipgloss.NewStyle().Foreground(t.Muted).Width(11)
	t.Selected = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	t.Border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Subtle)
	t.Tag = lipgloss.NewStyle().Foreground(t.Accent)
	t.Error = lipgloss.NewStyle().Foreground(t.Danger)
	t.Status = lipgloss.NewStyle().Foreground(t.Success)
	return t
}

// Help returns key-help styles that stay legible on any background,
// including translucent terminals: bold accent keys, soft descriptions.
func (t Theme) Help() help.Styles {
	key := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	desc := lipgloss.NewStyle().Foreground(t.Soft)
	sep := lipgloss.NewStyle().Foreground(t.Muted)
	return help.Styles{
		Ellipsis:       sep,
		ShortKey:       key,
		ShortDesc:      desc,
		ShortSeparator: sep,
		FullKey:        key,
		FullDesc:       desc,
		FullSeparator:  sep,
	}
}

// EnvColor returns the badge colour for an environment name.
func (t Theme) EnvColor(env string) color.Color {
	switch strings.ToLower(env) {
	case "production", "prod", "live":
		return t.Danger
	case "staging", "stage", "acceptance", "test":
		return t.Warning
	case "":
		return nil
	default:
		return t.Success
	}
}

// Badge renders an environment badge, or "" when env is empty.
func (t Theme) Badge(env string) string {
	c := t.EnvColor(env)
	if c == nil {
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(c).Bold(true).
		Padding(0, 1).Render(strings.ToUpper(env))
}

// IsProduction reports whether env should get the extra-care treatment
// (confirmations on destructive operations).
func IsProduction(env string) bool {
	switch strings.ToLower(env) {
	case "production", "prod", "live":
		return true
	}
	return false
}
