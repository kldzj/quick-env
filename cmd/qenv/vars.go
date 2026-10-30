package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/kldzj/quick-env/internal/environ"
	"github.com/kldzj/quick-env/internal/prompt"
)

// minMaskLen is the shortest value qenv run masks: shorter ones would also match
// ordinary text and garble the output.
const minMaskLen = 8

func sortedNames(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// exportScript renders vars for `eval "$(qenv env)"`.
func exportScript(vars map[string]string) string {
	var b strings.Builder
	for _, name := range sortedNames(vars) {
		fmt.Fprintf(&b, "export %s='%s'\n", name, strings.ReplaceAll(vars[name], "'", `'\''`))
	}
	return b.String()
}

// maskTargets maps each value long enough to mask to the text that replaces it.
func maskTargets(vars map[string]string) map[string]string {
	targets := map[string]string{}
	for name, value := range vars {
		if utf8.RuneCountInString(value) >= minMaskLen {
			targets[value] = "[qenv:" + name + "]"
		}
	}
	return targets
}

func setPreview(name, value, old string) string {
	preview := name + "=" + prompt.Mask(value)
	if old != "" {
		preview += "  (replaces " + prompt.Mask(old) + ")"
	}
	return preview
}

func listing(vars map[string]string) string {
	width := 0
	for name := range vars {
		width = max(width, len(name))
	}
	var b strings.Builder
	for _, name := range sortedNames(vars) {
		fmt.Fprintf(&b, "%-*s  %s\n", width, name, prompt.Mask(vars[name]))
	}
	return b.String()
}

func parseRun(args []string) (noMask bool, command []string, err error) {
	i := 0
	for ; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if arg == "--no-mask" {
			noMask = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return false, nil, fmt.Errorf("unknown option %s", arg)
		}
		break
	}
	if i >= len(args) {
		return false, nil, errors.New("no command given")
	}
	return noMask, args[i:], nil
}

// onlyValidNames drops names that aren't plain variable names. They come from the
// keyring, and qenv env's output gets eval'd.
func onlyValidNames(vars map[string]string) map[string]string {
	valid := map[string]string{}
	for name, value := range vars {
		if environ.ValidName(name) {
			valid[name] = value
		}
	}
	return valid
}

// forwardSignal decides whether qenv run passes sig on to the command. In a
// terminal's foreground, Ctrl-C and Ctrl-\ reach the command along with qenv, so
// passing them on again would deliver them twice.
func forwardSignal(sig os.Signal, foreground bool) bool {
	if sig == syscall.SIGINT || sig == syscall.SIGQUIT {
		return !foreground
	}
	return true
}

// inForeground reports whether qenv runs in the foreground of its terminal.
func inForeground() bool {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	defer tty.Close()
	pgrp, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	return err == nil && pgrp == unix.Getpgrp()
}
