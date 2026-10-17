# nrepl

An nREPL server inside a running let-go program. Connect an editor to a
live app and redefine a fn, inspect state, or reload a namespace, with no
restart.

```clojure
{:deps {abogoyavlensky/letgo-nrepl
        {:git/url "https://github.com/abogoyavlensky/letgo-packages"
         :git/tag "nrepl-v0.1.0"
         :deps/root "nrepl"}}}
```

```clojure
(require '[nrepl.core :as nrepl]
         '[string :as str])

;; LGX_RUN is set by `lgx run` and absent in a built binary. Check for a
;; non-blank value: os/getenv returns "" for an unset variable, and "" is
;; truthy, so a bare (when (os/getenv "LGX_RUN") ...) is always on.
(when-not (str/blank? (os/getenv "LGX_RUN"))
  (nrepl/start!))             ; => the bound port; also writes .nrepl-port
```

## Why it exists

`lg -n` serves nREPL only once the given script has returned. A program
that never returns, such as a Wails event loop or an `http/serve` process,
never gets a server, and `lgx nrepl` starts a separate process that cannot
see the running one. `start!` builds the server inside the process
instead. Namespaces are process-global, so every var the program defined
is visible to it.

## API

| Fn | Does |
|---|---|
| `(start!)`, `(start! {:port n})` | Serve on `127.0.0.1`, port 0 (the default) picks a free one. Returns the port and writes `.nrepl-port`. Throws if a server is already running. |
| `(stop!)` | Stop accepting and remove `.nrepl-port`. Live connections end when their clients disconnect. No-op when stopped. |
| `(port)` | The running port, or nil. |

## Things to know

- **Gate it.** The server is a dev tool: anything that can reach the port
  can run code in your process. It binds loopback only, and `start!` is
  meant to sit behind a check like the `LGX_RUN` one above.
- **One namespace for everyone.** let-go's `*ns*` is process-global, so
  sessions evaluate in the process's current namespace and share it.
  `(in-ns 'my.ns)` works as in any REPL. Editors that send `ns` with each
  eval handle this for you.
- **Make the parts you reload hot.** A redefined var takes effect wherever
  the var is called through. Callbacks registered as fn values keep the old
  fn, so register vars (`#'handler`) instead, and keep state in `defonce`.
- **Reload a namespace by loading its file.** let-go has no
  `(require 'x :reload)`. An editor's load-file sends the whole buffer,
  which re-evaluates the namespace in place.
- **Main-thread work.** Evaluation runs on the server's own goroutine. That
  is fine for logic and state. For UI frameworks that require the main
  thread, go through the framework's hop (in Wails, `wails.core/on-main!`).

## Tests

```sh
cd nrepl && lgx test
```

The tests drive a real server over TCP with a small python3 client
(`test/nrepl/client.py`).
