package keyring

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// lockForTest locks the test keychain and unlocks it again when the test ends.
func lockForTest(t *testing.T) {
	t.Helper()
	// In a desktop session, reading from a locked keychain brings up macOS's unlock
	// dialog, and nobody is there to answer it.
	if out, _ := exec.Command("launchctl", "managername").Output(); strings.TrimSpace(string(out)) == "Aqua" {
		t.Skip("in a desktop session a locked keychain shows a dialog")
	}
	keychain := os.Getenv("QENV_KEYCHAIN")
	if out, err := exec.Command(securityPath, "lock-keychain", keychain).CombinedOutput(); err != nil {
		t.Fatalf("locking test keychain: %v: %s", err, out)
	}
	t.Cleanup(func() {
		out, err := exec.Command(securityPath, "unlock-keychain", "-p", os.Getenv("QENV_TEST_KEYRING_PASSWORD"), keychain).CombinedOutput()
		if err != nil {
			t.Fatalf("unlocking test keychain: %v: %s", err, out)
		}
	})
}
