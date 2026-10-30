package redact

import (
	"bytes"
	"errors"
	"testing"
)

var secrets = map[string]string{
	"hf_abc123xyz": "[HF]",
	"abcdefgh":     "[SHORT]",
	"abcdefghijkl": "[LONG]",
}

// writeAll writes each chunk separately, then flushes.
func writeAll(t *testing.T, chunks ...string) string {
	t.Helper()
	var out bytes.Buffer
	w := New(&out, secrets)
	for _, c := range chunks {
		n, err := w.Write([]byte(c))
		if err != nil || n != len(c) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", c, n, err, len(c))
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestReplaces(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"text without secrets is unchanged", []string{"hello world\n"}, "hello world\n"},
		{"secret in one write", []string{"token=hf_abc123xyz ok\n"}, "token=[HF] ok\n"},
		{"secret split across writes", []string{"token=hf_ab", "c123xyz ok\n"}, "token=[HF] ok\n"},
		{"two different secrets", []string{"hf_abc123xyz abcdefgh\n"}, "[HF] [SHORT]\n"},
		{"longer secret wins over its prefix", []string{"abcdefghijkl abcdefgh\n"}, "[LONG] [SHORT]\n"},
		{"longer secret wins even when split right after the shorter one", []string{"abcdefgh", "ijkl\n"}, "[LONG]\n"},
		{"same secret twice", []string{"hf_abc123xyzhf_abc123xyz"}, "[HF][HF]"},
		{"partial secret at the end is kept as is", []string{"see hf_abc"}, "see hf_abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := writeAll(t, tt.chunks...); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReplacesWhenWrittenByteByByte(t *testing.T) {
	in := "a hf_abc123xyz b abcdefghijkl c abcdefgh d hf_abc"
	var chunks []string
	for i := range in {
		chunks = append(chunks, in[i:i+1])
	}
	want := "a [HF] b [LONG] c [SHORT] d hf_abc"
	if got := writeAll(t, chunks...); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Output must not lag: only bytes that could still turn into a secret wait for
// the next write.
func TestOnlyHoldsBackAPossibleSecretStart(t *testing.T) {
	var out bytes.Buffer
	w := New(&out, secrets)
	w.Write([]byte("progress 50%\n"))
	if got := out.String(); got != "progress 50%\n" {
		t.Errorf("after plain line: %q, want it all written", got)
	}
	w.Write([]byte("next: hf_ab"))
	if got := out.String(); got != "progress 50%\nnext: " {
		t.Errorf("after partial secret: %q, want everything before it", got)
	}
}

type failingWriter struct{ writes int }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.writes++
	return 0, errors.New("disk full")
}

// If the real output fails (a full disk, a closed file), the command writing
// through the Writer must not block on a pipe nobody drains anymore.
func TestKeepsAcceptingOutputAfterAWriteFails(t *testing.T) {
	out := &failingWriter{}
	w := New(out, secrets)
	for _, chunk := range []string{"first\n", "second\n", "third\n"} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", chunk, n, err, len(chunk))
		}
	}
	if out.writes != 1 {
		t.Errorf("underlying writer called %d times, want 1 (give up after the first failure)", out.writes)
	}
	if err := w.Flush(); err == nil {
		t.Error("Flush() = nil, want the write error")
	}
}
