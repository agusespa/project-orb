package llm

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func CheckInstallation() (bool, bool, error) {
	if _, err := exec.LookPath("llama-server"); err != nil {
		return false, false, nil
	}

	if _, err := exec.LookPath("brew"); err != nil {
		return true, false, nil
	}

	cmd := exec.Command("brew", "outdated", "llama.cpp")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return true, false, nil
	}

	isOutdated := strings.Contains(string(output), "llama.cpp")
	return true, isOutdated, nil
}

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

func HasHomebrew() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}
