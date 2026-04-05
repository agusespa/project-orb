package setup

import (
	"fmt"
	"strconv"
	"strings"
)

func IsYes(input string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes" || input == "ye" || input == "yeah"
}

func IsYesOrNo(input string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes" || input == "ye" || input == "yeah" ||
		input == "n" || input == "no" || input == "nope"
}

func ValidateModelSelection(input string, availableCount int) (int, error) {
	if availableCount == 0 {
		return 0, fmt.Errorf("no models available")
	}

	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > availableCount {
		return 0, fmt.Errorf("invalid selection. Please enter a number between 1 and %d", availableCount)
	}

	return selection, nil
}

func ValidateModeSelection(input string) (int, error) {
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > 3 {
		return 0, fmt.Errorf("invalid selection. Please enter 1, 2, or 3")
	}

	return selection, nil
}

func ModeIDFromSelection(selection int) string {
	modes := []string{"coach", "performance-review", "analyst"}
	if selection < 1 || selection > len(modes) {
		return "coach"
	}
	return modes[selection-1]
}
