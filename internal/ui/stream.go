package ui

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

var ErrRunnerNotConfigured = errors.New("stream runner is not configured")

type StreamResult struct {
	session  agent.SessionContext
	canceled bool
}

type StreamChannels struct {
	TokenCh <-chan string
	ErrCh   <-chan error
	DoneCh  <-chan StreamResult
	Cancel  context.CancelFunc
}

type StreamRunner interface {
	Start(prompt string, session agent.SessionContext) (tea.Cmd, StreamChannels)
}

type RunnerFactory func(mode agent.Mode) (StreamRunner, error)

type AgentRunner struct {
	Service *agent.Service
}

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

func waitForStreamResult(ch <-chan StreamResult) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return doneChannelClosedMsg{}
		}
		return streamDoneMsg(result)
	}
}

func (r AgentRunner) Start(prompt string, session agent.SessionContext) (tea.Cmd, StreamChannels) {
	ctx, cancel := context.WithCancel(context.Background())
	tokenCh := make(chan string, 10) // Buffered to prevent blocking on cancel
	errCh := make(chan error, 1)
	doneCh := make(chan StreamResult, 1)

	channels := StreamChannels{
		TokenCh: tokenCh,
		ErrCh:   errCh,
		DoneCh:  doneCh,
		Cancel:  cancel,
	}

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer close(doneCh)
		defer slog.Debug("Stream goroutine cleaned up")

		if r.Service == nil {
			errCh <- ErrRunnerNotConfigured
			return
		}

		if err := r.Service.PrepareSession(ctx, &session); err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- StreamResult{session: session, canceled: true}
				return
			}
			errCh <- err
			return
		}

		slog.Info("Starting generation pipeline", "prompt", prompt, "source", "User")

		analysis, err := r.Service.GenerateAnalysis(ctx, prompt, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- StreamResult{session: session, canceled: true}
				return
			}
			errCh <- err
			return
		}

		slog.Debug("Thinking stage", "prompt", prompt, "analysis", analysis)

		responseCh, responseErrCh, err := r.Service.GenerateResponse(ctx, prompt, analysis, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- StreamResult{session: session, canceled: true}
				return
			}
			errCh <- err
			return
		}

		for responseCh != nil || responseErrCh != nil {
			select {
			case <-ctx.Done():
				// Drain remaining tokens to prevent goroutine leak
				if responseCh != nil {
					for range responseCh {
					}
				}
				doneCh <- StreamResult{session: session, canceled: true}
				return
			case token, ok := <-responseCh:
				if !ok {
					responseCh = nil
					continue
				}
				select {
				case <-ctx.Done():
					doneCh <- StreamResult{session: session, canceled: true}
					return
				case tokenCh <- token:
				}
			case err, ok := <-responseErrCh:
				if !ok {
					responseErrCh = nil
					continue
				}
				errCh <- err
				return
			}
		}

		doneCh <- StreamResult{session: session}
	}()

	return tea.Batch(waitForToken(tokenCh), waitForErr(errCh), waitForStreamResult(doneCh)), channels
}
