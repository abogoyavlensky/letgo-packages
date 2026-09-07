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

Nothing is tagged yet. Both original blockers are cleared: lgx now reads a
package's `lgx.edn` from `:deps/root`, and let-go's `[]any` boxing fix is
merged. What remains is a let-go **release** to pin `sql/shim/go.mod` against —
it currently requires a placeholder `v0.0.0`, and the newest release predates
the merged interop work.

Until then, consumers pin `:lg-version` to a commit on let-go's `main`, which
builds the whole stack from the module proxy with no let-go checkout:

```clojure
{:lg-version "f26eb497299760e93ce430302f13ab3a954eab64"}
```

Each `example/` uses `:local/root ".."` so it tests the working tree.
`LGX_LETGO_REPLACE` is now only for developing against uncommitted let-go
changes — a sha pin covers everything else.
