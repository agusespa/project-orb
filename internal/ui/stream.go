package ui

import (
	"time"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// AgentRunner wraps agent.Runner to integrate with Bubbletea
// It converts channel operations into tea.Cmd messages
type AgentRunner struct {
	Service *agent.Service
}

// Start begins streaming and returns both tea commands and channels
func (r AgentRunner) Start(prompt string, session agent.SessionContext) (tea.Cmd, agent.StreamChannels) {
	runner := agent.Runner{Service: r.Service}
	channels := runner.Start(prompt, session)

	return tea.Batch(
		waitForToken(channels.TokenCh),
		waitForErr(channels.ErrCh),
		waitForStreamResult(channels.DoneCh),
	), channels
}

// Tea command helpers - convert channel operations to Bubbletea messages

func spinnerTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
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

func waitForStreamResult(ch <-chan agent.StreamResult) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return doneChannelClosedMsg{}
		}
		return result
	}
}
