package files

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/theme"
)

// standalone runs the file manager as its own program.
type standalone struct {
	m      Model
	result CloseMsg
}

func (s standalone) Init() tea.Cmd { return tea.Batch(tea.RequestBackgroundColor, s.m.Init()) }

func (s standalone) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		s.m.SetTheme(theme.New(msg.IsDark()))
		return s, nil
	case tea.WindowSizeMsg:
		s.m.SetSize(msg.Width, msg.Height)
		return s, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			s.result = CloseMsg{Host: s.m.cfg.Host, LocalDir: s.m.panes[left].cwd, RemoteDir: s.m.panes[right].cwd}
			s.m.Close()
			return s, tea.Quit
		}
	case CloseMsg:
		s.result = msg
		return s, tea.Quit
	}
	var cmd tea.Cmd
	s.m, cmd = s.m.Update(msg)
	return s, cmd
}

func (s standalone) View() tea.View {
	v := tea.NewView(s.m.View())
	v.AltScreen = true
	v.WindowTitle = "rootnet · " + s.m.cfg.Host.Name
	return v
}

// Run shows the file manager full screen until the user quits and returns
// the final directories.
func Run(ctx context.Context, cfg Config) (CloseMsg, error) {
	final, err := tea.NewProgram(standalone{m: New(ctx, theme.New(true), cfg)}, tea.WithContext(ctx)).Run()
	if err != nil {
		return CloseMsg{}, err
	}
	return final.(standalone).result, nil
}
