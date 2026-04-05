package llm

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	appName    = "project-orb"
	configFile = "config.yaml"
)

type Config struct {
	LlamaCpp LlamaCppConfig `yaml:"llama_cpp"`
}

type LlamaCppConfig struct {
	ModelsDir      string `yaml:"models_dir"`
	ChatModel      string `yaml:"chat_model"`
	EmbeddingModel string `yaml:"embedding_model"`
	ChatPort       int    `yaml:"chat_port"`
	EmbeddingPort  int    `yaml:"embedding_port"`
}

func DefaultConfig() Config {
	return Config{
		LlamaCpp: LlamaCppConfig{
			ModelsDir:      "",
			ChatModel:      "",
			EmbeddingModel: "",
			ChatPort:       8080,
			EmbeddingPort:  8081,
		},
	}
}

func LoadConfig() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found at %s", configPath)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

func SaveConfig(config *Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func (c *Config) Validate() error {
	if c.LlamaCpp.ModelsDir == "" {
		return fmt.Errorf("models_dir is required")
	}

	if c.LlamaCpp.ChatModel == "" {
		return fmt.Errorf("chat_model is required")
	}

	if c.LlamaCpp.EmbeddingModel == "" {
		return fmt.Errorf("embedding_model is required")
	}

	if _, err := os.Stat(c.LlamaCpp.ModelsDir); err != nil {
		return fmt.Errorf("models_dir does not exist: %s", c.LlamaCpp.ModelsDir)
	}

	chatModelPath := filepath.Join(c.LlamaCpp.ModelsDir, c.LlamaCpp.ChatModel)
	if _, err := os.Stat(chatModelPath); err != nil {
		return fmt.Errorf("chat_model not found: %s", chatModelPath)
	}

	embeddingModelPath := filepath.Join(c.LlamaCpp.ModelsDir, c.LlamaCpp.EmbeddingModel)
	if _, err := os.Stat(embeddingModelPath); err != nil {
		return fmt.Errorf("embedding_model not found: %s", embeddingModelPath)
	}

	if c.LlamaCpp.ChatPort <= 0 || c.LlamaCpp.ChatPort > 65535 {
		return fmt.Errorf("invalid chat_port: %d", c.LlamaCpp.ChatPort)
	}

	if c.LlamaCpp.EmbeddingPort <= 0 || c.LlamaCpp.EmbeddingPort > 65535 {
		return fmt.Errorf("invalid embedding_port: %d", c.LlamaCpp.EmbeddingPort)
	}

	if c.LlamaCpp.ChatPort == c.LlamaCpp.EmbeddingPort {
		return fmt.Errorf("chat_port and embedding_port must be different")
	}

	return nil
}

func (c *Config) ChatModelPath() string {
	return filepath.Join(c.LlamaCpp.ModelsDir, c.LlamaCpp.ChatModel)
}

func (c *Config) EmbeddingModelPath() string {
	return filepath.Join(c.LlamaCpp.ModelsDir, c.LlamaCpp.EmbeddingModel)
}

func getConfigPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, configFile), nil
}

func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	return filepath.Join(homeDir, ".config", appName), nil
}
