package keyring

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

// Recorded from `security default-keychain -d user` on macOS 27.
func TestParseKeychainPath(t *testing.T) {
	got := parseKeychainPath("    \"/Users/me/Library/Keychains/login.keychain-db\"\n")
	if want := "/Users/me/Library/Keychains/login.keychain-db"; got != want {
		t.Errorf("parseKeychainPath = %q, want %q", got, want)
	}
}

// The first two records are recorded from `security dump-keychain` on macOS 27;
// the others are other apps' entries in the same format.
const dumpFixture = `keychain: "/private/tmp/qenv-probe-98278.keychain-db"
version: 256
class: "genp"
attributes:
    0x00000007 <blob>="qenv: QUOTED"
    0x00000008 <blob>=<NULL>
    "acct"<blob>="QUOTED"
    "cdat"<timedate>=0x32303236313033303132303234365A00  "20261030120246Z\000"
    "crtr"<uint32>=<NULL>
    "svce"<blob>="qenv"
    "type"<uint32>=<NULL>
keychain: "/private/tmp/qenv-probe-98278.keychain-db"
version: 256
class: "genp"
attributes:
    0x00000007 <blob>="qenv:NAME"
    0x00000008 <blob>=<NULL>
    "acct"<blob>="NAME"
    "svce"<blob>="qenv"
    "type"<uint32>=<NULL>
keychain: "/Users/x/Library/Keychains/login.keychain-db"
version: 256
class: "genp"
attributes:
    0x00000007 <blob>="gh:github.com"
    "acct"<blob>="octocat"
    "svce"<blob>="gh:github.com"
keychain: "/Users/x/Library/Keychains/login.keychain-db"
version: 256
class: "inet"
attributes:
    "acct"<blob>="IMPOSTOR"
    "srvr"<blob>="qenv"
    "svce"<blob>="qenv"
`

func TestParseDumpFindsOnlyQenvPasswords(t *testing.T) {
	got := parseDump(dumpFixture)
	if want := []string{"QUOTED", "NAME"}; !reflect.DeepEqual(got, want) {
		t.Errorf("parseDump = %q, want %q", got, want)
	}
}

func TestEncodedValuesRoundTrip(t *testing.T) {
	for _, value := range []string{"hf_abc123", "two words", `it's "quoted" \ back`, "ünïcode €", "a+b/c=d"} {
		encoded := encodeValue(value)
		if !safeToken(encoded) {
			t.Errorf("encodeValue(%q) = %q, which `security -i` can't take as one word", value, encoded)
		}
		if got := decodeValue(encoded); got != value {
			t.Errorf("decodeValue(encodeValue(%q)) = %q", value, got)
		}
	}
}

func TestDecodeLeavesValuesItDidntEncodeAlone(t *testing.T) {
	for _, raw := range []string{"plain", "qenv-base64url:not base64!"} {
		if got := decodeValue(raw); got != raw {
			t.Errorf("decodeValue(%q) = %q, want it unchanged", raw, got)
		}
	}
}

func TestSafeToken(t *testing.T) {
	for _, s := range []string{"HF_TOKEN", "/Users/x/Library/Keychains/login.keychain-db", "/tmp/a-b.keychain-db"} {
		if !safeToken(s) {
			t.Errorf("safeToken(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "has space", "it's", `say "hi"`, `back\slash`, "new\nline"} {
		if safeToken(s) {
			t.Errorf("safeToken(%q) = true, want false", s)
		}
	}
}

// Exit codes and messages recorded from security on macOS 27.
func TestClassifySecurityErrors(t *testing.T) {
	tests := []struct {
		name   string
		code   int
		stderr string
		want   error
	}{
		{"missing item", 44, "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n", ErrNotFound},
		{"unlock dialog cancelled", 128, "", ErrLocked},
		{"wrong passphrase", 51, "security: SecKeychainUnlock /tmp/k: The user name or passphrase you entered is not correct.\n", ErrLocked},
		// Not recorded (needs a session without a desktop); matched by message, the code is a guess.
		{"no way to ask (e.g. over SSH)", 1, "security: SecKeychainItemCopyContent: User interaction is not allowed.\n", ErrLocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.code, tt.stderr); !errors.Is(got, tt.want) {
				t.Errorf("classify = %v, want %v", got, tt.want)
			}
		})
	}
	other := classify(1, "security: something else broke\n")
	if errors.Is(other, ErrNotFound) || errors.Is(other, ErrLocked) || other == nil {
		t.Errorf("classify(other) = %v, want a plain error", other)
	}
}

// security silently falls back to your default keychain when given a path that
// doesn't exist (seen on macOS 27), so a wrong QENV_KEYCHAIN must be refused.
func TestCheckKeychain(t *testing.T) {
	dir := t.TempDir()
	existing := dir + "/test.keychain-db"
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkKeychain(existing); err != nil {
		t.Errorf("checkKeychain(existing) = %v, want nil", err)
	}
	if err := checkKeychain(dir + "/missing.keychain-db"); err == nil {
		t.Error("checkKeychain(missing) = nil, want an error")
	}
	spaced := dir + "/with space.keychain-db"
	os.WriteFile(spaced, nil, 0o600)
	if err := checkKeychain(spaced); err == nil {
		t.Error("checkKeychain(path with a space) = nil, want an error")
	}
}
