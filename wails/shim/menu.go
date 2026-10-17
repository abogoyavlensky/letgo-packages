package shim

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// The application menu is data: a vector of item maps, built here into a
// Wails menu. Being data is what makes it reloadable - edit the spec, call
// set-menu! again from the REPL, and the menu bar is rebuilt in place.
//
//	[{:role :app-menu}
//	 {:label "File"
//	  :items [{:label "Export…" :accelerator "CmdOrCtrl+E" :on-click #'export!}
//	          {:separator true}
//	          {:role :quit}]}
//	 {:role :edit-menu}]
//
// An item is exactly one of: {:separator true}; {:role r}; or {:label s}
// with either :items (a submenu) or an optional :on-click and
// :accelerator. Keys may be keywords or strings.

var menuRoles = map[string]application.Role{
	"app-menu":     application.AppMenu,
	"file-menu":    application.FileMenu,
	"edit-menu":    application.EditMenu,
	"view-menu":    application.ViewMenu,
	"window-menu":  application.WindowMenu,
	"help-menu":    application.HelpMenu,
	"quit":         application.Quit,
	"about":        application.About,
	"close-window": application.CloseWindow,
	"reload":       application.Reload,
}

// buildMenu builds a Wails menu from a spec. An error names the path of
// the offending item, e.g. `[1 :items 0 :role]`, so a typo in a nested
// submenu is findable.
//
// Wails registers every item it creates in a process-global map until the
// item is destroyed, so a build that fails part-way destroys what it made.
// It also needs the application: some roles and the accelerator check
// reach Wails' global app, which is nil before new-app.
func buildMenu(spec vm.Value) (*application.Menu, error) {
	if application.Get() == nil {
		return nil, errors.New("menus need the application: call new-app first")
	}
	m := application.NewMenu()
	if err := addItems(m, spec, nil); err != nil {
		m.Destroy()
		return nil, err
	}
	return m, nil
}

// CheckMenu builds spec and throws it away, so a spec can be validated -
// in a test, say - without installing it.
//
// Every fn here that can fail returns (bool, error) rather than a bare
// error: let-go throws on the error of a (value, error) pair but hands a
// lone error back as a value, which would let a bad spec pass silently.
func CheckMenu(spec vm.Value) (bool, error) {
	m, err := buildMenu(spec)
	if err != nil {
		return false, err
	}
	m.Destroy()
	return true, nil
}

var (
	menuMu      sync.Mutex
	currentMenu *application.Menu
)

// SetMenu builds spec and installs it as the application menu. The menu it
// replaces is destroyed, so rebuilding from the REPL again and again does
// not pile up items and the callbacks they hold.
func SetMenu(app *application.App, spec vm.Value) (bool, error) {
	m, err := buildMenu(spec)
	if err != nil {
		return false, err
	}
	menuMu.Lock()
	defer menuMu.Unlock()
	app.Menu.Set(m)
	if currentMenu != nil {
		currentMenu.Destroy()
	}
	currentMenu = m
	return true, nil
}

func addItems(m *application.Menu, items vm.Value, path []string) error {
	seq, ok := items.(vm.Sequable)
	if !ok || items == vm.NIL {
		return menuError(path, "must be a vector of item maps")
	}
	if isMap(items) {
		return menuError(path, "must be a vector of item maps, got a map")
	}
	// An empty vector's seq is not nil: it yields one nil element.
	if c, ok := items.(vm.Counted); ok && c.RawCount() == 0 {
		return nil
	}
	i := 0
	for s := seq.Seq(); s != nil; s = s.Next() {
		if err := addItem(m, s.First(), at(path, fmt.Sprint(i))); err != nil {
			return err
		}
		i++
	}
	return nil
}

func addItem(m *application.Menu, item vm.Value, path []string) error {
	if !isMap(item) {
		return menuError(path, "must be a map")
	}
	if optBool(item, "separator") {
		m.AddSeparator()
		return nil
	}
	if r := opt(item, "role"); r != nil {
		name, _ := ToGo(r).(string)
		role, ok := menuRoles[name]
		if !ok {
			return menuError(at(path, ":role"), fmt.Sprintf("unknown role %s (known: %s)", r.String(), knownRoles))
		}
		m.AddRole(role)
		return nil
	}
	label, ok := optString(item, "label")
	if !ok {
		return menuError(path, "needs :label, :role or :separator")
	}
	if sub := opt(item, "items"); sub != nil {
		return addItems(m.AddSubmenu(label), sub, at(path, ":items"))
	}
	mi := m.Add(label)
	if acc := opt(item, "accelerator"); acc != nil {
		s, ok := ToGo(acc).(string)
		if !ok {
			return menuError(at(path, ":accelerator"), "must be a string")
		}
		// Wails logs a malformed shortcut and leaves the item without one;
		// reading it back is how to tell, without copying its parser.
		if mi.SetAccelerator(s).GetAccelerator() == "" {
			return menuError(at(path, ":accelerator"), fmt.Sprintf("%q is not a valid accelerator", s))
		}
	}
	fn, err := optFn(item, "on-click")
	if err != nil {
		return menuError(at(path, ":on-click"), "must be a fn or a var")
	}
	if fn != nil {
		mi.OnClick(func(*application.Context) { callback("menu "+label, fn) })
	}
	return nil
}

func menuError(path []string, msg string) error {
	return fmt.Errorf("menu spec [%s]: %s", strings.Join(path, " "), msg)
}

// at extends an error path without sharing the parent's backing array.
func at(path []string, seg string) []string {
	return append(append([]string(nil), path...), seg)
}

const knownRoles = ":app-menu :file-menu :edit-menu :view-menu :window-menu " +
	":help-menu :quit :about :close-window :reload"
