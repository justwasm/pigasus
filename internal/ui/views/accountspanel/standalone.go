package accountspanel

import (
	"context"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/palette"
	"golang.org/x/term"
)

// StandaloneModel wraps a Panel as a top-level Bubbletea Model.
type StandaloneModel struct {
	panel  *Panel
	width  int
	height int
}

// QuotaDoneMsg is emitted when a quota query completes.
type QuotaDoneMsg struct {
	Email   string
	Summary any
	Err     error
}

// NewStandaloneModel constructs a StandaloneModel with the given seams.
func NewStandaloneModel(seams Seams) *StandaloneModel {
	if seams.Close == nil {
		seams.Close = func() tea.Cmd { return tea.Quit }
	}
	if seams.QuotaCmd == nil {
		seams.QuotaCmd = func(email string) tea.Cmd {
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), accounts.QuotaTimeout)
				defer cancel()
				summary, err := accounts.FetchQuota(ctx, email)
				return QuotaDoneMsg{Email: email, Summary: summary, Err: err}
			}
		}
	}
	p := New(seams)
	return &StandaloneModel{panel: p}
}

// Panel returns the underlying Panel.
func (m *StandaloneModel) Panel() *Panel {
	if m == nil {
		return nil
	}
	return m.panel
}

func (m *StandaloneModel) Init() tea.Cmd {
	if m == nil || m.panel == nil {
		return nil
	}
	m.panel.Reload()
	return m.panel.QueueQuota()
}

func (m *StandaloneModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil || m.panel == nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case DismissedMsg:
		return m, tea.Quit
	case QuotaDoneMsg:
		return m, m.panel.QuotaDone(msg.Email, msg.Summary != nil, msg.Err)
	case tea.KeyPressMsg:
		k := tea.Key(msg)
		// When not in inline-edit mode on Pool tab:
		if !m.panel.pool.editing {
			switch k.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		}
		handled, cmd := m.panel.HandleKey(k)
		if handled {
			return m, cmd
		}
	}
	return m, nil
}

func (m *StandaloneModel) View() tea.View {
	if m == nil || m.panel == nil {
		return tea.NewView("")
	}
	restore := palette.PushScheme("terminal")
	defer restore()

	lines := m.panel.Lines()
	v := tea.NewView(strings.Join(lines, "\n") + "\n")
	v.AltScreen = true
	return v
}

// RunStandalone runs the accounts panel as a full-screen interactive Bubbletea application.
func RunStandalone(in io.Reader, out io.Writer, seams Seams) error {
	m := NewStandaloneModel(seams)
	var opts []tea.ProgramOption
	if in != nil {
		opts = append(opts, tea.WithInput(in))
	}
	if out != nil {
		opts = append(opts, tea.WithOutput(out))
	}

	prog := tea.NewProgram(m, opts...)
	if f, ok := out.(*os.File); ok {
		if cols, rows, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
			m.width, m.height = cols, rows
		}
	}
	_, err := prog.Run()
	return err
}
