package keyring

// Helpers for the macOS backend, which drives /usr/bin/security. They don't need
// a Mac, so they're built (and tested) everywhere.

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// encodedPrefix marks values qenv stored. Encoding keeps every value a single
// plain word for `security -i`, and `security -w` prints it back verbatim.
const encodedPrefix = "qenv-base64url:"

func encodeValue(value string) string {
	return encodedPrefix + base64.RawURLEncoding.EncodeToString([]byte(value))
}

// decodeValue undoes encodeValue, and leaves values it didn't produce as they are.
func decodeValue(stored string) string {
	encoded, ok := strings.CutPrefix(stored, encodedPrefix)
	if !ok {
		return stored
	}
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return stored
	}
	return string(value)
}

// safeToken reports whether s can be passed to `security -i` as one word.
func safeToken(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\r\n'\"\\")
}

// parseKeychainPath reads `security default-keychain` output: an indented, quoted path.
func parseKeychainPath(out string) string {
	return strings.Trim(strings.TrimSpace(out), `"`)
}

var dumpAttribute = regexp.MustCompile(`^\s*"(\w{4})"<blob>="(.*)"$`)

// parseDump returns the names of qenv's entries in `security dump-keychain` output.
func parseDump(out string) []string {
	var names []string
	class, service, account := "", "", ""
	flush := func() {
		if class == "genp" && service == application && account != "" {
			names = append(names, account)
		}
		class, service, account = "", "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "keychain: ") {
			flush()
			continue
		}
		if c, ok := strings.CutPrefix(line, "class: "); ok {
			class = strings.Trim(c, `"`)
			continue
		}
		if m := dumpAttribute.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "svce":
				service = m[2]
			case "acct":
				account = m[2]
			}
		}
	}
	flush()
	return names
}

// classify turns a failed `security` run into ErrNotFound, ErrLocked or a plain error.
func classify(code int, stderr string) error {
	msg := strings.TrimSpace(stderr)
	switch {
	case code == 44 || strings.Contains(msg, "could not be found"):
		return ErrNotFound
	// 128: the unlock dialog was cancelled. 51: wrong passphrase.
	case code == 128 || code == 51 || strings.Contains(msg, "User interaction is not allowed"):
		return ErrLocked
	}
	return fmt.Errorf("security exited with %d: %s", code, msg)
}

// checkKeychain makes sure security will use exactly this keychain: given a path
// that doesn't exist, it silently falls back to your default keychain instead.
func checkKeychain(path string) error {
	if !safeToken(path) {
		return fmt.Errorf("keychain path %q can't contain spaces, quotes or backslashes", path)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("keychain %s not found", path)
	}
	return nil
}
