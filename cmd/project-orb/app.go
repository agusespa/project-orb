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

func initialModel(setupResult *SetupResult) model {
	mode, _ := agent.FindMode(setupResult.SelectedMode)

	personaPath, err := agent.EnsurePersonaFile()
	if err != nil {
		return newModel(modelDependencies{
			runnerFactory: nil,
			currentMode:   mode,
			agentName:     "Agent",
			personaPath:   "",
			err:           err,
		})
	}

	agentName, err := agent.LoadAgentName()
	if err != nil {
		return newModel(modelDependencies{
			runnerFactory: nil,
			currentMode:   mode,
			agentName:     "Agent",
			personaPath:   personaPath,
			err:           err,
		})
	}
	if agentName == "" {
		agentName = "Agent"
	}

	client, err := agent.NewClient(agent.DefaultClientConfig())
	if err != nil {
		return newModel(modelDependencies{
			runnerFactory: nil,
			currentMode:   mode,
			agentName:     agentName,
			personaPath:   personaPath,
			err:           err,
		})
	}

	return newModel(modelDependencies{
		runnerFactory: newRunnerFactory(client),
		currentMode:   mode,
		agentName:     agentName,
		personaPath:   personaPath,
		err:           nil,
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
