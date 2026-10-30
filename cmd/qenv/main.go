// qenv keeps secrets in your keyring as environment variables and hands them to
// the commands you choose.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"golang.org/x/term"

	"github.com/kldzj/quick-env/internal/environ"
	"github.com/kldzj/quick-env/internal/keyring"
	"github.com/kldzj/quick-env/internal/prompt"
	"github.com/kldzj/quick-env/internal/redact"
)

const usage = `qenv keeps secrets in your keyring as environment variables.

usage:
  qenv set NAME            ask for a value and save it
  qenv ls                  list saved variables, masked
  qenv rm NAME ...         delete saved variables
  qenv get NAME            print a value, e.g. "$(qenv get API_TOKEN)"
  qenv run [--no-mask] [--] COMMAND ...
                           run a command with all saved variables set
  qenv env                 print export lines: eval "$(qenv env)"

qenv run replaces saved values with [qenv:NAME] in output that isn't going to
a terminal (a pipe, a file, a coding agent). --no-mask turns that off.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	case "set":
		return cmdSet(rest)
	case "ls":
		return cmdList(rest)
	case "rm":
		return cmdRemove(rest)
	case "get":
		return cmdGet(rest)
	case "env":
		return cmdEnv()
	case "run":
		return cmdRun(rest)
	}
	fmt.Fprintf(os.Stderr, "qenv: unknown command %q\n\n%s", sub, usage)
	return 2
}

func usageError(msg string) int {
	fmt.Fprintf(os.Stderr, "qenv: %s\n", msg)
	return 2
}

func fail(err error) int {
	msg := err.Error()
	switch {
	case errors.Is(err, keyring.ErrUnavailable):
		if runtime.GOOS == "linux" {
			msg += " (is gnome-keyring, KWallet or KeePassXC running?)"
		}
	case errors.Is(err, keyring.ErrLocked):
		msg = "the keyring is locked"
	}
	fmt.Fprintf(os.Stderr, "qenv: %s\n", msg)
	return 1
}

func notSaved(name string) int {
	fmt.Fprintf(os.Stderr, "qenv: no saved variable %s\n", name)
	return 1
}

func cmdSet(args []string) int {
	if len(args) != 1 {
		return usageError("usage: qenv set NAME")
	}
	name := args[0]
	if !environ.ValidName(name) {
		return usageError(fmt.Sprintf("%q is not a valid variable name", name))
	}
	// Open the keyring first, so a problem there doesn't cost you a paste.
	k, err := keyring.Open(true)
	if err != nil {
		return fail(err)
	}
	defer k.Close()
	old, err := k.Get(name)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fail(err)
	}

	value, err := askValue(name, old)
	if errors.Is(err, prompt.ErrCancelled) {
		return 130
	}
	if err != nil {
		return fail(err)
	}
	if err := k.Set(name, value); err != nil {
		return fail(err)
	}
	fmt.Fprintf(os.Stderr, "saved %s · use it with: qenv run -- COMMAND\n", name)
	return 0
}

func askValue(name, old string) (string, error) {
	s, err := prompt.Open()
	if err != nil {
		return "", err
	}
	defer s.Close()
	values, err := s.Ask([]string{name})
	if err == nil && !s.Confirm(setPreview(name, values[0], old), "save") {
		err = prompt.ErrCancelled
	}
	if err != nil {
		s.Note("cancelled, nothing was saved")
		return "", err
	}
	return values[0], nil
}

func cmdList(args []string) int {
	if len(args) != 0 {
		return usageError("usage: qenv ls")
	}
	vars, err := loadAll()
	if err != nil {
		return fail(err)
	}
	if len(vars) == 0 {
		fmt.Fprintln(os.Stderr, "no saved variables · add one with: qenv set NAME")
		return 0
	}
	fmt.Print(listing(vars))
	return 0
}

func cmdRemove(args []string) int {
	if len(args) == 0 {
		return usageError("usage: qenv rm NAME ...")
	}
	k, err := keyring.Open(true)
	if err != nil {
		return fail(err)
	}
	defer k.Close()
	code := 0
	for _, name := range args {
		err := k.Delete(name)
		if errors.Is(err, keyring.ErrNotFound) {
			code = notSaved(name)
			continue
		}
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(os.Stderr, "removed %s\n", name)
	}
	return code
}

func cmdGet(args []string) int {
	if len(args) != 1 {
		return usageError("usage: qenv get NAME")
	}
	k, err := keyring.Open(true)
	if err != nil {
		return fail(err)
	}
	defer k.Close()
	value, err := k.Get(args[0])
	if errors.Is(err, keyring.ErrNotFound) {
		return notSaved(args[0])
	}
	if err != nil {
		return fail(err)
	}
	// No trailing newline: piped somewhere, it would become part of the secret.
	fmt.Print(value)
	return 0
}

// cmdEnv is meant to run at shell start, so it never asks for anything and never
// complains: whatever it can't read just isn't loaded.
func cmdEnv() int {
	k, err := keyring.Open(false)
	if err != nil {
		return 0
	}
	defer k.Close()
	vars, _ := k.All()
	fmt.Print(exportScript(onlyValidNames(vars)))
	return 0
}

func loadAll() (map[string]string, error) {
	k, err := keyring.Open(true)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	vars, err := k.All()
	return onlyValidNames(vars), err
}

func cmdRun(args []string) int {
	noMask, command, err := parseRun(args)
	if err != nil {
		return usageError(err.Error() + "\nusage: qenv run [--no-mask] [--] COMMAND ...")
	}
	path, err := exec.LookPath(command[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "qenv: command not found: %s\n", command[0])
		return 127
	}
	vars, err := loadAll()
	if err != nil {
		return fail(err)
	}
	var pairs []string
	for _, name := range sortedNames(vars) {
		pairs = append(pairs, name+"="+vars[name])
	}
	env := environ.Set(os.Environ(), pairs...)

	// Your terminal is left alone; output that's captured somewhere gets masked.
	targets := maskTargets(vars)
	maskOut := !noMask && len(targets) > 0 && !term.IsTerminal(int(os.Stdout.Fd()))
	maskErr := !noMask && len(targets) > 0 && !term.IsTerminal(int(os.Stderr.Fd()))
	if !maskOut && !maskErr {
		err = syscall.Exec(path, command, env)
		fmt.Fprintf(os.Stderr, "qenv: %v\n", err)
		return 126
	}
	return runMasked(path, command, env, targets, maskOut, maskErr)
}

func runMasked(path string, command, env []string, targets map[string]string, maskOut, maskErr bool) int {
	cmd := exec.Command(path)
	cmd.Args = command
	cmd.Env = env
	cmd.Stdin = os.Stdin
	var writers []*redact.Writer
	wrap := func(f *os.File, mask bool) io.Writer {
		if !mask {
			return f // the command writes to it directly
		}
		w := redact.New(f, targets)
		writers = append(writers, w)
		return w
	}
	cmd.Stdout = wrap(os.Stdout, maskOut)
	cmd.Stderr = wrap(os.Stderr, maskErr)

	// qenv has to outlive the command to flush its output, so it catches these
	// and passes on the ones the command doesn't get by itself.
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	foreground := inForeground()

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "qenv: %v\n", err)
		return 126
	}
	go func() {
		for sig := range signals {
			if forwardSignal(sig, foreground) {
				cmd.Process.Signal(sig)
			}
		}
	}()
	cmd.Wait()
	signal.Stop(signals)
	for _, w := range writers {
		if err := w.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "qenv: couldn't write the command's output: %v\n", err)
		}
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return cmd.ProcessState.ExitCode()
}
