package main

import (
	"context"
	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

func newProgram(m model, ctx context.Context) *tea.Program {
	m.shutdownCtx = ctx
	return tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
}

func initialModel() model {
	defaultMode := agent.DefaultMode()
	personaPath, err := agent.EnsurePersonaFile()
	coachName, nameErr := agent.LoadCoachName()
	if err == nil && nameErr != nil {
		err = nameErr
	}
	if coachName == "" {
		coachName = "Coach"
	}

	client, clientErr := agent.NewClient(agent.DefaultClientConfig())
	if err == nil && clientErr != nil {
		err = clientErr
	}

	return newModel(modelDependencies{
		runnerFactory: newRunnerFactory(client),
		currentMode:   defaultMode,
		coachName:     coachName,
		personaPath:   personaPath,
		err:           err,
	})
}

func newRunnerFactory(client *agent.Client) runnerFactory {
	return func(mode agent.Mode) (streamRunner, error) {
		service, err := agent.NewService(client, mode)
		if err != nil {
			return nil, err
		}

		return newAgentRunner(service), nil
	}
}
