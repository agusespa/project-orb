package main

import (
	"project-orb/internal/coach"

	tea "github.com/charmbracelet/bubbletea"
)

func newProgram(m model) *tea.Program {
	return tea.NewProgram(m, tea.WithAltScreen())
}

func initialModel() model {
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

	service, serviceErr := coach.NewService(client)
	if err == nil && serviceErr != nil {
		err = serviceErr
	}

	return newModel(modelDependencies{
		runner:      newCoachRunner(service),
		coachName:   coachName,
		personaPath: personaPath,
		err:         err,
	})
}
