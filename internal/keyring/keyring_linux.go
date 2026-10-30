package keyring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	dest        = "org.freedesktop.secrets"
	servicePath = dbus.ObjectPath("/org/freedesktop/secrets")
	ifService   = "org.freedesktop.Secret.Service"
	ifItem      = "org.freedesktop.Secret.Item"
	ifPrompt    = "org.freedesktop.Secret.Prompt"
	noPrompt    = dbus.ObjectPath("/")

	// Every call is bounded so a wedged keyring can't hang a new shell.
	callTimeout = 2 * time.Second
	// Unlocking waits for you to type your password into the keyring's dialog.
	promptTimeout = 5 * time.Minute
)

type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type Keyring struct {
	conn        *dbus.Conn
	session     dbus.ObjectPath
	interactive bool
}

// busAddress finds the session bus without ever launching one: a new bus at every
// SSH login would be far worse than no variables.
func busAddress() string {
	if addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" {
		return addr
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		bus := filepath.Join(dir, "bus")
		if _, err := os.Stat(bus); err == nil {
			return "unix:path=" + bus
		}
	}
	return ""
}

// Open connects to the keyring. When interactive, a locked keyring is unlocked by
// its own password dialog; otherwise locked variables are skipped.
func Open(interactive bool) (*Keyring, error) {
	addr := busAddress()
	if addr == "" {
		return nil, ErrUnavailable
	}
	// The context lives as long as the connection, so only cancel it if connecting stalls.
	ctx, cancel := context.WithCancel(context.Background())
	stall := time.AfterFunc(callTimeout, cancel)
	conn, err := dbus.Connect(addr, dbus.WithContext(ctx))
	stall.Stop()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	k := &Keyring{conn: conn, interactive: interactive}
	var output dbus.Variant
	if err := k.call(servicePath, ifService+".OpenSession", "plain", dbus.MakeVariant("")).Store(&output, &k.session); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return k, nil
}

func (k *Keyring) Close() {
	k.conn.Close()
}

func (k *Keyring) Set(name, value string) error {
	var coll dbus.ObjectPath
	if err := k.call(servicePath, ifService+".ReadAlias", "default").Store(&coll); err != nil {
		return err
	}
	if coll == noPrompt {
		return errors.New("no default keyring to save into")
	}
	if err := k.unlock([]dbus.ObjectPath{coll}); err != nil {
		return err
	}
	props := map[string]dbus.Variant{
		"org.freedesktop.Secret.Item.Label":      dbus.MakeVariant("qenv: " + name),
		"org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(attributes(name)),
	}
	s := secret{Session: k.session, Parameters: []byte{}, Value: []byte(value), ContentType: "text/plain"}
	var item, prompt dbus.ObjectPath
	if err := k.call(coll, "org.freedesktop.Secret.Collection.CreateItem", props, s, true).Store(&item, &prompt); err != nil {
		return err
	}
	return k.prompt(prompt)
}

func (k *Keyring) Get(name string) (string, error) {
	items, err := k.find(attributes(name))
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", ErrNotFound
	}
	secrets, err := k.secrets(items[:1])
	if err != nil {
		return "", err
	}
	return string(secrets[items[0]].Value), nil
}

// All returns every saved variable it can read. When not interactive and some are
// locked, it returns the readable ones along with ErrLocked.
func (k *Keyring) All() (map[string]string, error) {
	unlocked, locked, err := k.search(map[string]string{"application": application})
	if err != nil {
		return nil, err
	}
	var lockErr error
	if len(locked) > 0 {
		if k.interactive {
			if err := k.unlock(locked); err != nil {
				return nil, err
			}
			unlocked = append(unlocked, locked...)
		} else {
			lockErr = ErrLocked
		}
	}
	secrets, err := k.secrets(unlocked)
	if err != nil {
		return nil, err
	}
	all := map[string]string{}
	for _, item := range unlocked {
		var attrs map[string]string
		if err := k.call(item, "org.freedesktop.DBus.Properties.Get", ifItem, "Attributes").Store(&attrs); err != nil {
			return nil, err
		}
		if s, ok := secrets[item]; ok && attrs["name"] != "" {
			all[attrs["name"]] = string(s.Value)
		}
	}
	return all, lockErr
}

func (k *Keyring) Delete(name string) error {
	items, err := k.find(attributes(name))
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return ErrNotFound
	}
	for _, item := range items {
		var prompt dbus.ObjectPath
		if err := k.call(item, ifItem+".Delete").Store(&prompt); err != nil {
			return err
		}
		if err := k.prompt(prompt); err != nil {
			return err
		}
	}
	return nil
}

func attributes(name string) map[string]string {
	return map[string]string{"application": application, "name": name}
}

// find returns the matching items, unlocking them if needed and allowed.
func (k *Keyring) find(attrs map[string]string) ([]dbus.ObjectPath, error) {
	unlocked, locked, err := k.search(attrs)
	if err != nil {
		return nil, err
	}
	if len(locked) > 0 {
		if err := k.unlock(locked); err != nil {
			return nil, err
		}
	}
	return append(unlocked, locked...), nil
}

func (k *Keyring) search(attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error) {
	err = k.call(servicePath, ifService+".SearchItems", attrs).Store(&unlocked, &locked)
	return unlocked, locked, err
}

func (k *Keyring) secrets(items []dbus.ObjectPath) (map[dbus.ObjectPath]secret, error) {
	out := map[dbus.ObjectPath]secret{}
	if len(items) == 0 {
		return out, nil
	}
	err := k.call(servicePath, ifService+".GetSecrets", items, k.session).Store(&out)
	return out, err
}

func (k *Keyring) unlock(objects []dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := k.call(servicePath, ifService+".Unlock", objects).Store(&unlocked, &prompt); err != nil {
		return err
	}
	if prompt == noPrompt {
		return nil
	}
	if !k.interactive {
		return ErrLocked
	}
	return k.prompt(prompt)
}

// prompt shows a keyring dialog (e.g. for the password) and waits for it.
// Dismissing it counts as the keyring staying locked.
func (k *Keyring) prompt(prompt dbus.ObjectPath) error {
	if prompt == noPrompt {
		return nil
	}
	if !k.interactive {
		return ErrLocked
	}
	signals := make(chan *dbus.Signal, 4)
	k.conn.Signal(signals)
	defer k.conn.RemoveSignal(signals)
	// Subscribe before triggering the prompt, or the answer could arrive first.
	match := []dbus.MatchOption{
		dbus.WithMatchObjectPath(prompt),
		dbus.WithMatchInterface(ifPrompt),
		dbus.WithMatchMember("Completed"),
	}
	if err := k.conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer k.conn.RemoveMatchSignal(match...)
	if err := k.call(prompt, ifPrompt+".Prompt", "").Err; err != nil {
		return err
	}
	timeout := time.After(promptTimeout)
	for {
		select {
		case sig := <-signals:
			if sig.Path != prompt || sig.Name != ifPrompt+".Completed" || len(sig.Body) == 0 {
				continue
			}
			if dismissed, _ := sig.Body[0].(bool); dismissed {
				return ErrLocked
			}
			return nil
		case <-timeout:
			return ErrLocked
		}
	}
}

func (k *Keyring) call(path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return k.conn.Object(dest, path).CallWithContext(ctx, method, 0, args...)
}
