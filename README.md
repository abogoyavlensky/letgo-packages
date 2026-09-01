# letgo-packages

Wrapper libraries for [let-go](https://github.com/nooga/let-go), consumed
with [lgx](https://github.com/abogoyavlensky/lgx).

One repository, one directory per package, each tagged independently.
Depend on one with `:deps/root`:

```clojure
{:deps {abogoyavlensky/letgo-sqlite
        {:git/url "https://github.com/abogoyavlensky/letgo-packages"
         :git/tag "sqlite-v0.1.0"
         :deps/root "sqlite"}}}
```

| Package | What it is |
|---|---|
| [`sql/`](sql/) | The driver-agnostic SQL layer: `database/sql` bindings, a Go shim, and a next.jdbc-shaped API (`execute!`, `execute-one!`, `query`, `with-transaction`) |
| [`sqlite/`](sqlite/) | SQLite driver over `sql/`, via the pure-Go `modernc.org/sqlite` |
| [`postgres/`](postgres/) | PostgreSQL driver over `sql/`, via the pure-Go `github.com/jackc/pgx/v5` |
| [`wails/`](wails/) | Desktop apps over [Wails v3](https://v3.wails.io): a webview frontend with let-go handlers behind it |

The driver packages are thin: `open`/`close!` plus re-exports of the
`sql` API. An app depends on one driver package; lgx's transitive
`:go/*` dep collection pulls the `sql` layer's bindings and shim up
through the driver's `:local/root` dep and links everything in one
custom-runtime build.

## Rules for driver packages

**Pure Go only.** A driver that needs cgo would force a C toolchain on
every user and break cross-compiled builds. Both current drivers are
pure Go; any future one must be too.

This rule is about *drivers*. `wails/` is cgo by necessity — the platform
webview is a C library on Linux and macOS — and it pays exactly the price
the rule exists to avoid: a C toolchain per developer, and no
cross-compilation. That is inherent to desktop UI, not a choice, and it is
why the package documents it up front.

## Tagging status

Nothing is tagged yet, and two blockers stand:

1. lgx reads a dependency's `lgx.edn` from the checkout root rather than
   from `:deps/root`, so external consumers would silently miss a
   package's Go coords. Fixed by the lgx cross-compilation work; do not
   tag before that fix ships.
2. `sql/shim/go.mod` requires let-go at a placeholder `v0.0.0` and needs
   a real release to pin - one that carries the `[]any` boxing fix (in
   let-go's `integration/go-interop`, unreleased as of this writing).

Development is unaffected: each `example/` uses `:local/root ".."`, and
runtime builds use `LGX_LETGO_REPLACE` pointed at a let-go checkout.
