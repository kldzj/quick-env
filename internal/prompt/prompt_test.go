package prompt

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	tests := []struct {
		secret, want string
	}{
		{"abcdefghijkl3f9a", "••••••••3f9a"},
		{"abcdefgh3f9a", "••••••••3f9a"}, // exactly 12: tail shown
		{"abcdefg3f9a", "••••••••"},      // 11: too short to show a tail
		{"αβγδεζηθικλμ", "••••••••ικλμ"}, // tail is 4 characters, not 4 bytes
	}
	for _, tt := range tests {
		if got := Mask(tt.secret); got != tt.want {
			t.Errorf("Mask(%q) = %q, want %q", tt.secret, got, tt.want)
		}
	}
}

func TestReadSecret(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		want     string
		wantEcho string
	}{
		{"enter as carriage return", "abc\r", "abc", "•••"},
		{"enter as newline", "abc\n", "abc", "•••"},
		{"backspace removes the last character", "abc\x7fd\r", "abd", "•••\b \b•"},
		{"ctrl-h also works as backspace", "ab\x08\r", "a", "••\b \b"},
		{"backspace on empty input does nothing", "\x7fab\r", "ab", "••"},
		{"ctrl-u clears the input", "abc\x15d\r", "d", "•••\b \b\b \b\b \b•"},
		{"one dot per character, not per byte", "äb\r", "äb", "••"},
		{"backspace removes a whole multi-byte character", "bä\x7f\r", "b", "••\b \b"},
		{"other control characters are ignored", "a\x01\tb\r", "ab", "••"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var echo bytes.Buffer
			got, err := readSecret(strings.NewReader(tt.in), &echo)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("secret = %q, want %q", got, tt.want)
			}
			if echo.String() != tt.wantEcho {
				t.Errorf("echo = %q, want %q", echo.String(), tt.wantEcho)
			}
		})
	}
}

func TestReadSecretCancels(t *testing.T) {
	tests := []struct{ name, in string }{
		{"ctrl-c", "ab\x03"},
		{"esc", "ab\x1b"},
		{"ctrl-d", "ab\x04"},
		{"input ends before enter", "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readSecret(strings.NewReader(tt.in), &bytes.Buffer{})
			if !errors.Is(err, ErrCancelled) {
				t.Errorf("err = %v, want ErrCancelled", err)
			}
		})
	}
}

func TestReadConfirm(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"enter runs", "\r", true},
		{"newline runs", "\n", true},
		{"other keys are ignored until enter", "xy \r", true},
		{"esc cancels", "\x1b", false},
		{"ctrl-c cancels", "x\x03\r", false},
		{"ctrl-d cancels", "\x04", false},
		{"input ending cancels", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readConfirm(strings.NewReader(tt.in)); got != tt.want {
				t.Errorf("readConfirm(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestConfirmShowsPreviewAndWaitsForEnter(t *testing.T) {
	var out bytes.Buffer
	if !confirm(strings.NewReader("\r"), &out, "tool ••••••••3f9a", "run", func() {}) {
		t.Error("confirm returned false on enter")
	}
	if !strings.Contains(out.String(), "tool ••••••••3f9a") {
		t.Errorf("preview not shown; output: %q", out.String())
	}
}

func TestConfirmDiscardsLeftoverInputBeforeShowingAnything(t *testing.T) {
	var out bytes.Buffer
	shownBeforeDiscard := -1
	confirm(strings.NewReader("\r"), &out, "tool", "run", func() { shownBeforeDiscard = out.Len() })
	if shownBeforeDiscard != 0 {
		t.Errorf("%d bytes were shown before leftover input was discarded; an enter pressed right after the hint appears would be thrown away", shownBeforeDiscard)
	}
}

func TestAskCollectsOneSecretPerSlot(t *testing.T) {
	labels := []string{"user", "token"}
	got, err := ask(strings.NewReader("s1\rs2\r"), &bytes.Buffer{}, labels, func() {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"s1", "s2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("secrets = %q, want %q", got, want)
	}
}

func TestAskTrimsSurroundingSpaces(t *testing.T) {
	labels := []string{"api-key"}
	got, err := ask(strings.NewReader("  key  \r"), &bytes.Buffer{}, labels, func() {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"key"}; !reflect.DeepEqual(got, want) {
		t.Errorf("secrets = %q, want %q", got, want)
	}
}

func TestAskRepeatsTheQuestionOnEmptyInput(t *testing.T) {
	labels := []string{"api-key"}
	var out bytes.Buffer
	got, err := ask(strings.NewReader("\r   \rkey\r"), &out, labels, func() {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"key"}; !reflect.DeepEqual(got, want) {
		t.Errorf("secrets = %q, want %q", got, want)
	}
	if n := strings.Count(out.String(), "api-key"); n != 3 {
		t.Errorf("asked %d times, want 3; output: %q", n, out.String())
	}
}

func TestAskDiscardsLeftoverInputBeforeEachPrompt(t *testing.T) {
	labels := []string{"a", "b"}
	fresh := 0
	_, err := ask(strings.NewReader("s1\rs2\r"), &bytes.Buffer{}, labels, func() { fresh++ })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fresh != 2 {
		t.Errorf("leftover input discarded %d times, want once per prompt (2)", fresh)
	}
}

func TestAskStopsOnCancel(t *testing.T) {
	labels := []string{"a", "b"}
	_, err := ask(strings.NewReader("s1\r\x03"), &bytes.Buffer{}, labels, func() {})
	if !errors.Is(err, ErrCancelled) {
		t.Errorf("err = %v, want ErrCancelled", err)
	}
}
