package shim

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// NewWindow opens a window. Same reasoning as New: the options struct is
// assembled here from a let-go map.
func NewWindow(app *application.App, opts vm.Value) (*application.WebviewWindow, error) {
	o := application.WebviewWindowOptions{}
	if s, ok := optString(opts, "title"); ok {
		o.Title = s
	}
	if n, ok := optInt(opts, "width"); ok {
		o.Width = n
	}
	if n, ok := optInt(opts, "height"); ok {
		o.Height = n
	}
	if n, ok := optInt(opts, "min-width"); ok {
		o.MinWidth = n
	}
	if n, ok := optInt(opts, "min-height"); ok {
		o.MinHeight = n
	}
	if s, ok := optString(opts, "url"); ok {
		o.URL = s
	}
	o.Hidden = optBool(opts, "hidden")
	tb, err := macTitleBar(opts)
	if err != nil {
		return nil, err
	}
	o.Mac.TitleBar = tb
	return app.Window.NewWithOptions(o), nil
}

// macTitleBar maps :mac-title-bar to one of Wails' title bar presets. With
// a hidden one the page draws under the traffic lights, and an element
// with the CSS --wails-draggable: drag moves the window. Only macOS uses
// it. The value is a keyword or a string, with or without the colon.
func macTitleBar(opts vm.Value) (application.MacTitleBar, error) {
	v := opt(opts, "mac-title-bar")
	if v == nil {
		return application.MacTitleBarDefault, nil
	}
	name, ok := ToGo(v).(string)
	switch strings.TrimPrefix(name, ":") {
	case "default":
		return application.MacTitleBarDefault, nil
	case "hidden":
		return application.MacTitleBarHidden, nil
	case "hidden-inset":
		return application.MacTitleBarHiddenInset, nil
	case "hidden-inset-unified":
		return application.MacTitleBarHiddenInsetUnified, nil
	}
	got := v.String()
	if ok {
		got = name
	}
	return application.MacTitleBar{}, fmt.Errorf(
		"new-window: :mac-title-bar must be one of :default, :hidden, :hidden-inset, :hidden-inset-unified; got %q", got)
}

// The window controls are pass-throughs. Each Wails method already moves
// its platform call onto the main thread (InvokeSync inside), so they are
// safe from any goroutine - a handler, a menu click, an nREPL eval. They
// return nothing: the Wails methods return the Window interface for
// chaining, which let-go has no use for.

func SetTitle(w *application.WebviewWindow, title string) { w.SetTitle(title) }
func Show(w *application.WebviewWindow)                   { w.Show() }
func Hide(w *application.WebviewWindow)                   { w.Hide() }
func Close(w *application.WebviewWindow)                  { w.Close() }
func Focus(w *application.WebviewWindow)                  { w.Focus() }
func Reload(w *application.WebviewWindow)                 { w.Reload() }
func ExecJS(w *application.WebviewWindow, js string)      { w.ExecJS(js) }

// InvokeSync runs fn on the main thread and waits for it. Wails' own
// methods hop there by themselves; this is for anything else that must
// run there. fn's result is discarded and an error is printed, as for
// every callback.
func InvokeSync(fn vm.Fn) {
	application.InvokeSync(func() { callback("on-main!", fn) })
}

// WatchAssets reloads win whenever a file under dir changes, polling a
// fingerprint of the tree every 300ms - a dev convenience for `lgx run`,
// where the frontend is served from disk. It returns a fn of no args that
// stops the watcher. A missing dir is an error up front; a dir that
// disappears later just stops triggering.
func WatchAssets(win *application.WebviewWindow, dir string) (func(), error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("watch-assets!: %q is not a directory", dir)
	}
	stop := make(chan struct{})
	go func() {
		prev := treeFingerprint(dir)
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if now := treeFingerprint(dir); now != prev {
					prev = now
					win.Reload()
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }, nil
}

// treeFingerprint hashes every entry's path, size and modification time.
// Comparing it, rather than the newest mtime, catches an edit to any file
// even when another one carries a later (or future) timestamp, plus adds,
// deletes and renames.
func treeFingerprint(dir string) uint64 {
	h := fnv.New64a()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fmt.Fprintf(h, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return h.Sum64()
}
