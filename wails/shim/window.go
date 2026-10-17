package shim

import (
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
