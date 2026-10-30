// Package keyring keeps qenv's variables in the system keyring: the freedesktop
// Secret Service on Linux (gnome-keyring, KWallet, KeePassXC), the Keychain on macOS.
//
// Each platform provides the same API: Open, Close, Set, Get, All and Delete.
package keyring

import "errors"

var (
	ErrUnavailable = errors.New("no keyring available")
	ErrLocked      = errors.New("keyring is locked")
	ErrNotFound    = errors.New("not found")
)

// application tags qenv's entries so they can be told apart from everything else.
const application = "qenv"
