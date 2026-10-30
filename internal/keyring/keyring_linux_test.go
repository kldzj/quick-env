package keyring

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestBusAddress(t *testing.T) {
	runDir := t.TempDir()
	withBus := t.TempDir()
	if err := os.WriteFile(filepath.Join(withBus, "bus"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, envAddr, runtimeDir, want string
	}{
		{"explicit address wins", "unix:path=/x/bus", withBus, "unix:path=/x/bus"},
		{"falls back to the user bus socket", "", withBus, "unix:path=" + filepath.Join(withBus, "bus")},
		{"no bus known", "", runDir, ""},
		{"no runtime dir either", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", tt.envAddr)
			t.Setenv("XDG_RUNTIME_DIR", tt.runtimeDir)
			if got := busAddress(); got != tt.want {
				t.Errorf("busAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Without a known session bus, Open must give up instead of launching one:
// it runs on every shell start, including SSH logins.
func TestOpenWithoutBusIsUnavailable(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	_, err := Open(false)
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

// lockForTest locks the test keyring and unlocks it again when the test ends.
func lockForTest(t *testing.T) {
	t.Helper()
	conn, err := dbus.Connect(os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
	if err != nil {
		t.Fatal(err)
	}
	svc := conn.Object(dest, servicePath)
	var coll dbus.ObjectPath
	if err := svc.Call(ifService+".ReadAlias", 0, "default").Store(&coll); err != nil {
		t.Fatal(err)
	}
	var locked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := svc.Call(ifService+".Lock", 0, []dbus.ObjectPath{coll}).Store(&locked, &prompt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer conn.Close()
		// Unlocking normally shows a password dialog; gnome-keyring also takes the
		// password directly, which is what scripts like this test use.
		var output dbus.Variant
		var session dbus.ObjectPath
		if err := svc.Call(ifService+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
			t.Fatal(err)
		}
		password := secret{Session: session, Parameters: []byte{}, Value: []byte(os.Getenv("QENV_TEST_KEYRING_PASSWORD")), ContentType: "text/plain"}
		if err := svc.Call("org.gnome.keyring.InternalUnsupportedGuiltRiddenInterface.UnlockWithMasterPassword", 0, coll, password).Err; err != nil {
			t.Fatalf("unlocking test keyring: %v", err)
		}
		v, err := conn.Object(dest, coll).GetProperty("org.freedesktop.Secret.Collection.Locked")
		if err != nil || v.Value() != false {
			t.Fatalf("test keyring still locked after unlocking (%v, %v)", v, err)
		}
	})
}
