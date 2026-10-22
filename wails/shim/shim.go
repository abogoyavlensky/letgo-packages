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
	"errors"
	"fmt"
	"sync"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
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
		val, err := FromJSON(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d of %q: %w", i, name, err)
		}
		lgArgs[i] = val
	}
	res, err := fn.Invoke(lgArgs)
	if err != nil {
		return nil, handlerError(err)
	}
	return ToGo(res), nil
}

// handlerError is what the frontend's rejected promise carries. A handler
// that throws (ex-info "a task needs a title" {}) reaches Go wrapped in the
// VM's call chain ("ExecutionError: calling title! caused by ..."); the
// frontend wants the message the handler wrote, so unwrap to the thrown
// value. Anything else - a Go error, a VM fault - keeps its full text.
func handlerError(err error) error {
	// The VM links its errors through GetCause, not Unwrap, so errors.As
	// does not see the thrown value; walk the chain by hand.
	for e := err; e != nil; {
		if te, ok := e.(*vm.ThrownError); ok {
			if ei, ok := te.Value.(*vm.ExInfo); ok {
				return errors.New(ei.Message())
			}
			return errors.New(te.Value.String())
		}
		c, ok := e.(interface{ GetCause() error })
		if !ok {
			break
		}
		e = c.GetCause()
	}
	return err
}

// Register binds a let-go fn to a name the frontend can call.
func Register(name string, fn vm.Fn) { handlers.Store(name, fn) }

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
	ns.Def("OnEvent", vm.MustBox(OnEvent))
	ns.Def("SetTitle", vm.MustBox(SetTitle))
	ns.Def("Show", vm.MustBox(Show))
	ns.Def("Hide", vm.MustBox(Hide))
	ns.Def("Close", vm.MustBox(Close))
	ns.Def("Focus", vm.MustBox(Focus))
	ns.Def("Reload", vm.MustBox(Reload))
	ns.Def("ExecJS", vm.MustBox(ExecJS))
	ns.Def("InvokeSync", vm.MustBox(InvokeSync))
	ns.Def("CheckMenu", vm.MustBox(CheckMenu))
	ns.Def("SetMenu", vm.MustBox(SetMenu))
	ns.Def("OpenFile", vm.MustBox(OpenFile))
	ns.Def("SaveFile", vm.MustBox(SaveFile))
	ns.Def("Message", vm.MustBox(Message))
	ns.Def("DataDir", vm.MustBox(DataDir))
	ns.Def("WatchAssets", vm.MustBox(WatchAssets))
	ns.Def("DevServerURL", vm.MustBox(DevServerURL))
	rt.RegisterNS(ns)
}
