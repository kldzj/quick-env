// qe runs a command with a secret you paste, keeping it out of your shell history.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/kldzj/quick-env/internal/prompt"
)

const usage = `qe runs a command with a secret you paste, keeping it out of your shell history.

usage:
  qe COMMAND ... @ ...      ask for a secret and put it where @ is
  qe -e NAME COMMAND ...    ask for a secret and pass it as env var NAME

@ must be its own argument (--key @) or follow = (--key=@).
Each @ gets its own prompt. -e can be repeated and combined with @.

example:
  qe mycli login --token @
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	p, err := parseArgs(args)
	if errors.Is(err, errHelp) {
		fmt.Print(usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "qe: %v\n\n%s", err, usage)
		return 2
	}

	// Check before asking, so a typo doesn't cost you a paste.
	path, err := exec.LookPath(p.command[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "qe: command not found: %s\n", p.command[0])
		return 127
	}

	secrets, err := ask(p)
	if errors.Is(err, prompt.ErrCancelled) {
		return 130
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "qe: %v\n", err)
		return 1
	}

	// Replace qe with the command: same terminal, its own exit code, no qe process left behind.
	err = syscall.Exec(path, p.argv(secrets), p.environ(os.Environ(), secrets))
	fmt.Fprintf(os.Stderr, "qe: %v\n", err)
	return 126
}

// ask collects the secrets and the go-ahead. The terminal is restored before it returns.
func ask(p plan) ([]string, error) {
	s, err := prompt.Open()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	secrets, err := s.Ask(p.labels())
	if err == nil && !s.Confirm(p.preview(secrets), "run") {
		err = prompt.ErrCancelled
	}
	if err != nil {
		s.Note("cancelled, nothing was run")
		return nil, err
	}
	return secrets, nil
}
