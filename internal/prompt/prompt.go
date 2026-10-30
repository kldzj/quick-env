// Package prompt asks for secrets on the terminal: hidden input with a dot per
// character, then a masked preview to confirm.
package prompt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"
)

var (
	ErrCancelled  = errors.New("cancelled")
	ErrNoTerminal = errors.New("needs an interactive terminal to ask for the secret")
)

// Session is the terminal in raw mode. Talking to /dev/tty directly keeps the
// caller's stdin/stdout untouched.
type Session struct {
	tty   *os.File
	fd    int
	state *term.State
	sig   chan os.Signal
}

func Open() (*Session, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, ErrNoTerminal
	}
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		tty.Close()
		return nil, err
	}
	s := &Session{tty: tty, fd: fd, state: state, sig: make(chan os.Signal, 1)}
	// Raw mode turns Ctrl-C into an ordinary key, but a signal sent from outside
	// would still end the process with the terminal left raw.
	signal.Notify(s.sig, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	go func() {
		if sig, ok := <-s.sig; ok {
			term.Restore(fd, state)
			os.Exit(128 + int(sig.(syscall.Signal)))
		}
	}()
	return s, nil
}

// Close restores the terminal. Call it before exec'ing anything.
func (s *Session) Close() {
	signal.Stop(s.sig)
	close(s.sig)
	term.Restore(s.fd, s.state)
	s.tty.Close()
}

// Ask prompts for one secret per label, in order.
func (s *Session) Ask(labels []string) ([]string, error) {
	return ask(s.tty, s.tty, labels, s.discardPending)
}

// Confirm shows preview and waits for Enter; action names what Enter does.
func (s *Session) Confirm(preview, action string) bool {
	return confirm(s.tty, s.tty, preview, action, s.discardPending)
}

// Note prints a dim line, e.g. to say nothing happened after a cancel.
func (s *Session) Note(msg string) {
	io.WriteString(s.tty, Style("2", "  "+msg)+"\r\n")
}

func (s *Session) discardPending() {
	flushInput(s.fd)
}

// Mask hides a secret, showing its last 4 characters only when it's long
// enough that they don't give much of it away.
func Mask(secret string) string {
	const dots = "••••••••"
	r := []rune(secret)
	if len(r) < 12 {
		return dots
	}
	return dots + string(r[len(r)-4:])
}

// Style wraps s in an ANSI SGR code ("1" bold, "2" dim) unless NO_COLOR is set.
func Style(code, s string) string {
	if os.Getenv("NO_COLOR") != "" {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

const (
	keyCtrlC     = 0x03
	keyCtrlD     = 0x04
	keyBackspace = 0x08
	keyCtrlU     = 0x15
	keyEsc       = 0x1b
	keyDelete    = 0x7f
)

// readByte reads exactly one byte, leaving anything typed after it unread.
func readByte(in io.Reader) (byte, error) {
	var b [1]byte
	for {
		n, err := in.Read(b[:])
		if n == 1 {
			return b[0], nil
		}
		if err != nil {
			return 0, err
		}
	}
}

// readSecret reads one line from a raw-mode terminal, echoing a dot per character.
// Ctrl-C, Ctrl-D and Esc cancel.
func readSecret(in io.Reader, out io.Writer) (string, error) {
	var buf []byte
	for {
		b, err := readByte(in)
		if err != nil {
			return "", ErrCancelled
		}
		switch {
		case b == '\r' || b == '\n':
			return string(buf), nil
		case b == keyCtrlC || b == keyCtrlD || b == keyEsc:
			return "", ErrCancelled
		case b == keyDelete || b == keyBackspace:
			if len(buf) > 0 {
				_, size := utf8.DecodeLastRune(buf)
				buf = buf[:len(buf)-size]
				io.WriteString(out, "\b \b")
			}
		case b == keyCtrlU:
			io.WriteString(out, strings.Repeat("\b \b", utf8.RuneCount(buf)))
			buf = buf[:0]
		case b < 0x20:
			// other control characters never belong in a secret
		default:
			buf = append(buf, b)
			if utf8.RuneStart(b) {
				io.WriteString(out, "•")
			}
		}
	}
}

// readConfirm waits for Enter (true) or Esc/Ctrl-C/Ctrl-D (false), ignoring other keys.
func readConfirm(in io.Reader) bool {
	for {
		b, err := readByte(in)
		if err != nil {
			return false
		}
		switch b {
		case '\r', '\n':
			return true
		case keyCtrlC, keyCtrlD, keyEsc:
			return false
		}
	}
}

// confirm shows the masked preview and waits for the go-ahead. Leftover input is
// dropped before anything is shown, so only a key pressed after seeing it counts.
func confirm(in io.Reader, out io.Writer, preview, action string, discardPending func()) bool {
	discardPending()
	io.WriteString(out, "\r\n  "+Style("1", preview)+"\r\n")
	io.WriteString(out, Style("2", "  enter to "+action+" · esc to cancel"))
	ok := readConfirm(in)
	io.WriteString(out, "\r\033[K") // clear the hint line
	return ok
}

// ask prompts for each label in turn. discardPending drops input typed before the
// prompt appeared, so a stray extra line in a paste can't answer the next question.
func ask(in io.Reader, out io.Writer, labels []string, discardPending func()) ([]string, error) {
	secrets := make([]string, 0, len(labels))
	for i, label := range labels {
		prompt := fmt.Sprintf("  secret %d/%d", i+1, len(labels))
		if label != "" {
			prompt += " (" + label + ")"
		}
		for {
			discardPending()
			io.WriteString(out, prompt+": ")
			secret, err := readSecret(in, out)
			io.WriteString(out, "\r\n")
			if err != nil {
				return nil, err
			}
			if secret = strings.TrimSpace(secret); secret != "" {
				secrets = append(secrets, secret)
				break
			}
		}
	}
	return secrets, nil
}
