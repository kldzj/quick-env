package main

import (
	"reflect"
	"syscall"
	"testing"
)

func TestExportScriptQuotesEveryValue(t *testing.T) {
	got := exportScript(map[string]string{"B": "it's $HOME", "A": "plain"})
	want := "export A='plain'\nexport B='it'\\''s $HOME'\n"
	if got != want {
		t.Errorf("exportScript = %q, want %q", got, want)
	}
}

func TestExportScriptEmpty(t *testing.T) {
	if got := exportScript(nil); got != "" {
		t.Errorf("exportScript(nil) = %q, want empty", got)
	}
}

func TestMaskTargetsSkipsShortValues(t *testing.T) {
	got := maskTargets(map[string]string{
		"HF_TOKEN": "hf_abcdefgh",
		"EIGHT":    "12345678",
		"PIN":      "1234567",
	})
	want := map[string]string{
		"hf_abcdefgh": "[qenv:HF_TOKEN]",
		"12345678":    "[qenv:EIGHT]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("maskTargets = %v, want %v", got, want)
	}
}

func TestSetPreview(t *testing.T) {
	if got, want := setPreview("HF_TOKEN", "hf_secretvalue1234", ""), "HF_TOKEN=••••••••1234"; got != want {
		t.Errorf("new: %q, want %q", got, want)
	}
	got := setPreview("HF_TOKEN", "hf_secretvalue1234", "hf_oldvalue99abcd")
	want := "HF_TOKEN=••••••••1234  (replaces ••••••••abcd)"
	if got != want {
		t.Errorf("replacing: %q, want %q", got, want)
	}
}

func TestListingIsSortedAndAligned(t *testing.T) {
	got := listing(map[string]string{"HF_TOKEN": "hf_secretvalue1234", "A": "x"})
	want := "A         ••••••••\nHF_TOKEN  ••••••••1234\n"
	if got != want {
		t.Errorf("listing = %q, want %q", got, want)
	}
}

func TestParseRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantNoMask bool
		wantCmd    []string
	}{
		{"plain command", []string{"python", "dl.py"}, false, []string{"python", "dl.py"}},
		{"after --", []string{"--", "python", "dl.py"}, false, []string{"python", "dl.py"}},
		{"--no-mask", []string{"--no-mask", "--", "sh"}, true, []string{"sh"}},
		{"options after the command are the command's", []string{"tool", "--no-mask"}, false, []string{"tool", "--no-mask"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			noMask, cmd, err := parseRun(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if noMask != tt.wantNoMask || !reflect.DeepEqual(cmd, tt.wantCmd) {
				t.Errorf("parseRun = %v, %q; want %v, %q", noMask, cmd, tt.wantNoMask, tt.wantCmd)
			}
		})
	}
	for _, args := range [][]string{nil, {"--"}, {"--no-mask"}, {"--bogus", "tool"}} {
		if _, _, err := parseRun(args); err == nil {
			t.Errorf("parseRun(%q): want an error", args)
		}
	}
}

// Names come back from the keyring, where anything that can write it could plant
// one; `eval "$(qenv env)"` must never run a name as shell code.
func TestOnlyValidNamesDropsAnythingElse(t *testing.T) {
	got := onlyValidNames(map[string]string{
		"API_TOKEN":          "a",
		"x; touch /tmp/pwnd": "b",
		"$(id)":              "c",
		"1BAD":               "d",
	})
	if want := map[string]string{"API_TOKEN": "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("onlyValidNames = %v, want %v", got, want)
	}
}

// In a terminal's foreground, Ctrl-C and Ctrl-\ reach the command along with qenv;
// passing them on again would make it see them twice.
func TestForwardSignal(t *testing.T) {
	tests := []struct {
		sig        syscall.Signal
		foreground bool
		want       bool
	}{
		{syscall.SIGINT, true, false},
		{syscall.SIGQUIT, true, false},
		{syscall.SIGTERM, true, true},
		{syscall.SIGHUP, true, true},
		{syscall.SIGINT, false, true},
		{syscall.SIGQUIT, false, true},
		{syscall.SIGTERM, false, true},
	}
	for _, tt := range tests {
		if got := forwardSignal(tt.sig, tt.foreground); got != tt.want {
			t.Errorf("forwardSignal(%v, foreground=%v) = %v, want %v", tt.sig, tt.foreground, got, tt.want)
		}
	}
}
