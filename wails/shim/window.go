package shim

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// NewWindow opens a window. Same reasoning as New: the options struct is
// assembled here from a let-go map.
func NewWindow(app *application.App, opts vm.Value) *application.WebviewWindow {
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
	return app.Window.NewWithOptions(o)
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

// WatchAssets reloads win whenever a file under dir changes, polling the
// newest modification time every 300ms - a dev convenience for `lgx run`,
// where the frontend is served from disk. It returns a fn of no args that
// stops the watcher. A missing dir is an error up front; a dir that
// disappears later just stops triggering.
func WatchAssets(win *application.WebviewWindow, dir string) (func(), error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("watch-assets!: %q is not a directory", dir)
	}
	stop := make(chan struct{})
	go func() {
		prev := newestModTime(dir)
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if now := newestModTime(dir); now.After(prev) {
					prev = now
					win.Reload()
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }, nil
}

func newestModTime(dir string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
