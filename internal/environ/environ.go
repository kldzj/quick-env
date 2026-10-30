// Package environ builds a child process environment.
package environ

import (
	"regexp"
	"strings"
)

var validName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Set returns base with each NAME=value in vars set. An existing NAME is dropped
// rather than duplicated, since programs disagree on which duplicate wins.
func Set(base []string, vars ...string) []string {
	names := map[string]bool{}
	for _, kv := range vars {
		name, _, _ := strings.Cut(kv, "=")
		names[name] = true
	}
	var out []string
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !names[name] {
			out = append(out, kv)
		}
	}
	return append(out, vars...)
}

// ValidName reports whether name can be used as an environment variable.
func ValidName(name string) bool { return validName.MatchString(name) }
