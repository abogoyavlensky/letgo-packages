package shim

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// New builds the application from a let-go options map. Adding an Options
// field is one line here; it is never a change to let-go.
func New(opts vm.Value) (*application.App, error) {
	o := application.Options{
		Services: []application.Service{application.NewService(&Bridge{})},
	}
	if s, ok := optString(opts, "name"); ok {
		o.Name = s
	}
	if s, ok := optString(opts, "description"); ok {
		o.Description = s
	}
	prefix, hasPrefix := optString(opts, "assets")
	dir, hasDir := optString(opts, "assets-dir")
	switch {
	case hasPrefix && hasDir:
		return nil, errors.New("set :assets or :assets-dir, not both")
	case hasPrefix && prefix != "":
		o.Assets = application.AssetOptions{Handler: newResourceAssets(prefix)}
	case hasDir && dir != "":
		o.Assets = application.AssetOptions{Handler: http.FileServer(http.Dir(dir))}
	}
	o.Mac.ApplicationShouldTerminateAfterLastWindowClosed = optBool(opts, "quit-on-last-window-closed")
	onShutdown, err := optFn(opts, "on-shutdown")
	if err != nil {
		return nil, err
	}
	if onShutdown != nil {
		o.OnShutdown = func() { callback("on-shutdown", onShutdown) }
	}
	return application.New(o), nil
}

// Emit sends an event to the frontend. data is lowered the same way a
// handler's return value is.
func Emit(app *application.App, name string, data vm.Value) bool {
	return app.Event.Emit(name, ToGo(data))
}

// Run blocks on the Wails event loop.
//
// It must be reached from let-go code running on the main goroutine:
// application's init() calls runtime.LockOSThread(), which pins main to
// the main OS thread, and the platform layers require it.
func Run(app *application.App) error { return app.Run() }

// Quit stops the application.
func Quit(app *application.App) { app.Quit() }

// OnEvent calls fn with the event's data, as a let-go value, whenever the
// frontend emits name (Events.Emit in @wailsio/runtime). Wails dispatches
// listeners on its own goroutines, so fn runs off the main thread. It
// returns a fn of no args that removes the listener.
func OnEvent(app *application.App, name string, fn vm.Fn) func() {
	return app.Event.On(name, func(e *application.CustomEvent) {
		data, err := FromJSON(e.Data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wails: event %q: %v\n", name, err)
			return
		}
		callback("event "+name, fn, data)
	})
}
