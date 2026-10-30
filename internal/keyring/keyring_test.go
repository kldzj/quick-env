package keyring

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

// These tests need a real keyring. test/run.sh sets one up that's thrown away
// afterwards (a private gnome-keyring on Linux, a temporary keychain file on
// macOS) and sets QENV_TEST_KEYRING=1; without that they skip, so they never
// touch your real keyring.
func openTest(t *testing.T) *Keyring {
	t.Helper()
	if os.Getenv("QENV_TEST_KEYRING") != "1" {
		t.Skip("needs the isolated keyring from test/run.sh")
	}
	k, err := Open(false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(k.Close)
	all, err := k.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for name := range all {
		if err := k.Delete(name); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
	}
	return k
}

func TestSetThenGet(t *testing.T) {
	k := openTest(t)
	if err := k.Set("HF_TOKEN", "hf_abc123"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := k.Get("HF_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "hf_abc123" {
		t.Errorf("Get = %q, want %q", got, "hf_abc123")
	}
}

func TestSetReplacesExistingValue(t *testing.T) {
	k := openTest(t)
	if err := k.Set("HF_TOKEN", "old"); err != nil {
		t.Fatal(err)
	}
	if err := k.Set("HF_TOKEN", "new"); err != nil {
		t.Fatal(err)
	}
	all, err := k.All()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"HF_TOKEN": "new"}; !reflect.DeepEqual(all, want) {
		t.Errorf("All = %v, want %v", all, want)
	}
}

func TestAllReturnsEverySavedVariable(t *testing.T) {
	k := openTest(t)
	for name, value := range map[string]string{"A": "1", "B": "two words", "C": "ünïcode"} {
		if err := k.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	all, err := k.All()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"A": "1", "B": "two words", "C": "ünïcode"}; !reflect.DeepEqual(all, want) {
		t.Errorf("All = %v, want %v", all, want)
	}
}

func TestDeleteRemovesOnlyThatVariable(t *testing.T) {
	k := openTest(t)
	k.Set("KEEP", "1")
	k.Set("DROP", "2")
	if err := k.Delete("DROP"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := k.Get("DROP"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
	if v, err := k.Get("KEEP"); err != nil || v != "1" {
		t.Errorf("Get KEEP = %q, %v; want \"1\", nil", v, err)
	}
}

func TestMissingVariableIsNotFound(t *testing.T) {
	k := openTest(t)
	if _, err := k.Get("NOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: err = %v, want ErrNotFound", err)
	}
	if err := k.Delete("NOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: err = %v, want ErrNotFound", err)
	}
}

func TestLockedKeyringIsSkippedWithoutAsking(t *testing.T) {
	k := openTest(t)
	if err := k.Set("HF_TOKEN", "hf_abc123"); err != nil {
		t.Fatal(err)
	}
	lockForTest(t)

	nk, err := Open(false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer nk.Close()
	start := time.Now()
	all, err := nk.All()
	if !errors.Is(err, ErrLocked) {
		t.Errorf("All: err = %v, want ErrLocked", err)
	}
	if len(all) != 0 {
		t.Errorf("All returned %v from a locked keyring", all)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("All took %v on a locked keyring; it must not wait for anything", d)
	}
}
