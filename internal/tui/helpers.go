package tui

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/trusted-technologies/nest/internal/schedule"
)

// cmdStyle styles the "$ nest ..." banner printed before a subprocess runs.
var cmdStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

// runGroups runs a huh form built from groups with the Charm theme.
func runGroups(groups ...*huh.Group) error {
	return huh.NewForm(groups...).WithTheme(huh.ThemeCharm()).Run()
}

// runFields runs a single-group huh form with the Charm theme.
func runFields(fields ...huh.Field) error {
	return runGroups(huh.NewGroup(fields...))
}

// aborted reports whether err means the user pressed Esc or Ctrl+C.
func aborted(err error) bool { return errors.Is(err, huh.ErrUserAborted) }

// showNote displays a dismissible note; both Enter and Esc close it.
func showNote(title, description string) {
	_ = runFields(huh.NewNote().
		Title(title).
		Description(description).
		Next(true).
		NextLabel("OK"))
}

// askConfirm asks a yes/no question preselected to def.
func askConfirm(title, description string, def bool) (bool, error) {
	v := def
	c := huh.NewConfirm().Title(title).Value(&v)
	if description != "" {
		c = c.Description(description)
	}
	if err := runFields(c); err != nil {
		return false, err
	}
	return v, nil
}

// validateRequired rejects empty input values.
func validateRequired(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("this field is required")
	}
	return nil
}

// validateSchedule accepts "HH:MM", a 5-field cron expression or empty.
func validateSchedule(s string) error {
	if !schedule.Valid(strings.TrimSpace(s)) {
		return errors.New("want HH:MM (e.g. 04:00), a 5-field cron expression (e.g. 30 3 * * 0) or empty")
	}
	return nil
}

// validateOptInt accepts an empty string or a non-negative integer.
func validateOptInt(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return errors.New("must be a non-negative number")
	}
	return nil
}

// validatePort accepts a TCP port number.
func validatePort(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return errors.New("must be a port number (1-65535)")
	}
	return nil
}

// parseOptInt parses an optional integer input ("" becomes 0).
func parseOptInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

const base62Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// genPassword returns a random n-character base62 password.
func genPassword(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		// Reject bytes that would make the modulo mapping non-uniform.
		for {
			var x [1]byte
			if _, err := rand.Read(x[:]); err != nil {
				return "", err
			}
			if int(x[0]) < 256-256%len(base62Alphabet) {
				b[i] = base62Alphabet[int(x[0])%len(base62Alphabet)]
				break
			}
		}
	}
	return string(b), nil
}

// splitLines splits a textarea value into trimmed non-empty lines.
func splitLines(s string) []string { return splitClean(strings.Split(s, "\n")) }

// splitComma splits a comma-separated value into trimmed non-empty items.
func splitComma(s string) []string { return splitClean(strings.Split(s, ",")) }

// splitClean trims parts and drops empty ones.
func splitClean(parts []string) []string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runNest re-executes the nest binary with args, inheriting stdio so the
// user watches the command's progress directly.
func runNest(args ...string) error {
	fmt.Println()
	fmt.Println(cmdStyle.Render("$ nest " + strings.Join(args, " ")))
	fmt.Println()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	fmt.Println()
	return err
}

// reportResult shows a success/failure note for a finished subprocess.
func reportResult(what string, err error) {
	if err != nil {
		showNote(what+" failed", fmt.Sprintf("The command exited with an error:\n\n%v", err))
		return
	}
	showNote(what+" finished", "The command completed successfully.")
}
