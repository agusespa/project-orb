package main

import (
	"project-orb/internal/coach"

	tea "github.com/charmbracelet/bubbletea"
)

func newProgram(m model) *tea.Program {
	return tea.NewProgram(m, tea.WithAltScreen())
}

func initialModel() model {
	defaultMode := coach.DefaultMode()
	personaPath, err := coach.EnsurePersonaFile()
	coachName, nameErr := coach.LoadCoachName()
	if err == nil && nameErr != nil {
		err = nameErr
	}
	if coachName == "" {
		coachName = "Coach"
	}

	client, clientErr := coach.NewClient(coach.DefaultClientConfig())
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

func newRunnerFactory(client *coach.Client) runnerFactory {
	return func(mode coach.Mode) (streamRunner, error) {
		service, err := coach.NewService(client, mode)
		if err != nil {
			return nil, err
		}

		return newCoachRunner(service), nil
	}
}
