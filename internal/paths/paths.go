package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const AppName = "project-orb"
const (
	ConfigFileName   = "config.yaml"
	PersonaFileName  = "persona.md"
	DebugLogFileName = "debug.log"
)

func ConfigDir() (string, error) {
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

func ConfigFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, ConfigFileName), nil
}

func DataDir() (string, error) {
	return xdgDir("XDG_DATA_HOME", ".local", "share")
}

func AnalysisSessionsPath(parts ...string) (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(append([]string{dir, "sessions"}, parts...)...), nil
}

func StateDir() (string, error) {
	return xdgDir("XDG_STATE_HOME", ".local", "state")
}

func DebugLogPath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, DebugLogFileName), nil
}

func PersonaFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, PersonaFileName), nil
}

func xdgDir(envName string, fallbackParts ...string) (string, error) {
	if xdgDir := strings.TrimSpace(os.Getenv(envName)); xdgDir != "" {
		return filepath.Join(xdgDir, AppName), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}

	parts := append([]string{homeDir}, fallbackParts...)
	parts = append(parts, AppName)
	return filepath.Join(parts...), nil
}
