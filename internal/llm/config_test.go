package llm

import (
	"path/filepath"
	"testing"

	"project-orb/internal/paths"
)

func TestGetConfigPathUsesXDGConfigHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	got, err := paths.ConfigFilePath()
	if err != nil {
		t.Fatalf("ConfigFilePath() error = %v", err)
	}

	want := filepath.Join(tmpDir, paths.AppName, paths.ConfigFileName)
	if got != want {
		t.Fatalf("ConfigFilePath() = %q, want %q", got, want)
	}
}
