package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

var errRunnerNotConfigured = errors.New("stream runner is not configured")

type streamResult struct {
	session  agent.SessionContext
	canceled bool
}

type streamRunner interface {
	Start(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)
	StartWelcome(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)
}

type runnerFactory func(mode agent.Mode) (streamRunner, error)

type agentRunner struct {
	service *agent.Service
}

func newAgentRunner(service *agent.Service) streamRunner {
	if service == nil {
		return nil
	}

	return agentRunner{service: service}
}

func (r agentRunner) Start(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return startStreaming(prompt, session, r.service)
}

func (r agentRunner) StartWelcome(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return startWelcomeStreaming(session, r.service)
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

func waitForStreamResult(ch <-chan streamResult) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return doneChannelClosedMsg{}
		}
		return streamDoneMsg(result)
	}
}

func startStreaming(prompt string, session agent.SessionContext, service *agent.Service) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	tokenCh := make(chan string, 10) // Buffered to prevent blocking on cancel
	errCh := make(chan error, 1)
	doneCh := make(chan streamResult, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer close(doneCh)
		defer slog.Debug("Stream goroutine cleaned up")

		if service == nil {
			errCh <- errRunnerNotConfigured
			return
		}

		if err := service.PrepareSession(ctx, &session); err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- streamResult{session: session, canceled: true}
				return
			}
			errCh <- err
			return
		}

		slog.Info("Starting generation pipeline", "prompt", prompt, "source", "User")

		analysis, err := service.GenerateAnalysis(ctx, prompt, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- streamResult{session: session, canceled: true}
				return
			}
			errCh <- err
			return
		}

		slog.Debug("Thinking stage", "prompt", prompt, "analysis", analysis)

		responseCh, responseErrCh, err := service.GenerateResponse(ctx, prompt, analysis, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- streamResult{session: session, canceled: true}
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
				doneCh <- streamResult{session: session, canceled: true}
				return
			case token, ok := <-responseCh:
				if !ok {
					responseCh = nil
					continue
				}
				select {
				case <-ctx.Done():
					doneCh <- streamResult{session: session, canceled: true}
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

		doneCh <- streamResult{session: session}
	}()

	return tea.Batch(waitForToken(tokenCh), waitForErr(errCh), waitForStreamResult(doneCh)), tokenCh, errCh, doneCh, cancel
}

func startWelcomeStreaming(session agent.SessionContext, service *agent.Service) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	tokenCh := make(chan string, 10) // Buffered to prevent blocking on cancel
	errCh := make(chan error, 1)
	doneCh := make(chan streamResult, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer close(doneCh)
		defer slog.Debug("Welcome stream goroutine cleaned up")

		if service == nil {
			errCh <- errRunnerNotConfigured
			return
		}

		slog.Info("Generating welcome message...", "source", "System")

		responseCh, responseErrCh, err := service.GenerateWelcome(ctx, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- streamResult{session: session, canceled: true}
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
				doneCh <- streamResult{session: session, canceled: true}
				return
			case token, ok := <-responseCh:
				if !ok {
					responseCh = nil
					continue
				}
				select {
				case <-ctx.Done():
					doneCh <- streamResult{session: session, canceled: true}
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

		doneCh <- streamResult{session: session}
	}()

	return tea.Batch(waitForToken(tokenCh), waitForErr(errCh), waitForStreamResult(doneCh)), tokenCh, errCh, doneCh, cancel
}
