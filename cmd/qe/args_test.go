package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseArgsFindsSlots(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want []slot
	}{
		{
			name: "standalone @ is labelled by the argument before it",
			argv: []string{"mycli", "login", "--token", "@"},
			want: []slot{{label: "token", index: 3}},
		},
		{
			name: "several @ are asked in order",
			argv: []string{"tool", "login", "--user", "@", "--token", "@"},
			want: []slot{{label: "user", index: 3}, {label: "token", index: 5}},
		},
		{
			name: "@ after = keeps the prefix",
			argv: []string{"tool", "--token=@"},
			want: []slot{{label: "token", index: 1, prefix: "--token="}},
		},
		{
			name: "@ after = works for non-flag assignments",
			argv: []string{"docker", "run", "-e", "TOKEN=@", "img"},
			want: []slot{{label: "TOKEN", index: 3, prefix: "TOKEN="}},
		},
		{
			name: "@ right after another @ gets no label",
			argv: []string{"tool", "@", "@"},
			want: []slot{{label: "tool", index: 1}, {label: "", index: 2}},
		},
		{
			name: "-e slots come first, then @ slots",
			argv: []string{"-e", "A", "-e", "B", "tool", "@"},
			want: []slot{{label: "A", env: "A"}, {label: "B", env: "B"}, {label: "tool", index: 1}},
		},
		{
			name: "@ inside other text is left alone",
			argv: []string{"-e", "K", "ssh", "user@host", "foo@", "@bar", "x=@y"},
			want: []slot{{label: "K", env: "K"}},
		},
		{
			name: "-- ends qe's own options",
			argv: []string{"--", "-tool", "@"},
			want: []slot{{label: "tool", index: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parseArgs(tt.argv)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(p.slots, tt.want) {
				t.Errorf("slots = %+v, want %+v", p.slots, tt.want)
			}
		})
	}
}

func TestParseArgsKeepsCommandAfterOptions(t *testing.T) {
	p, err := parseArgs([]string{"-e", "A", "tool", "-e", "@"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"tool", "-e", "@"}
	if !reflect.DeepEqual(p.command, want) {
		t.Errorf("command = %q, want %q", p.command, want)
	}
}

func TestParseArgsRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{"nothing at all", nil},
		{"no secret to ask for", []string{"mycli", "login", "--token", "KEY"}},
		{"-e without a name", []string{"-e"}},
		{"-e with an invalid name", []string{"-e", "1BAD", "tool"}},
		{"-e without a command", []string{"-e", "A"}},
		{"unknown option", []string{"-x", "tool", "@"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseArgs(tt.argv)
			var ue usageError
			if !errors.As(err, &ue) {
				t.Errorf("err = %v, want a usageError", err)
			}
		})
	}
}

func TestParseArgsHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		if _, err := parseArgs([]string{flag}); !errors.Is(err, errHelp) {
			t.Errorf("%s: err = %v, want errHelp", flag, err)
		}
	}
}

func TestPlanBuildsArgvAndEnv(t *testing.T) {
	p, err := parseArgs([]string{"-e", "API", "tool", "--user", "@", "--token=@"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secrets := []string{"envsecret", "u1", "t2"}

	wantArgv := []string{"tool", "--user", "u1", "--token=t2"}
	if got := p.argv(secrets); !reflect.DeepEqual(got, wantArgv) {
		t.Errorf("argv = %q, want %q", got, wantArgv)
	}
	wantEnv := []string{"HOME=/h", "API=envsecret"}
	if got := p.environ([]string{"HOME=/h"}, secrets); !reflect.DeepEqual(got, wantEnv) {
		t.Errorf("environ = %q, want %q", got, wantEnv)
	}
}

func TestPlanEnvironReplacesAnExistingValue(t *testing.T) {
	p, err := parseArgs([]string{"-e", "FOO", "tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := p.environ([]string{"HOME=/h", "FOO=old", "FOOBAR=keep"}, []string{"new"})
	want := []string{"HOME=/h", "FOOBAR=keep", "FOO=new"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("environ = %q, want %q", got, want)
	}
}

func TestPreview(t *testing.T) {
	const key = "xxxxxxxxxxxx3f9a"
	tests := []struct {
		name    string
		argv    []string
		secrets []string
		want    string
	}{
		{
			name:    "standalone @",
			argv:    []string{"mycli", "login", "--token", "@"},
			secrets: []string{key},
			want:    "mycli login --token ••••••••3f9a",
		},
		{
			name:    "env var",
			argv:    []string{"-e", "API_TOKEN", "sometool"},
			secrets: []string{key},
			want:    "API_TOKEN=••••••••3f9a sometool",
		},
		{
			name:    "@ after = and arguments that need quoting",
			argv:    []string{"tool", "--token=@", "two words", "it's", ""},
			secrets: []string{key},
			want:    `tool --token=••••••••3f9a 'two words' 'it'\''s' ''`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parseArgs(tt.argv)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := p.preview(tt.secrets); got != tt.want {
				t.Errorf("preview = %q, want %q", got, tt.want)
			}
		})
	}
}
