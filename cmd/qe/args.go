package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kldzj/quick-env/internal/environ"
	"github.com/kldzj/quick-env/internal/prompt"
)

var errHelp = errors.New("help requested")

// usageError is a mistake in how qe was called; main prints it with a usage hint.
type usageError string

func (e usageError) Error() string { return string(e) }

// slot is one secret to ask for: either an env var (env != "") or an @ in the command.
type slot struct {
	label  string
	env    string
	index  int    // position of the @ argument in the command
	prefix string // text before the @, e.g. "--token="
}

type plan struct {
	command []string
	slots   []slot
}

func parseArgs(argv []string) (plan, error) {
	var p plan
	i := 0
	for ; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			i++
			break
		}
		if arg == "-h" || arg == "--help" {
			return plan{}, errHelp
		}
		if arg == "-e" {
			if i+1 >= len(argv) {
				return plan{}, usageError("-e needs a variable name, like: qe -e API_KEY sometool")
			}
			name := argv[i+1]
			if !environ.ValidName(name) {
				return plan{}, usageError(fmt.Sprintf("%q is not a valid variable name", name))
			}
			p.slots = append(p.slots, slot{label: name, env: name})
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return plan{}, usageError("unknown option " + arg)
		}
		break
	}

	p.command = argv[i:]
	if len(p.command) == 0 {
		return plan{}, usageError("no command given")
	}
	for j := 1; j < len(p.command); j++ {
		arg := p.command[j]
		switch {
		case arg == "@":
			label := ""
			if prev := p.command[j-1]; j == 1 || !isPlaceholder(prev) {
				label = strings.TrimLeft(prev, "-")
			}
			p.slots = append(p.slots, slot{label: label, index: j})
		case strings.HasSuffix(arg, "=@"):
			prefix := strings.TrimSuffix(arg, "@")
			label := strings.TrimLeft(strings.TrimSuffix(prefix, "="), "-")
			p.slots = append(p.slots, slot{label: label, index: j, prefix: prefix})
		}
	}
	if len(p.slots) == 0 {
		return plan{}, usageError("nothing to ask for: put @ where the secret goes, or use -e NAME")
	}
	return p, nil
}

func isPlaceholder(arg string) bool {
	return arg == "@" || strings.HasSuffix(arg, "=@")
}

// argv returns the command with each @ replaced by its secret.
// secrets line up with p.slots.
func (p plan) argv(secrets []string) []string {
	out := append([]string(nil), p.command...)
	for i, s := range p.slots {
		if s.env == "" {
			out[s.index] = s.prefix + secrets[i]
		}
	}
	return out
}

// environ returns base with NAME=secret set for each -e slot.
func (p plan) environ(base, secrets []string) []string {
	var vars []string
	for i, s := range p.slots {
		if s.env != "" {
			vars = append(vars, s.env+"="+secrets[i])
		}
	}
	return environ.Set(base, vars...)
}

// labels names each slot for its prompt.
func (p plan) labels() []string {
	labels := make([]string, len(p.slots))
	for i, s := range p.slots {
		labels[i] = s.label
	}
	return labels
}

// preview renders the command as it would be typed, with secrets masked.
func (p plan) preview(secrets []string) string {
	words := make([]string, len(p.command))
	for i, arg := range p.command {
		words[i] = shellQuote(arg)
	}
	var env []string
	for i, s := range p.slots {
		if s.env != "" {
			env = append(env, s.env+"="+prompt.Mask(secrets[i]))
			continue
		}
		words[s.index] = prompt.Mask(secrets[i])
		if s.prefix != "" {
			words[s.index] = shellQuote(s.prefix) + words[s.index]
		}
	}
	return strings.Join(append(env, words...), " ")
}

var safeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(s string) string {
	if safeWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
