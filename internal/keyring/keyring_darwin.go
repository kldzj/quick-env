package keyring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	securityPath = "/usr/bin/security"

	// Reading from a locked keychain brings up macOS's unlock dialog. When nobody
	// is there to answer it (qenv env at shell start), give up quickly: this is
	// the budget for everything a non-interactive Keyring does.
	quickTimeout = 3 * time.Second
	// Otherwise wait for you to type your password.
	slowTimeout = 5 * time.Minute

	maxDuplicates = 10
)

var errNotSaved = errors.New("the keychain didn't save the value (is it locked?)")

// Keyring keeps each variable as a generic password (service "qenv", account
// NAME) through Apple's security command. The Keychain then always sees the same
// Apple-signed program, so it doesn't ask again after every qenv update.
type Keyring struct {
	keychain    string
	interactive bool
	deadline    time.Time // non-interactive only
}

// Open uses your default keychain, or the one at $QENV_KEYCHAIN.
func Open(interactive bool) (*Keyring, error) {
	if _, err := os.Stat(securityPath); err != nil {
		return nil, ErrUnavailable
	}
	k := &Keyring{keychain: os.Getenv("QENV_KEYCHAIN"), interactive: interactive, deadline: time.Now().Add(quickTimeout)}
	if k.keychain == "" {
		out, err := k.security("", "default-keychain", "-d", "user")
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		k.keychain = parseKeychainPath(out)
	}
	if err := checkKeychain(k.keychain); err != nil {
		return nil, err
	}
	return k, nil
}

func (k *Keyring) Close() {}

func (k *Keyring) Set(name, value string) error {
	if !safeToken(name) {
		return fmt.Errorf("%q can't be used as a name", name)
	}
	// Fed through stdin, so the value never shows up in ps. The quotes keep the
	// label's space; every other word is plain.
	line := fmt.Sprintf("add-generic-password -U -a %s -s %s -l 'qenv: %s' -w %s %s\n",
		name, application, name, encodeValue(value), k.keychain)
	if _, err := k.security(line, "-i"); err != nil {
		// Its message could quote the line, value included.
		return errNotSaved
	}
	// security -i reports success even when it fails, so check.
	got, err := k.Get(name)
	if errors.Is(err, ErrLocked) {
		return err
	}
	if err != nil || got != value {
		return errNotSaved
	}
	return nil
}

func (k *Keyring) Get(name string) (string, error) {
	out, err := k.security("", "find-generic-password", "-a", name, "-s", application, "-w", k.keychain)
	if err != nil {
		return "", err
	}
	return decodeValue(strings.TrimSuffix(out, "\n")), nil
}

// All reads every saved variable. Listing works even while the keychain is
// locked, reading values doesn't: then it returns what it has with ErrLocked.
func (k *Keyring) All() (map[string]string, error) {
	out, err := k.security("", "dump-keychain", k.keychain)
	if err != nil {
		return nil, err
	}
	all := map[string]string{}
	for _, name := range parseDump(out) {
		value, err := k.Get(name)
		if errors.Is(err, ErrNotFound) {
			continue // removed in the meantime
		}
		if err != nil {
			return all, err
		}
		all[name] = value
	}
	return all, nil
}

func (k *Keyring) Delete(name string) error {
	// security deletes one match at a time; keep going so no duplicate survives.
	for i := 0; i < maxDuplicates; i++ {
		_, err := k.security("", "delete-generic-password", "-a", name, "-s", application, k.keychain)
		if errors.Is(err, ErrNotFound) {
			if i == 0 {
				return ErrNotFound
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (k *Keyring) security(stdin string, args ...string) (string, error) {
	deadline := k.deadline
	if k.interactive {
		deadline = time.Now().Add(slowTimeout)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ErrLocked // most likely an unlock dialog nobody answered
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return "", classify(exit.ExitCode(), stderr.String())
	}
	if err != nil {
		return "", err
	}
	return stdout.String(), nil
}
