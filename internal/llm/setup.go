package llm

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RunSetupWizard guides the user through initial configuration
func RunSetupWizard() (*Config, error) {
	fmt.Println("\n=== Project Orb Setup ===")
	fmt.Println("Let's configure your llama.cpp models.")

	reader := bufio.NewReader(os.Stdin)

	// Get models directory
	modelsDir, err := promptModelsDirectory(reader)
	if err != nil {
		return nil, err
	}

	// Scan for .gguf files
	models, err := scanForModels(modelsDir)
	if err != nil {
		return nil, err
	}

	if len(models) == 0 {
		fmt.Printf("\n[WARNING] No .gguf model files found in %s\n", modelsDir)
		fmt.Println("Please add model files to this directory and run setup again.")
		return nil, fmt.Errorf("no models found")
	}

	// Select chat model
	fmt.Println("\nAvailable models:")
	for i, model := range models {
		fmt.Printf("  %d. %s\n", i+1, model)
	}

	chatModel, err := promptModelSelection(reader, models, "chat")
	if err != nil {
		return nil, err
	}

	// Select embedding model
	embeddingModel, err := promptModelSelection(reader, models, "embedding")
	if err != nil {
		return nil, err
	}

	config := &Config{
		LlamaCpp: LlamaCppConfig{
			ModelsDir:      modelsDir,
			ChatModel:      chatModel,
			EmbeddingModel: embeddingModel,
			ChatPort:       8080,
			EmbeddingPort:  8081,
		},
	}

	// Save configuration
	if err := SaveConfig(config); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	configPath, _ := getConfigPath()
	fmt.Printf("\n[OK] Configuration saved to %s\n", configPath)

	return config, nil
}

func promptModelsDirectory(reader *bufio.Reader) (string, error) {
	homeDir, _ := os.UserHomeDir()
	defaultDir := filepath.Join(homeDir, "models")

	fmt.Printf("Models directory [%s]: ", defaultDir)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		input = defaultDir
	}

	// Expand ~ to home directory
	if strings.HasPrefix(input, "~") {
		input = filepath.Join(homeDir, input[1:])
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// Check if directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		fmt.Printf("\n[WARNING] Directory does not exist: %s\n", absPath)
		if promptYesNo(reader, "Create it?", true) {
			if err := os.MkdirAll(absPath, 0o755); err != nil {
				return "", fmt.Errorf("create directory: %w", err)
			}
			fmt.Println("[OK] Directory created")
		} else {
			return "", fmt.Errorf("directory does not exist")
		}
	}

	return absPath, nil
}

func promptModelSelection(reader *bufio.Reader, models []string, modelType string) (string, error) {
	for {
		fmt.Printf("\nSelect %s model (1-%d): ", modelType, len(models))
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		input = strings.TrimSpace(input)
		var selection int
		if _, err := fmt.Sscanf(input, "%d", &selection); err != nil || selection < 1 || selection > len(models) {
			fmt.Println("[ERROR] Invalid selection. Please try again.")
			continue
		}

		return models[selection-1], nil
	}
}

func promptYesNo(reader *bufio.Reader, prompt string, defaultYes bool) bool {
	suffix := " [Y/n]: "
	if !defaultYes {
		suffix = " [y/N]: "
	}

	fmt.Print(prompt + suffix)
	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultYes
	}

	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return defaultYes
	}

	return input == "y" || input == "yes"
}

func scanForModels(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
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
