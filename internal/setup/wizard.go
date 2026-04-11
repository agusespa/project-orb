package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"project-orb/internal/agent"
	"project-orb/internal/llm"
	"project-orb/internal/paths"
	"project-orb/internal/text"
)

type Wizard struct {
	ctx context.Context
}

func NewWizard(ctx context.Context) *Wizard {
	return &Wizard{ctx: ctx}
}

func (w *Wizard) DefaultModelsDirInput() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	if strings.HasSuffix(homeDir, string(os.PathSeparator)) {
		return homeDir
	}

	return homeDir + string(os.PathSeparator)
}

func (w *Wizard) ValidateModelsDir(input string) (string, error) {
	if strings.HasPrefix(input, "~") {
		homeDir, _ := os.UserHomeDir()
		input = filepath.Join(homeDir, input[1:])
	}

	absPath, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("directory doesn't exist: %s", absPath)
	}

	return absPath, nil
}

func (w *Wizard) ScanModels(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var models []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".gguf") {
			models = append(models, name)
		}
	}

	return models, nil
}

func (w *Wizard) CheckPersonaExists() (exists bool, personaPath string, err error) {
	personaPath, err = paths.PersonaFilePath()
	if err != nil {
		return false, "", err
	}

	_, err = os.Stat(personaPath)
	if err == nil {
		return true, personaPath, nil
	}

	if os.IsNotExist(err) {
		personaPath, err = agent.EnsurePersonaFile()
		return false, personaPath, err
	}

	return false, "", err
}

func (w *Wizard) BuildPersona(name, tone string) string {
	var b strings.Builder

	b.WriteString("# Persona\n\n")

	if name != "" {
		b.WriteString(text.PersonaNameLine(name))
	}

	if tone == "" {
		tone = text.DefaultPersonality
	}
	b.WriteString(text.PersonaToneLine(tone))

	return b.String()
}

func (w *Wizard) SavePersona(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func (w *Wizard) StartServer(config *llm.Config) (*llm.Manager, error) {
	manager, err := llm.NewManager(config)
	if err != nil {
		return nil, fmt.Errorf("create LLM manager: %w", err)
	}

	if err := manager.StartChatServer(w.ctx); err != nil {
		return manager, fmt.Errorf("start chat server: %w", err)
	}
	if err := manager.StartEmbeddingServer(w.ctx); err != nil {
		return manager, fmt.Errorf("start embedding server: %w", err)
	}

	return manager, nil
}
