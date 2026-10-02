package files

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/theme"
)

// question is a small modal with single-key answers.
type question struct {
	title   string
	lines   []string
	border  color.Color
	options []qOption
}

type qOption struct {
	key, label string
	run        func(m *Model) tea.Cmd // nil = just close
}

// answer returns the option for key; esc picks the last option (the
// "cancel" choice by convention).
func (q *question) answer(k string) (qOption, bool) {
	if k == "esc" && len(q.options) > 0 {
		return q.options[len(q.options)-1], true
	}
	for _, o := range q.options {
		if o.key == k {
			return o, true
		}
	}
	return qOption{}, false
}

func (q *question) view(t theme.Theme) string {
	border := q.border
	if border == nil {
		border = t.Accent
	}
	keyStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	var opts []string
	for _, o := range q.options {
		opts = append(opts, keyStyle.Render(o.key)+" "+o.label)
	}
	body := []string{lipgloss.NewStyle().Bold(true).Render(q.title), ""}
	body = append(body, q.lines...)
	body = append(body, "", strings.Join(opts, "   "))
	return t.Border.BorderForeground(border).Padding(1, 2).Render(strings.Join(body, "\n"))
}
