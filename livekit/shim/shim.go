// Package shim embeds the LiveKit SFU in a let-go binary.
//
// Like the Wails wrapper, this package uses no generated bindings.
// lginterop can carry none of the livekit-server API:
//
//   - Every server entry point takes a struct. The server is assembled
//     by google/wire from a *config.Config, and lgx runs lginterop with
//     -opaque-structs, so no constructors are emitted and let-go has no
//     way to build one. Start builds it here from a let-go map, by way
//     of the same YAML/JSON parser livekit-server's own CLI uses.
//
//   - Starting the server is a sequence, not a call: parse, validate
//     keys, load TURN secrets, create the local node, initialise
//     metrics, wire the server, then run its blocking Start on a
//     goroutine and wait for it to come up. Start does that sequence
//     once, in the order cmd/server/main.go does.
//
// livekit-server only compiles against LiveKit's forks of three pion
// modules. Go ignores a dependency's replace directives, so the forks
// reach the build through :go/replace on this module's coord in
// livekit/lgx.edn, not through this module's go.mod.
package shim

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/livekit/livekit-server/pkg/config"
	"github.com/livekit/livekit-server/pkg/routing"
	"github.com/livekit/livekit-server/pkg/service"
	"github.com/livekit/livekit-server/pkg/telemetry/prometheus"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

// startTimeout bounds how long Start waits for the server to report
// running before it gives up.
const startTimeout = 15 * time.Second

// Server is the handle let-go holds, opaque on that side. done receives
// the return value of the server's blocking Start: nil after a clean
// Stop, an error when startup failed.
type Server struct {
	srv  *service.LivekitServer
	done chan error
}

// Start parses conf (a let-go map of LiveKit's YAML config keys), starts
// the server, and returns once it is accepting connections.
//
// A start that fails is fatal for the process, not something to retry.
// Upstream's Start opens the router, the IO service and its TCP
// listeners one at a time before it marks itself running, and a later
// failure returns without closing what it already opened; Stop returns
// early while the server is not running, so nothing here can reclaim
// that state either. livekit-server's own CLI exits on a start error.
//
// When the wait times out with the server still starting, the caller
// never gets a handle, so a reaper goroutine takes ownership: it stops
// the server the moment it comes up, or exits when startup fails.
func Start(confVal vm.Value) (*Server, error) {
	body, err := json.Marshal(asMap(confVal))
	if err != nil {
		return nil, fmt.Errorf("livekit: encoding config: %w", err)
	}
	// strictMode rejects unknown keys, which is what turns a typo in the
	// config map into an error instead of a silently ignored setting. A
	// nil CLI command skips the flag overrides.
	conf, err := config.NewConfig(string(body), true, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("livekit: %w", err)
	}
	config.InitLoggerFromConfig(&conf.Logging)
	if err := conf.ValidateKeys(); err != nil {
		return nil, fmt.Errorf("livekit: %w", err)
	}
	if err := conf.LoadTURNSecrets(); err != nil {
		return nil, fmt.Errorf("livekit: %w", err)
	}
	node, err := routing.NewLocalNode(conf)
	if err != nil {
		return nil, fmt.Errorf("livekit: %w", err)
	}
	// Idempotent: a second Init in one process is a no-op, which is what
	// lets a halted server be started again (integrant's halt/init cycle).
	if err := prometheus.Init(string(node.NodeID()), node.NodeType()); err != nil {
		return nil, fmt.Errorf("livekit: %w", err)
	}
	srv, err := service.InitializeServer(conf, node)
	if err != nil {
		return nil, fmt.Errorf("livekit: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- srv.Start() }()

	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	deadline := time.Now().Add(startTimeout)
	for {
		select {
		case err := <-done:
			if err == nil {
				err = errors.New("server exited during startup")
			}
			return nil, fmt.Errorf("livekit: %w", err)
		case <-tick.C:
			if srv.IsRunning() {
				return &Server{srv: srv, done: done}, nil
			}
			if time.Now().After(deadline) {
				go reap(srv, done)
				return nil, fmt.Errorf("livekit: server did not start within %s", startTimeout)
			}
		}
	}
}

// reap owns a server whose Start outlived the startup wait.
func reap(srv *service.LivekitServer, done chan error) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			if srv.IsRunning() {
				srv.Stop(true)
				return
			}
		}
	}
}

// Stop shuts the server down and returns once it has. Without force it
// first waits for connected participants to leave.
func Stop(s *Server, force bool) { s.srv.Stop(force) }

// Running reports whether the server is accepting connections.
func Running(s *Server) bool { return s.srv.IsRunning() }

// HTTPPort is the port the HTTP and signalling API listens on.
func HTTPPort(s *Server) int { return s.srv.HTTPPort() }

// ToGo lowers a let-go value to something encoding/json can marshal.
// Copied from the wails shim: the shims are independent Go modules, and
// sharing it would take a third one.
//
// Unbox alone is not enough: a map and a vector both unbox to themselves.
// A let-go map is a *vm.PersistentMap whose Seq yields vm.MapEntry, so a
// type switch on vm.Map alone silently turns every map into a list of
// pairs.
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

// keyString renders a map key as a config key. Keywords lose their
// leading colon, so {:port 7880} and {"port" 7880} are the same config.
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
// map[string]any, so every entry point taking options declares vm.Value
// and converts here.
func asMap(v vm.Value) map[string]any {
	if m, ok := ToGo(v).(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// init registers the namespace directly rather than through
// rt.RegisterInstaller: pkg/rt drains its installer queue during its own
// package init, which Go runs before this one, so anything queued from
// here would never run.
func init() {
	ns := vm.NewNamespace("livekit.shim")
	ns.Def("Start", vm.MustBox(Start))
	ns.Def("Stop", vm.MustBox(Stop))
	ns.Def("Running", vm.MustBox(Running))
	ns.Def("HTTPPort", vm.MustBox(HTTPPort))
	rt.RegisterNS(ns)
}
