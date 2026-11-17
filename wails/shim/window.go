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
	state, err := startState(opts)
	if err != nil {
		return nil, err
	}
	o.StartState = state
	if err := position(opts, &o); err != nil {
		return nil, err
	}
	return app.Window.NewWithOptions(o), nil
}

// position reads :x and :y, the window's top-left corner in Wails'
// coordinates (on macOS, points from the primary screen's top-left). Both
// or neither: without them the window centres, as Wails does by default.
// optInt answers false for a missing key and for a non-integer alike, so
// presence is checked first.
func position(opts vm.Value, o *application.WebviewWindowOptions) error {
	hasX, hasY := opt(opts, "x") != nil, opt(opts, "y") != nil
	if !hasX && !hasY {
		return nil
	}
	x, okX := optInt(opts, "x")
	y, okY := optInt(opts, "y")
	if !hasX || !hasY || !okX || !okY {
		return fmt.Errorf("new-window: :x and :y go together, both integers; got :x %v :y %v",
			opt(opts, "x"), opt(opts, "y"))
	}
	o.X, o.Y = x, y
	o.InitialPosition = application.WindowXY
	return nil
}

// macTitleBar maps :mac-title-bar to one of Wails' title bar presets. With
// a hidden one the page draws under the traffic lights, and an element
// with the CSS --wails-draggable: drag moves the window. Only macOS uses
// it. The value is a keyword or a string, with or without the colon.
// :hidden-inset-compact is the unified preset with AppKit's compact
// toolbar: a lower band than unified's, with the lights centred in it.
// Wails has the style but no preset for it.
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
	case "hidden-inset-compact":
		tb := application.MacTitleBarHiddenInsetUnified
		tb.ToolbarStyle = application.MacToolbarStyleUnifiedCompact
		return tb, nil
	}
	got := v.String()
	if ok {
		got = name
	}
	return application.MacTitleBar{}, fmt.Errorf(
		"new-window: :mac-title-bar must be one of :default, :hidden, :hidden-inset, :hidden-inset-unified, :hidden-inset-compact; got %q", got)
}

// startState maps :start-state to the state a window opens in. Wails
// applies it as the window is created, on every platform. The value is a
// keyword or a string, with or without the colon. :minimised is left out
// until something needs it.
func startState(opts vm.Value) (application.WindowState, error) {
	v := opt(opts, "start-state")
	if v == nil {
		return application.WindowStateNormal, nil
	}
	name, ok := ToGo(v).(string)
	switch strings.TrimPrefix(name, ":") {
	case "normal":
		return application.WindowStateNormal, nil
	case "maximised":
		return application.WindowStateMaximised, nil
	case "fullscreen":
		return application.WindowStateFullscreen, nil
	}
	got := v.String()
	if ok {
		got = name
	}
	return application.WindowStateNormal, fmt.Errorf(
		"new-window: :start-state must be one of :normal, :maximised, :fullscreen; got %q", got)
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

// SetZoom is the page zoom, 1.0 for 100%. Wails clamps it to at least 1.0.
func SetZoom(w *application.WebviewWindow, level float64) { w.SetZoom(level) }

func IsMaximised(w *application.WebviewWindow) bool  { return w.IsMaximised() }
func IsFullscreen(w *application.WebviewWindow) bool { return w.IsFullscreen() }

// Bounds is the window's frame: its top-left corner and its size, in the
// units SetBounds takes. All zero in server mode and once the window is
// gone.
func Bounds(w *application.WebviewWindow) (vm.Value, error) {
	return rectValue(w.Bounds())
}

// SetBounds sizes the window and then moves it. Wails' own SetBounds
// moves first, using the old height, and on macOS then resizes from the
// bottom-left corner, so a change in height would move the top edge.
// windowSetSize animates synchronously, so the move lands after it.
func SetBounds(w *application.WebviewWindow, x, y, width, height int) {
	w.SetSize(width, height)
	w.SetPosition(x, y)
}

// WorkArea is the work area (the screen less the menu bar and the Dock) of
// the screen the window is on, in the units of Bounds. A window on no
// screen - its display was unplugged - gets the primary screen's. nil when
// neither is known, which is always so in server mode.
func WorkArea(w *application.WebviewWindow) (vm.Value, error) {
	if s, err := w.GetScreen(); err == nil && s != nil {
		if area := windowWorkArea(s); area.Width > 0 && area.Height > 0 {
			return rectValue(area)
		}
	}
	// The cached list is already in DIPs (screenmanager's applyDPIScaling),
	// so the primary screen's work area is used as it is.
	if app := application.Get(); app != nil {
		if p := app.Screen.GetPrimary(); p != nil && p.WorkArea.Width > 0 {
			return rectValue(p.WorkArea)
		}
	}
	return vm.NIL, nil
}

func rectValue(r application.Rect) (vm.Value, error) {
	return FromJSON(map[string]any{
		"x": float64(r.X), "y": float64(r.Y),
		"width": float64(r.Width), "height": float64(r.Height),
	})
}

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
