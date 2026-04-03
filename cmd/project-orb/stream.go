package main

import (
	"context"
	"errors"
	"time"

	"project-orb/internal/coach"

	tea "github.com/charmbracelet/bubbletea"
)

var errRunnerNotConfigured = errors.New("stream runner is not configured")

type streamResult struct {
	session  coach.SessionContext
	canceled bool
}

type streamRunner interface {
	Start(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)
}

type coachRunner struct {
	service *coach.Service
}

func newCoachRunner(service *coach.Service) streamRunner {
	if service == nil {
		return nil
	}

	return coachRunner{service: service}
}

func (r coachRunner) Start(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return startStreaming(prompt, session, r.service)
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
		return streamDoneMsg{session: result.session, canceled: result.canceled}
	}
}

func startStreaming(prompt string, session coach.SessionContext, service *coach.Service) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	tokenCh := make(chan string)
	errCh := make(chan error, 1)
	doneCh := make(chan streamResult, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer close(doneCh)

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

		analysis, err := service.GenerateAnalysisWithContext(ctx, prompt, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- streamResult{session: session, canceled: true}
				return
			}
			errCh <- err
			return
		}

		responseCh, responseErrCh, err := service.GenerateResponseWithContext(ctx, prompt, analysis, session)
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
