package paths

import (
	"path/filepath"
	"testing"
)

func TestConfigDirUsesXDGConfigHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}

	want := filepath.Join(tmpDir, AppName)
	if got != want {
		t.Fatalf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestDataDirUsesXDGDataHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}

	want := filepath.Join(tmpDir, AppName)
	if got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
}

func TestStateDirUsesXDGStateHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error = %v", err)
	}

	want := filepath.Join(tmpDir, AppName)
	if got != want {
		t.Fatalf("StateDir() = %q, want %q", got, want)
	}
}
