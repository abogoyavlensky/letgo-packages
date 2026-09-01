# wails

Desktop apps for let-go: a thin wrapper over
[Wails v3](https://v3.wails.io), which pairs a Go backend with a webview
frontend. Your UI is HTML/CSS/JS; your application logic is let-go.

**Status: works, GUI unverified.** The full stack — Wails linked into an
lgx-built runtime, an app and window created from let-go, frontend calls
dispatched to let-go handlers, a single-binary build — is verified end to
end. It was verified in Wails' cgo-free `-tags server` mode (an HTTP
server plus a browser, no native webview), because the machine had no
GTK. Everything above the platform layer is the same code either way, but
the native window itself has not been run. See
[`Requirements`](#requirements) before assuming it works on your machine.

## Requirements

- [lgx](https://github.com/abogoyavlensky/lgx) with `:go/*` support, and
  a let-go carrying the out-of-tree interop work. Both are unreleased —
  see [Development](#development).
- The Go toolchain on `PATH`. lgx builds a custom `lg` that links Wails.
- **A C toolchain and the platform webview headers**, because Wails is
  cgo on the two Unix desktops:

  | Platform | Needs |
  |---|---|
  | Linux | GTK4 + WebKitGTK 6.0 dev packages, a C compiler (`libgtk-4-dev libwebkitgtk-6.0-dev` on Debian/Ubuntu) |
  | macOS | Xcode command line tools (Cocoa, WKWebView) |
  | Windows | nothing extra — WebView2 is pure Go, but let-go itself does not build for `GOOS=windows` yet |

- Node is optional. The example is a single HTML file; a real frontend
  would use Vite and `@wailsio/runtime` as in any Wails project.

## Use

```clojure
;; lgx.edn
{:paths ["src"]
 :main "main.lg"
 :lg-version "1.11.1"
 :deps {abogoyavlensky/letgo-wails {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                    :git/tag "wails-v0.1.0"
                                    :deps/root "wails"}}}
```

```clojure
(ns main
  (:require [wails.core :as w]))

(defn -main []
  ;; Handlers are ordinary let-go fns, registered by name.
  (w/handler! "greet" (fn [name] (str "Hello, " name "!")))
  (w/handler! "stats" (fn [] {:runtime "let-go" :items ["a" "b"]}))

  (let [app (w/new-app {"name" "my-app" "assets-dir" "frontend"})]
    (w/new-window app {"title" "My App" "width" 800 "height" 600})
    (w/run! app)))

;; Required: lg -b runs top-level forms at compile time.
(when-not *compiling-aot* (-main))
```

From the frontend:

```js
import { Call } from "/wails/runtime.js";

const BRIDGE = "github.com/abogoyavlensky/letgo-packages/wails/shim.Bridge.Call";
const call = (name, ...args) => Call.ByName(BRIDGE, name, args);

await call("greet", "world");   // => "Hello, world!"
await call("stats");            // => {runtime: "let-go", items: ["a", "b"]}
```

## API

| | |
|---|---|
| `(new-app opts)` | Build the application. Returns the app every other fn takes. |
| `(new-window app opts)` | Open a window. |
| `(handler! name f)` | Register `f` as the handler the frontend reaches by `name`. |
| `(emit! app name data)` | Send an event to the frontend. |
| `(run! app)` | Block on the event loop. Returns the error as a value, `nil` on a clean exit. |
| `(quit! app)` | Stop the application. |
| `bridge-method` | The fully-qualified name the frontend passes to `Call.ByName`. |

`new-app` opts: `"name"`, `"description"`, `"assets-dir"`.
`new-window` opts: `"title"`, `"width"`, `"height"`, `"url"`.
All are optional; keys are strings, not keywords, because they name Go
struct fields.

### Values across the boundary

Handler arguments arrive as let-go values. Return values are lowered to
JSON: a map becomes an object, a vector an array, a keyword its name
without the colon, `nil` becomes `null`. Throwing from a handler rejects
the frontend's promise with the exception message.

## Why there is no generated binding

The other packages here (`sql`, `sqlite`, `postgres`) lean on `lginterop`
and keep the hand-written Go to a minimum. This one generates nothing.
Two properties of the Wails API put it out of reach:

- **Every entry point takes a struct literal.** `application.New` wants an
  `application.Options`; `NewWithOptions` a `WebviewWindowOptions`. lgx
  runs `lginterop` with `-opaque-structs`, which emits no constructors, so
  let-go cannot build one. The shim assembles them from a let-go map.
- **`application.NewService` is generic**, and `lginterop` skips generics
  *silently*. Service registration is the whole point of a Wails backend,
  and it would vanish with no diagnostic.

On top of that, Wails binds a *Go type's* methods, discovered
reflectively — and a let-go fn is not a Go method. So the shim exposes one
bound method, `Bridge.Call`, which dispatches by name. Wails resolves
bound methods by fully-qualified name at run time (`Call.ByName`), so
`wails3 generate bindings` is not part of the picture. You trade generated
TypeScript types for a three-line JS helper.

## Layout

```
wails/
├── lgx.edn        deps: the shim (:go/local). No :go/interop at all.
├── shim/          the only Go: options assembly, service registration,
│   └── shim.go      the let-go dispatcher, and value lowering
├── src/wails/
│   └── core.lg    the veneer — new-app, new-window, handler!, run!
└── example/       a runnable app used to verify the whole stack
```

## Running the example

```
cd example
lgx run
```

The first run builds the custom runtime (a minute or so, once). On a
machine without the platform webview headers you can still exercise
everything but the window, in Wails' server mode:

```
GOFLAGS=-tags=server lgx run          # then open http://localhost:8080
```

Note that server mode also needs `CGO_ENABLED=0`, because a few of Wails'
internal packages gate on `linux && cgo` without a `!server` guard.

## Known limits

- **No cross-compilation.** lgx forces `CGO_ENABLED=0` on every
  `lgx build --target`, and a GUI build needs cgo on Linux and macOS.
  Build on the machine you are shipping for. (Wails has the same
  constraint; it ships a Docker image for Linux cross-builds.)
- **No Windows.** It is the one platform where Wails needs no cgo, but
  let-go does not build for `GOOS=windows` yet — `pkg/rt/term.go` uses
  `golang.org/x/sys/unix` unguarded.
- **Assets are served from a directory**, so a built binary is not
  self-contained. Embedding needs `//go:embed` in a Go module that can see
  your frontend, which this package cannot be. Add a tiny
  `{:go/local "assets"}` module of your own that embeds `frontend/dist`
  and passes the `http.Handler`.
- **`lgx` cannot pass Go build tags.** Wails wants `-tags production` for
  release builds. Use the `GOFLAGS` environment variable until lgx grows a
  config key for it.
- **The window API is minimal.** Menus, dialogs, systray and the rest of
  Wails are reachable, but each needs a wrapper in `shim.go` — one func
  per entry point, for the struct-literal reason above.

## Development

While the packages are unreleased, `lgx.edn` points at the shim with
`{:go/local "shim"}` and `shim/go.mod` requires let-go at a placeholder
`v0.0.0`, which the runtime module's `replace` governs. To build against a
let-go working tree:

```
LGX_LETGO_REPLACE=/path/to/let-go lgx run
```

The dev loop is fast once the first build is done: about 0.3s when only
`.lg` files changed, about 1.4s after a Go edit to the shim (a full
recompile and relink of the runtime).
