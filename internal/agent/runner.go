package agent

import (
	"context"
	"errors"
	"log/slog"
)

var ErrRunnerNotConfigured = errors.New("stream runner is not configured")

// StreamResult contains the outcome of a streaming generation
type StreamResult struct {
	Session  SessionContext
	Canceled bool
}

// StreamChannels provides communication channels for async streaming operations
type StreamChannels struct {
	TokenCh <-chan string
	ErrCh   <-chan error
	DoneCh  <-chan StreamResult
	Cancel  context.CancelFunc
}

// Runner orchestrates the agent generation pipeline
type Runner struct {
	Service *Service
}

// Start begins the generation pipeline: PrepareSession → GenerateAnalysis → GenerateResponse
// Returns channels for streaming tokens, errors, and completion status
func (r Runner) Start(prompt string, session SessionContext) StreamChannels {
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
				doneCh <- StreamResult{Session: session, Canceled: true}
				return
			}
			errCh <- err
			return
		}

		slog.Info("Starting generation pipeline", "prompt", prompt, "source", "User")

		analysis, err := r.Service.GenerateAnalysis(ctx, prompt, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- StreamResult{Session: session, Canceled: true}
				return
			}
			errCh <- err
			return
		}

		slog.Debug("Thinking stage", "prompt", prompt, "analysis", analysis)

		responseCh, responseErrCh, err := r.Service.GenerateResponse(ctx, prompt, analysis, session)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				doneCh <- StreamResult{Session: session, Canceled: true}
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
				doneCh <- StreamResult{Session: session, Canceled: true}
				return
			case token, ok := <-responseCh:
				if !ok {
					responseCh = nil
					continue
				}
				select {
				case <-ctx.Done():
					doneCh <- StreamResult{Session: session, Canceled: true}
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

		doneCh <- StreamResult{Session: session}
	}()

	return channels
}
