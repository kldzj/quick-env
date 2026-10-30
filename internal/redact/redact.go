// Package redact hides known secret values in a stream of output.
package redact

import (
	"bytes"
	"io"
	"sort"
)

// Writer replaces each secret written through it. A secret can arrive split
// across writes, so a tail that could still become one is held until the next
// Write or Flush; everything else passes straight through.
type Writer struct {
	w       io.Writer
	secrets [][]byte // longest first, so a secret wins over its own prefix
	repl    map[string][]byte
	maxLen  int
	buf     []byte
	err     error // first failed write; output after it is dropped
}

// New wraps w, replacing each key of replacements with its value.
func New(w io.Writer, replacements map[string]string) *Writer {
	r := &Writer{w: w, repl: map[string][]byte{}}
	for secret, replacement := range replacements {
		if secret == "" {
			continue
		}
		r.secrets = append(r.secrets, []byte(secret))
		r.repl[secret] = []byte(replacement)
		r.maxLen = max(r.maxLen, len(secret))
	}
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
	return r
}

// Write never fails: once the real output fails, the rest is dropped so the
// command writing through it never blocks. Flush reports the failure.
func (r *Writer) Write(p []byte) (int, error) {
	if r.err == nil {
		r.buf = append(r.buf, p...)
		if err := r.emit(false); err != nil {
			r.err, r.buf = err, nil
		}
	}
	return len(p), nil
}

// Flush writes out whatever is still held back. Call it once the stream ends.
func (r *Writer) Flush() error {
	if r.err == nil {
		r.err = r.emit(true)
	}
	return r.err
}

func (r *Writer) emit(final bool) error {
	var out []byte
	i := 0
scan:
	for i < len(r.buf) {
		rest := r.buf[i:]
		// Could rest still grow into a (longer) secret? Then wait for more.
		if !final && len(rest) < r.maxLen {
			for _, s := range r.secrets {
				if len(rest) < len(s) && bytes.HasPrefix(s, rest) {
					break scan
				}
			}
		}
		for _, s := range r.secrets {
			if bytes.HasPrefix(rest, s) {
				out = append(out, r.repl[string(s)]...)
				i += len(s)
				continue scan
			}
		}
		out = append(out, r.buf[i])
		i++
	}
	r.buf = append(r.buf[:0], r.buf[i:]...)
	if len(out) == 0 {
		return nil
	}
	_, err := r.w.Write(out)
	return err
}
