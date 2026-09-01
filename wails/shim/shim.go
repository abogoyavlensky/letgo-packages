// Package shim supplies the pieces of the Wails v3 wrapper that
// generation cannot produce.
//
// Unlike the database/sql wrapper, this package uses no generated
// bindings at all. lginterop can carry almost none of the Wails API:
//
//   - Every entry point takes a struct literal. application.New wants an
//     application.Options, App.Window.NewWithOptions a
//     WebviewWindowOptions. lgx runs lginterop with -opaque-structs, so
//     struct types stay opaque and no constructors are emitted; let-go
//     has no way to build one. New and NewWindow assemble them here from
//     a let-go map.
//
//   - application.NewService is generic, and lginterop skips generics
//     silently (`generic?` gates its emission sites). Service
//     registration - the whole point of a Wails backend - would vanish
//     with no diagnostic. The one service is registered here.
//
//   - Wails binds a *Go type's* methods, discovered reflectively. let-go
//     fns are not Go methods, so there is nothing for it to bind.
//     Bridge.Call is the single bound method, and it dispatches by name
//     to a let-go fn. The frontend reaches a handler with
//     Call.ByName("<this package>.Bridge.Call", name, args) - Wails
//     resolves bound methods by fully-qualified name at run time, so no
//     `wails3 generate bindings` step is involved.
package shim

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// BridgeMethod is the fully-qualified name the frontend calls. Exported
// so the veneer can hand it to JavaScript rather than have every app
// hard-code this package's import path.
const BridgeMethod = "github.com/abogoyavlensky/letgo-packages/wails/shim.Bridge.Call"

var handlers sync.Map // string -> vm.Fn

// Bridge is the single Wails service. Its one exported method is the
// entry point for every frontend call.
type Bridge struct{}

// Call dispatches to the let-go fn registered as name.
//
// Wails invokes service methods on its own goroutines, so this invokes a
// let-go fn from a non-main goroutine. That is the same thing pkg/rt's
// http/serve already does for its handlers.
func (b *Bridge) Call(name string, args []any) (any, error) {
	v, ok := handlers.Load(name)
	if !ok {
		return nil, fmt.Errorf("no let-go handler registered as %q", name)
	}
	fn, ok := v.(vm.Fn)
	if !ok {
		return nil, fmt.Errorf("handler %q is not invokable", name)
	}
	lgArgs := make([]vm.Value, len(args))
	for i, a := range args {
		val, err := vm.ToLetGo(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d of %q: %w", i, name, err)
		}
		lgArgs[i] = val
	}
	res, err := fn.Invoke(lgArgs)
	if err != nil {
		return nil, err
	}
	return ToGo(res), nil
}

// ToGo lowers a let-go value to something encoding/json can marshal.
//
// Unbox alone is not enough: a map and a vector both unbox to themselves.
// A let-go map is a *vm.PersistentMap whose Seq yields vm.MapEntry, so a
// type switch on vm.Map alone silently turns every map into a list of
// pairs - JSON arrays where the frontend expects objects.
func ToGo(v vm.Value) any {
	if v == nil || v == vm.NIL {
		return nil
	}
	switch t := v.(type) {
	case vm.Keyword:
		return string(t)
	case vm.Symbol:
		return string(t)
	case vm.String:
		return string(t)
	}
	if s, ok := v.(vm.Sequable); ok {
		sq := s.Seq()
		if sq != nil {
			if _, isEntry := sq.First().(vm.MapEntry); isEntry {
				out := map[string]any{}
				for ; sq != nil; sq = sq.Next() {
					e := sq.First().(vm.MapEntry)
					out[keyString(e.Key)] = ToGo(e.Value)
				}
				return out
			}
		}
		out := []any{}
		for ; sq != nil; sq = sq.Next() {
			out = append(out, ToGo(sq.First()))
		}
		return out
	}
	return v.Unbox()
}

// keyString renders a map key as the JSON object key. Keywords lose their
// leading colon, which is what a JavaScript caller expects.
func keyString(k vm.Value) string {
	switch t := k.(type) {
	case vm.Keyword:
		return string(t)
	case vm.String:
		return string(t)
	case vm.Symbol:
		return string(t)
	}
	return fmt.Sprintf("%v", k.Unbox())
}

// asMap lowers a let-go map argument. A let-go map does not unbox to a Go
// map[string]any - the reflect proxy rejects it with "reflect: Call using
// *vm.PersistentMap as type map[string]interface {}" - so every shim entry
// point taking options declares vm.Value and converts here.
func asMap(v vm.Value) map[string]any {
	if m, ok := ToGo(v).(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// Register binds a let-go fn to a name the frontend can call.
func Register(name string, fn vm.Fn) { handlers.Store(name, fn) }

// New builds the application. Adding an Options field is one line here;
// it is never a change to let-go.
func New(optsVal vm.Value) *application.App {
	opts := asMap(optsVal)
	o := application.Options{
		Services: []application.Service{application.NewService(&Bridge{})},
	}
	if s, ok := opts["name"].(string); ok {
		o.Name = s
	}
	if s, ok := opts["description"].(string); ok {
		o.Description = s
	}
	if s, ok := opts["assets-dir"].(string); ok && s != "" {
		o.Assets = application.AssetOptions{Handler: http.FileServer(http.Dir(s))}
	}
	return application.New(o)
}

// NewWindow opens a window. Same reasoning as New.
func NewWindow(app *application.App, optsVal vm.Value) *application.WebviewWindow {
	opts := asMap(optsVal)
	o := application.WebviewWindowOptions{}
	if s, ok := opts["title"].(string); ok {
		o.Title = s
	}
	if n, ok := asInt(opts["width"]); ok {
		o.Width = n
	}
	if n, ok := asInt(opts["height"]); ok {
		o.Height = n
	}
	if s, ok := opts["url"].(string); ok {
		o.URL = s
	}
	return app.Window.NewWithOptions(o)
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

// init registers the namespace directly rather than through
// rt.RegisterInstaller - the same reason the -out-pkg output does. pkg/rt
// drains its installer queue during its own package init, and Go runs an
// imported package's init first, so anything queued from here would
// arrive after the drain and silently never run.
func init() {
	ns := vm.NewNamespace("wails.shim")
	ns.Def("BridgeMethod", vm.MustBox(BridgeMethod))
	ns.Def("New", vm.MustBox(New))
	ns.Def("NewWindow", vm.MustBox(NewWindow))
	ns.Def("Register", vm.MustBox(Register))
	ns.Def("Emit", vm.MustBox(Emit))
	ns.Def("Run", vm.MustBox(Run))
	ns.Def("Quit", vm.MustBox(Quit))
	rt.RegisterNS(ns)
}
