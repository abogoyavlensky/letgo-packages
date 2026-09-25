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
| [`duckdb/`](duckdb/) | DuckDB, an in-process analytical database, over `sql/`, via the cgo `github.com/duckdb/duckdb-go/v2`; DuckDB values arrive as plain let-go values |
| [`wails/`](wails/) | Desktop apps over [Wails v3](https://v3.wails.io): a webview frontend with let-go handlers behind it |
| [`ragtime/`](ragtime/) | Schema migrations with [ragtime](https://github.com/weavejester/ragtime)'s core: a `DataStore` and a `Migration` over `sql/`, so any driver package works |

The driver packages are thin: `open`/`close!` plus re-exports of the
`sql` API. An app depends on one driver package; lgx's transitive
`:go/*` dep collection pulls the `sql` layer's bindings and shim up
through the driver's `sql` dep and links everything in one
custom-runtime build.

## Rules for driver packages

**Pure Go, unless no pure-Go driver exists.** A driver that needs cgo
forces a C toolchain on every user and breaks cross-compiled builds, so
a pure-Go driver always wins when there is one: sqlite and postgres are
pure Go for that reason.

`duckdb/` is the exception, because DuckDB has no pure-Go
implementation — its only Go driver links the C++ library. It pays the
full price: a C toolchain per developer, native builds only, and a
binary about 75 MB larger. Its README puts that first, so nobody finds
out at deploy time.

This rule is about *drivers*. `wails/` is cgo by necessity — the platform
webview is a C library on Linux and macOS — and it pays exactly the price
the rule exists to avoid: a C toolchain per developer, and no
cross-compilation. That is inherent to desktop UI, not a choice, and it is
why the package documents it up front.

## Releasing

Two kinds of tags live in this repo:

| Tag | Form | Read by |
|---|---|---|
| Go module tag | `<pkg>/shim/vX.Y.Z` (e.g. `sql/shim/v0.1.0`) | `go get`, through the module proxy. Go dictates the form: a module whose `go.mod` sits in a subdirectory is versioned by a tag prefixed with that path. |
| Package tag | `<pkg>-vX.Y.Z` (e.g. `sqlite-v0.1.0`) | lgx, via `:git/tag`. Go ignores tags that are not semver. |

Only `sql`, `wails` and `duckdb` have a shim. `sqlite`, `postgres` and
`ragtime` have none, and their release is the package tag alone. All four
SQL packages depend on `sql` by package tag
(`{:git/url ... :git/tag "sql-vX.Y.Z" :deps/root "sql"}`) and inherit its
shim through it. Every package in a release round names the same
`sql-vX.Y.Z`, byte for byte: lgx then resolves the four coords to one
checkout, and a consumer that mixes two of them gets no `already resolved`
warning. The local `{:local/root "../sql"}` lives only in each driver's
`:test` context, so `lgx test` runs against the working tree. ragtime has
no such override: its tests pull sqlite, which names the tag, and a local
`sql` beside it would clash.

When a shim changed, in this order:

1. Tag `<pkg>/shim/vX.Y.Z` on the commit that contains the shim change
   and push the tag.
2. From a throwaway module, require let-go first, then
   `go get github.com/abogoyavlensky/letgo-packages/<pkg>/shim@vX.Y.Z`.
   It must report plain `vX.Y.Z`, not a pseudo-version.
3. Set `<pkg>/lgx.edn` to `{:go/version "vX.Y.Z"}` and commit.
4. Tag every affected package `<pkg>-vX.Y.Z` on that commit and push.

The order matters: the coord in step 3 names a tag that `go get` fetches
from GitHub, so the Go tag has to exist before the commit that references
it, and the package tag has to follow that commit so consumers receive the
flipped `lgx.edn`. When only `.lg` files changed, do step 4 alone.

When `sql/` changed, release it before its dependents:

1. Release `sql` itself: the shim steps above if its shim changed, then
   tag `sql-vX.Y.Z` and push.
2. Set the `letgo-sql` coord in `sqlite`, `postgres`, `duckdb` and
   `ragtime` to that tag and commit.
3. Tag the four packages `<pkg>-vX.Y.Z` on that commit and push.

**The `v0.0.0` let-go require** in `sql/shim/go.mod`,
`wails/shim/go.mod` and `duckdb/shim/go.mod` is deliberate. A shim has no let-go version of its
own: Go's minimal version selection resolves the placeholder to whatever
the consumer's `:lg-version` pins, so the project's pin stays
authoritative. A real version here would set a floor and silently bump an
older pin. The cost is that `shim/` does not build on its own — `go vet`
inside it needs a `replace` or a `go.work` — it compiles through the
runtime module lgx generates. Needs let-go 1.13.0 or newer, the first
release carrying the interop work; consumers pin it with
`:lg-runtime :built`:

```clojure
{:lg-runtime :built
 :lg-version "1.13.0"}
```

**Never move or delete a pushed tag.** proxy.golang.org and sum.golang.org
record a Go module tag permanently, and lgx caches a `:git/tag` checkout
under the tag name. Fix forward with a new version.

**Working on a shim.** Flip `<pkg>/lgx.edn` back to `{:go/local "shim"}`
locally. lgx then rebuilds the runtime on every command — incremental,
about a second — so edits to `shim.go` take effect, and each `example/`
picks them up through its `:local/root ".."` dep. Do not commit the
flip-back; release per the steps above instead. `LGX_LETGO_REPLACE` is
the separate lever for developing against an uncommitted let-go change.
