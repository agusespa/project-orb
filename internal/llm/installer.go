package llm

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// CheckInstallation checks if llama-server is installed and up to date
// Returns (isInstalled, isOutdated, error)
func CheckInstallation() (bool, bool, error) {
	// Check if llama-server exists in PATH
	if _, err := exec.LookPath("llama-server"); err != nil {
		return false, false, nil
	}

	// Check if Homebrew is available
	if _, err := exec.LookPath("brew"); err != nil {
		// llama-server exists but brew not available, can't check for updates
		return true, false, nil
	}

	// Check if llama.cpp is outdated
	cmd := exec.Command("brew", "outdated", "llama.cpp")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Command failed, but llama-server exists, so continue
		return true, false, nil
	}

	isOutdated := strings.Contains(string(output), "llama.cpp")
	return true, isOutdated, nil
}

// InstallViaBrew installs llama.cpp via Homebrew
func InstallViaBrew() error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("homebrew not found in PATH")
	}

	cmd := exec.Command("brew", "install", "llama.cpp")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew install failed: %w\n%s", err, stderr.String())
	}

	return nil
}

// UpgradeViaBrew upgrades llama.cpp via Homebrew
func UpgradeViaBrew() error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("homebrew not found in PATH")
	}

	cmd := exec.Command("brew", "upgrade", "llama.cpp")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew upgrade failed: %w\n%s", err, stderr.String())
	}

	return nil
}

// HasHomebrew checks if Homebrew is installed
func HasHomebrew() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}
