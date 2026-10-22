// Package shim gives let-go the system keyring through
// github.com/zalando/go-keyring: the macOS Keychain, the Secret Service on
// Linux, the Credential Manager on Windows.
//
// go-keyring's API is four flat functions, but a generated binding would
// not do: lginterop names the namespace after the path's last segment
// (go-keyring), and a missing entry arrives only as an error value to
// compare against keyring.ErrNotFound. The shim maps that case instead:
// Get answers nil and Delete succeeds.
//
// Every call holds one mutex. go-keyring's memory provider (MockInit) is a
// plain map with no lock of its own, and a let-go program calls from many
// goroutines (a Wails app's bridge calls run concurrently). Serialising
// the real providers too costs nothing at the rate a keyring is used.
package shim

import (
	"errors"
	"sync"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
	"github.com/zalando/go-keyring"
)

var mu sync.Mutex

// Set stores password under service and user, replacing any stored one.
func Set(service, user, password string) error {
	mu.Lock()
	defer mu.Unlock()
	return keyring.Set(service, user, password)
}

// Get returns the password stored under service and user, or nil when
// there is none.
func Get(service, user string) (vm.Value, error) {
	mu.Lock()
	defer mu.Unlock()
	p, err := keyring.Get(service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return vm.NIL, nil
	}
	if err != nil {
		return vm.NIL, err
	}
	return vm.String(p), nil
}

// Delete removes the password stored under service and user. A missing
// one is not an error.
func Delete(service, user string) error {
	mu.Lock()
	defer mu.Unlock()
	err := keyring.Delete(service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// UseMemory replaces the system keyring with an empty in-memory one, for
// the rest of the process: go-keyring's provider is a package variable.
func UseMemory() {
	mu.Lock()
	defer mu.Unlock()
	keyring.MockInit()
}

// Registered from init, not rt.RegisterInstaller: pkg/rt drains its
// installer queue during its own package init, which runs before this one.
func init() {
	ns := vm.NewNamespace("keyring.shim")
	ns.Def("Set", vm.MustBox(Set))
	ns.Def("Get", vm.MustBox(Get))
	ns.Def("Delete", vm.MustBox(Delete))
	ns.Def("UseMemory", vm.MustBox(UseMemory))
	rt.RegisterNS(ns)
}
