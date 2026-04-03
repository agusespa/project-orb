package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tokenMsg string
type streamErrMsg struct {
	err error
}
type streamDoneMsg struct{}
type tokenChannelClosedMsg struct{}
type errChannelClosedMsg struct{}
type doneChannelClosedMsg struct{}

type model struct {
	width      int
	height     int
	input      string
	output     strings.Builder
	streaming  bool
	completed  bool
	err        error
	tokenCh    <-chan string
	errCh      <-chan error
	doneCh     <-chan struct{}
	outputBox  lipgloss.Style
	promptBox  lipgloss.Style
	helpStyle  lipgloss.Style
	errorStyle lipgloss.Style
}

func initialModel() model {
	return model{
		outputBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("12")).
			Padding(1, 2),
		promptBox: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("10")),
		helpStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")),
		errorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func waitForToken(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		token, ok := <-ch
		if !ok {
			return tokenChannelClosedMsg{}
		}
		return tokenMsg(token)
	}
}

func waitForErr(ch <-chan error) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-ch
		if !ok {
			return errChannelClosedMsg{}
		}
		return streamErrMsg{err: err}
	}
}

func waitForDone(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		if !ok {
			return doneChannelClosedMsg{}
		}
		return streamDoneMsg{}
	}
}

func startStreaming(prompt string) (tea.Cmd, <-chan string, <-chan error, <-chan struct{}) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)
	doneCh := make(chan struct{}, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer close(doneCh)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if err := StreamCompletion(ctx, prompt, tokenCh); err != nil {
			errCh <- err
			return
		}

		doneCh <- struct{}{}
	}()

	return tea.Batch(waitForToken(tokenCh), waitForErr(errCh), waitForDone(doneCh)), tokenCh, errCh, doneCh
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.streaming || strings.TrimSpace(m.input) == "" {
				return m, nil
			}

			m.err = nil
			m.completed = false
			m.streaming = true
			m.output.Reset()

			cmd, tokenCh, errCh, doneCh := startStreaming(strings.TrimSpace(m.input))
			m.tokenCh = tokenCh
			m.errCh = errCh
			m.doneCh = doneCh
			return m, cmd
		case tea.KeyBackspace, tea.KeyDelete:
			if m.streaming || len(m.input) == 0 {
				return m, nil
			}
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
			return m, nil
		default:
			if m.streaming {
				return m, nil
			}
			if msg.Type == tea.KeyRunes {
				m.input += string(msg.Runes)
			}
			return m, nil
		}
	case tokenMsg:
		m.output.WriteString(string(msg))
		return m, waitForToken(m.tokenCh)
	case streamErrMsg:
		m.streaming = false
		m.completed = false
		m.err = msg.err
		return m, nil
	case streamDoneMsg:
		m.streaming = false
		m.completed = true
		return m, nil
	case tokenChannelClosedMsg, errChannelClosedMsg, doneChannelClosedMsg:
		return m, nil
	}

	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	title := "Local AI Life Coach"
	if m.streaming {
		title += " (streaming...)"
	} else if m.completed {
		title += " (complete)"
	}

	b.WriteString(title)
	b.WriteString("\n\n")

	b.WriteString(m.promptBox.Render("Your prompt"))
	b.WriteString("\n")
	b.WriteString(m.input)
	if !m.streaming {
		b.WriteString("█")
	}
	b.WriteString("\n")
	b.WriteString(m.helpStyle.Render("Press Enter to send. Press Esc or Ctrl+C to quit."))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(m.errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	output := m.output.String()
	if output == "" {
		if m.streaming {
			output = "Waiting for the first tokens..."
		} else {
			output = "Agent output will appear here."
		}
	}

	boxWidth := m.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	box := m.outputBox.Width(boxWidth).Render(output)
	b.WriteString(box)

	return b.String()
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("failed to start UI: %v\n", err)
	}
}
