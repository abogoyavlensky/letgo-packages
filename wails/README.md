# wails

Desktop apps for let-go: a thin wrapper over
[Wails v3](https://v3.wails.io), which pairs a Go backend with a webview
frontend. Your UI is HTML/CSS/JS; your application logic is let-go.

**Status: works.** The native window was verified on macOS with Wails
v3.0.0-beta.16: a WKWebView window created from let-go, handlers answering
frontend calls, a let-go map arriving as a JSON object, and a single-binary
build. v0.2.0 moves to beta.30 and adds menus, dialogs, events from the
frontend, window controls and resource-served assets. Its tests run in
Wails' cgo-free server mode; the native side of the new pieces still needs
a check on a desktop. The Linux GTK4/WebKitGTK window has never been run.

## Requirements

- [lgx](https://github.com/abogoyavlensky/lgx) 0.4.2 or newer (the
  in-tree shim relies on its `:go/local` stamp), and let-go 1.13.0 or
  newer, the first release carrying the interop work (see [Use](#use)).
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
 :resource-paths ["resources"]     ; resources/public is the frontend
 :main "main.lg"
 :lg-runtime :built
 :lg-version "1.13.0"
 :deps {abogoyavlensky/letgo-wails {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                    :git/tag "wails-v0.3.0"
                                    :deps/root "wails"}}}
```

```clojure
(ns main
  (:require [wails.core :as w]))

(defn greet [name] (str "Hello, " name "!"))

(defn -main []
  ;; Register the var, not the fn: a var is re-read on every call, so
  ;; redefining greet changes the running app.
  (w/handler! "greet" #'greet)

  (let [app (w/new-app {:name "my-app" :assets "public"})]
    (w/new-window app {:title "My App" :width 800 :height 600})
    (w/set-menu! app [{:role :app-menu} {:role :edit-menu} {:role :window-menu}])
    (w/run! app)))

;; Required: lg -b runs top-level forms at compile time.
(when-not *compiling-aot* (-main))
```

From the frontend:

```js
import { Call, Events } from "/wails/runtime.js";

const BRIDGE = "github.com/abogoyavlensky/letgo-packages/wails/shim.Bridge.Call";
const call = (name, ...args) => Call.ByName(BRIDGE, name, args);

await call("greet", "world");        // => "Hello, world!"
Events.On("tasks-changed", reload);  // an emit! from let-go
Events.Emit("saved", { id: 3 });     // reaches an on-event! listener
```

## API

Options maps take keyword or string keys alike (`{:title "x"}` or
`{"title" "x"}`).

| | |
|---|---|
| `(new-app opts)` | Build the application. `:name`, `:description`, `:assets` (resource prefix, preferred), `:assets-dir` (cwd-relative dir), `:quit-on-last-window-closed`, `:on-shutdown` (fn). |
| `(new-window app opts)` | Open a window and return it. `:title`, `:width`, `:height`, `:min-width`, `:min-height`, `:url`, `:hidden`. |
| `(set-title! win s)` `(show! win)` `(hide! win)` `(close! win)` `(focus! win)` `(reload! win)` `(exec-js! win js)` | Window controls. Safe from any thread: Wails moves each onto the main thread. |
| `(on-main! f)` | Run `f` on the main thread and wait, for anything else that must run there. |
| `(handler! name f)` | Register `f` (a fn or a var) as the handler the frontend reaches by `name`. |
| `(emit! app name data)` | Send an event to the frontend. |
| `(on-event! app name f)` | Call `(f data)` when the frontend emits `name`. Returns a fn that removes the listener. |
| `(set-menu! app spec)` | Install the application menu from data (below). Calling it again rebuilds the menu. |
| `(check-menu spec)` | Validate a menu spec without installing it: true, or throws naming the bad item. |
| `(open-file app opts)` `(save-file app opts)` | File dialogs; the chosen path, or nil on cancel. `:title`, `:filters` `[["JSON" "*.json"]]`, and `:filename` for save. |
| `(message! app kind title body)` | A message dialog; `kind` is `:info`, `:warning` or `:error`. |
| `(data-dir app-name)` | The per-user data directory (`~/Library/Application Support/<name>`, `~/.config/<name>`), created if missing. |
| `(watch-assets! win dir)` | Dev only: reload `win` when a file under `dir` changes. Returns a stop fn. |
| `(dev-server-url)` | The frontend dev server the assets come from (`FRONTEND_DEVSERVER_URL`), or `nil`. Always `nil` in a `production` build. |
| `(run! app)` | Block on the event loop. Call it from the entry fn, on the main thread. Returns the error as a value, `nil` on a clean exit. |
| `(quit! app)` | Stop the application. |
| `bridge-method` | The fully-qualified name the frontend passes to `Call.ByName`. |

### The frontend comes from resources

`:assets "public"` serves URL `/x` as the resource `public/x` and `/` as
`public/index.html`. Under `lgx run` that reads your `:resource-paths`
from disk on every request, so an edit shows on the next reload; after
`lgx build` it reads the copy embedded in the binary. Prefer it to
`:assets-dir`, which is relative to the working directory: a macOS `.app`
started from Finder runs in `/`, and its window comes up blank.

### Menus are data

```clojure
[{:role :app-menu}                  ; macOS application menu; put it first
 {:label "File"
  :items [{:label "Export…" :accelerator "CmdOrCtrl+E" :on-click #'export!}
          {:separator true}
          {:role :quit}]}
 {:role :edit-menu}                 ; without it Cmd+C/V do nothing on macOS
 {:role :window-menu}]
```

An item is `{:separator true}`, `{:role r}`, or `{:label s}` with either
`:items` (a submenu) or `:on-click` (a fn of no args) plus an optional
`:accelerator`. Roles: `:app-menu :file-menu :edit-menu :view-menu
:window-menu :help-menu :quit :about :close-window :reload`. A malformed
spec throws with the item's path, e.g. `menu spec [1 :items 0 :role]`.

### Values across the boundary

Handler arguments and event data arrive as let-go values: objects as maps
with string keys, arrays as vectors, whole numbers as ints. Return values
are lowered to JSON: a map becomes an object, a vector an array, a keyword
its name without the colon, `nil` becomes `null`. A string result reaches
the frontend as the string. Throwing from a handler rejects the frontend's
promise with the exception message.

### Callbacks and threads

Handlers, `on-event!` listeners, menu clicks and `:on-shutdown` all run on
Wails' goroutines, not the main thread. An exception thrown by a callback
that nothing awaits (a listener, a click) is printed to stderr as
`wails: <where>: <error>`; it never takes the app down.

### Live reload

Pass vars wherever a fn is taken (`#'greet`, `:on-click #'export!`):
redefining the var changes the running app. With an nREPL inside the app
(the [`nrepl`](../nrepl) package) you can edit a namespace, load the
buffer, and the next call runs the new code. `set-menu!` again rebuilds
the menu, and `watch-assets!` reloads the window when the frontend
changes. Only what `-main` builds (the app, the windows) needs a restart.

### Dev server (HMR)

Reloading the whole window loses the page's state on every edit. For hot
module replacement, run the frontend's dev server and point the app at it:

```
npx vite                                             # in the frontend dir
FRONTEND_DEVSERVER_URL=http://localhost:5173 lgx run
```

With the variable set, the `:assets` (or `:assets-dir`) handler proxies
every asset request to the dev server, through Wails' own proxy. The page
keeps Wails' origin (`wails://localhost:5173` on macOS), so the bridge
under `/wails/*` keeps answering. Wails waits up to 5 s for the dev server
at startup and exits if it does not answer. Skip `watch-assets!` in this
mode, since the dev server reloads the page itself.

Vite's HMR websocket has to reach Vite directly. In server mode the page
is served from Wails' port, where nothing answers a websocket, so fix the
port in `vite.config.ts`:

```js
server: {
  port: 5173,
  strictPort: true,
  ws: { protocol: "ws", host: "localhost", clientPort: 5173 },
}
```

A build with the `production` tag ignores the variable and always serves
the embedded frontend.

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
├── lgx.edn        deps: the in-tree shim ({:go/local "shim"}); no :go/interop
├── shim/          the only Go, one concern per file
│   ├── shim.go      the bridge, the handler registry, namespace registration
│   ├── app.go       new-app, events, run/quit, data-dir
│   ├── window.go    windows, their controls, on-main!, the asset watcher
│   ├── menu.go      the menu spec builder
│   ├── dialogs.go   file and message dialogs
│   ├── assets.go    the frontend served from resources
│   ├── callback.go  invoking let-go from Wails goroutines
│   └── convert.go   values across the boundary
├── src/wails/
│   └── core.lg    the veneer
├── test/          server-mode tests (see Development)
└── example/       a runnable app
```

## Running the example

```
cd example
lgx run
```

The first run builds the custom runtime, which on macOS takes a few
minutes the first time — the Objective-C compile of the Cocoa layer is the
slow part. After that it is cached.

On a machine without the platform webview headers you can still exercise
everything but the window, in Wails' server mode:

```
CGO_ENABLED=0 GOFLAGS=-tags=server lgx run    # then open http://localhost:8080
```

Both are needed: a few of Wails' internal packages gate on `linux && cgo`
without a `!server` guard. `WAILS_SERVER_PORT` moves the port.

## Known limits

- **No cross-compilation.** lgx forces `CGO_ENABLED=0` on every
  `lgx build --target`, and a GUI build needs cgo on Linux and macOS.
  Build on the machine you are shipping for. (Wails has the same
  constraint; it ships a Docker image for Linux cross-builds.)
- **No Windows.** It is the one platform where Wails needs no cgo, but
  let-go does not build for `GOOS=windows` yet — `pkg/rt/term.go` uses
  `golang.org/x/sys/unix` unguarded.
- **Build tags.** Wails wants `-tags production` for release builds.
  lgx 0.5.1 has a `:go/build-tags` key (`:targets {:bin
  {:go/build-tags ["production"]}}`), merged with any `GOFLAGS`; with
  0.5.0 or older, use the `GOFLAGS` environment variable.
- **Not everything is wrapped.** Tray icons, the clipboard, context menus,
  global shortcuts and screens are reachable from Wails but have no
  wrapper yet; each needs a func in the shim, for the struct-literal
  reason above.

## Development

The shim lives in-tree as `{:go/local "shim"}`, so editing a `.go` file is
picked up on the next `lgx` command. The tests run the real app in server
mode, with `run!` on a future:

```
cd wails
CGO_ENABLED=0 GOFLAGS=-tags=server lgx test
```

CI runs them the same way. To build against a let-go working tree:

```
LGX_LETGO_REPLACE=/abs/path/to/let-go lgx run
```

The dev loop is fast once the first build is done: about 0.3s when only
`.lg` files changed, about 1.4s after a Go edit to the shim (a full
recompile and relink of the runtime).
