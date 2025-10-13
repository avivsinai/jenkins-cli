package terminal

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// Prompt requests a value from stdin.
func Prompt(label string, defaultValue string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	if defaultValue != "" {
		_, _ = fmt.Fprintf(os.Stdout, "%s [%s]: ", label, defaultValue)
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "%s: ", label)
	}

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue, nil
	}
	return input, nil
}

// PromptSecret reads a sensitive value without echoing input.
func PromptSecret(label string) (string, error) {
	_, _ = fmt.Fprintf(os.Stdout, "%s: ", label)
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
