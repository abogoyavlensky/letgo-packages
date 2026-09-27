# livekit

Realtime audio and video for let-go: the [LiveKit](https://livekit.io)
SFU embedded in your let-go binary, plus join-token minting and webhook
verification. There is no separate media server to deploy. Your app starts
the SFU and hands clients tokens for it.

**Status: works.** Verified end to end on linux/amd64 with livekit-server
v1.13.7. That covers the server started from let-go, the room API answering
tokens minted from let-go, webhook verification, two integrant halt/init
cycles in one process, and a single-binary build. The example binary is
78 MB.

## Requirements

- [lgx](https://github.com/abogoyavlensky/lgx) 0.4.2 or newer for cached
  runs. 0.4.1 works (it has `:go/replace`) but rebuilds the runtime on
  every command. let-go 1.13.0 or newer.
- The Go toolchain on `PATH`. lgx builds a custom `lg` that links
  livekit-server. The first build downloads LiveKit's module graph and
  takes a minute or more; after that the runtime is cached.
- No C toolchain: everything here is pure Go (`CGO_ENABLED=0` works).

## Use

```clojure
;; lgx.edn
{:paths ["src"]
 :main "main.lg"
 :lg-runtime :built
 :lg-version "1.13.0"
 :deps {abogoyavlensky/letgo-livekit {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                      :git/tag "livekit-v0.1.0"
                                      :deps/root "livekit"}}}
```

```clojure
(ns main
  (:require [livekit.core :as lk]))

(def api-key "devkey")
(def api-secret "a-secret-of-at-least-32-characters!")

(defn -main []
  (let [server (lk/start! {:port 7880
                           :rtc {:tcp_port 7881
                                 :port_range_start 50000
                                 :port_range_end 60000
                                 :use_external_ip true}
                           :keys {api-key api-secret}
                           :logging {:level "info"}})]
    ;; Hand this to a browser client (livekit-client) to join "demo".
    (println (lk/token api-key api-secret
                       {:identity "alice" :room "demo" :room-join true}))
    @(promise)))

;; Required: lg -b runs top-level forms at compile time.
(when-not *compiling-aot* (-main))
```

### Config

`start!` takes LiveKit's own YAML config as a let-go map, with the same key
names (`bind_addresses`, `tcp_port`, not kebab-case). Keys can be strings or
keywords; a keyword is read as its name, so `{:port 7880}` and
`{"port" 7880}` are the same. The map is parsed strictly, so a misspelled key
is an error rather than a silently ignored setting. Every key is in
[LiveKit's config reference](https://docs.livekit.io/home/self-hosting/deployment/).
Secrets must be at least 32 characters.

### Integrant

```clojure
(ns app.system
  (:require [integrant.core :as ig]
            [livekit.integrant]))

(def config
  {:livekit/server {:config {:port 7880 :keys {"devkey" "..."}}}
   :app/handler {:livekit (ig/ref :livekit/server)}})
```

The component's value is the server handle. `halt-key!` stops it
gracefully, waiting for connected participants to leave. A system can be
halted and initialised again in the same process.

## API

| | |
|---|---|
| `(start! config)` | Start the server and return its handle once it accepts connections. Throws on failure or after 15s. |
| `(stop! server)` / `(stop! server force?)` | Stop and return once stopped. Without `force?`, first waits for participants to leave. |
| `(running? server)` | True while the server accepts connections. |
| `(http-port server)` | The HTTP/signalling port. |
| `(token api-key secret opts)` | Mint an access token (JWT). |
| `(verify-webhook auth-header body api-key secret)` | True when a webhook is genuine. |

`token` opts, all optional: `:identity`, `:name`, `:room`, `:ttl-seconds`
(default 6 hours), the grant booleans `:room-join`, `:room-create`,
`:room-list`, `:room-admin`, `:can-publish`, `:can-subscribe` and
`:can-publish-data`, and `:sha256`, the body claim a webhook carries.
Joining a room needs `:identity`, `:room` and `:room-join true`. Calls to
the server's room API (`/twirp/livekit.RoomService/...`, a plain HTTP POST
with `Authorization: Bearer <token>`) need `:room-create`/`:room-list`/
`:room-admin`.

`verify-webhook` takes the request's `Authorization` header and raw body.
It checks that the token was issued by `api-key`, is signed with `secret`
and unexpired, and that its `sha256` claim matches the body. Any failure is
`false`.

## Why there is no generated binding

Every livekit-server entry point takes a struct (`*config.Config`) or is
wired by `google/wire`, and the token API takes `*auth.VideoGrant`, a struct
with pointer-typed booleans. lgx runs `lginterop` with `-opaque-structs`,
which emits no constructors, so let-go cannot build any of them. The shim
assembles them from let-go maps and exposes six plain functions.

## The pion forks, and bumping livekit-server

livekit-server does not compile against upstream pion. Its `go.mod`
replaces three modules with LiveKit's forks, and Go honours `replace` only
in the main module, which here is the runtime lgx generates. So this
package's `lgx.edn` carries the same three on the shim coord as
`:go/replace`, and lgx writes them into every consumer's runtime.

**When bumping livekit-server, copy the replace block from the new tag's
`go.mod` into `lgx.edn`** (and into `shim/go.mod`, where it is
documentation). A stale block fails the runtime build with errors like
`se.EnableSped undefined`. If your project also replaces one of those
modules with a different target, lgx stops with a conflict naming both.

## Layout

```
livekit/
├── lgx.edn        the shim coord with :go/replace, integrant deps
├── shim/          the only Go: server lifecycle, tokens, webhook check
│   └── shim.go
├── src/livekit/
│   ├── core.lg        the veneer: start!, stop!, token, verify-webhook
│   └── integrant.lg   the :livekit/server component
├── test/livekit/  runs a real server on 127.0.0.1:7890
└── example/       an integrant system; exits 0 when every check passes
```

## Running the example and tests

```
cd example && lgx run      # or: lgx build && ./bin/app
cd .. && lgx test
```

Both use ports 7890 and 7891 on 127.0.0.1.

## Known limits

- **A failed `start!` is fatal for the process.** LiveKit opens its router,
  IO service and listeners one at a time, and a start that fails partway
  does not close what it already opened. Restart the process instead of
  retrying. livekit-server's own CLI exits in the same case.
- **Platforms.** Verified on linux/amd64. A native macOS build should
  work but has not been run yet. `lgx build --target darwin/*` from Linux
  fails (`hwstats ... undefined: cpu.Stats`), because LiveKit's `hwstats`
  needs cgo on darwin and cross-builds run with `CGO_ENABLED=0`. Build on
  a Mac for macOS.
- **Size.** The custom runtime and every binary built from it are about
  80 MB.
- **Networking is still WebRTC's.** Embedding changes nothing about
  reachability: clients need the server's public IP with the UDP port range
  open, or TURN enabled (`:turn {:enabled true ...}`).

## Development

The shim ships in-tree: `lgx.edn` declares it `{:go/local "shim"}`, and a
release is the `livekit-vX.Y.Z` package tag alone, with no Go module tag.
Edit `shim/shim.go` and the next `lgx run` or `lgx test` rebuilds the
runtime; edits to `.lg` files alone reuse it. To build against a let-go
working tree:

```
LGX_LETGO_REPLACE=/path/to/let-go lgx run
```
