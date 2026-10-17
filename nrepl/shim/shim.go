// Package shim serves nREPL from inside a running let-go program.
//
// let-go ships an nREPL server, but the only way to start it is `lg -n`,
// and lg starts it after the given script has run (pkg/cli: the server is
// created once the files are done). A program that never returns - a Wails
// event loop, an http/serve process - therefore never gets one. This shim
// builds the server for the process it is linked into, so an editor can
// evaluate against the live program: redefine a handler, inspect state,
// reload a namespace with load-file.
//
// Namespaces live in a process-global registry, so a fresh compiler
// context sees every var the program has defined. Evaluation runs in the
// process's current namespace (let-go's *ns* is process-global; nREPL
// sessions share it), and (in-ns 'x) switches it as in any REPL.
package shim

import (
	"fmt"
	"sync"

	"github.com/nooga/let-go/pkg/compiler"
	"github.com/nooga/let-go/pkg/nrepl"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

var (
	mu     sync.Mutex
	server *nrepl.NreplServer
)

// Start serves nREPL on 127.0.0.1:port - 0 lets the OS pick a free port -
// and returns the bound port. The server also writes .nrepl-port in the
// working directory, which is where editors look for it. One server per
// process: a second Start while one runs is an error naming its port.
func Start(port int) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	if server != nil {
		return 0, fmt.Errorf("nREPL already running on port %d", server.Port())
	}
	// NewCompiler sets the process-wide current namespace to the one it is
	// given. Hand it the current one and restore the root afterwards, so
	// starting a server changes nothing for the caller.
	root := rt.CurrentNS.Root()
	cur, ok := rt.CurrentNS.Deref().(*vm.Namespace)
	if !ok {
		cur = rt.NS("user")
	}
	ctx := compiler.NewCompiler(vm.NewConsts(), cur)
	rt.CurrentNS.SetRoot(root)

	s := nrepl.NewNreplServer(ctx)
	if err := s.Start(port); err != nil {
		return 0, err
	}
	server = s
	return s.Port(), nil
}

// Stop shuts the server down and removes .nrepl-port. Open client
// connections are not closed: the server stops accepting, and each live
// connection ends when its client disconnects. A no-op when none is
// running.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if server == nil {
		return
	}
	server.Stop()
	server = nil
}

// Port is the running server's port, or 0 when none is running.
func Port() int {
	mu.Lock()
	defer mu.Unlock()
	if server == nil {
		return 0
	}
	return server.Port()
}

// init registers the namespace directly rather than through
// rt.RegisterInstaller. pkg/rt drains its installer queue during its own
// package init, and Go runs an imported package's init first, so anything
// queued from here would arrive after the drain and silently never run.
func init() {
	ns := vm.NewNamespace("nrepl.shim")
	ns.Def("Start", vm.MustBox(Start))
	ns.Def("Stop", vm.MustBox(Stop))
	ns.Def("Port", vm.MustBox(Port))
	rt.RegisterNS(ns)
}
