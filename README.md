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

## Releasing

Two kinds of tags live in this repo:

| Tag | Form | Read by |
|---|---|---|
| Go module tag | `<pkg>/shim/vX.Y.Z` (e.g. `sql/shim/v0.1.0`) | `go get`, through the module proxy. Go dictates the form: a module whose `go.mod` sits in a subdirectory is versioned by a tag prefixed with that path. |
| Package tag | `<pkg>-vX.Y.Z` (e.g. `sqlite-v0.1.0`) | lgx, via `:git/tag`. Go ignores tags that are not semver. |

Only `sql` and `wails` have a shim. `sqlite` and `postgres` have none —
they inherit `sql`'s through their `:local/root "../sql"` dep, so a shim
change in `sql` is a package bump for them too.

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

**The `v0.0.0` let-go require** in `sql/shim/go.mod` and
`wails/shim/go.mod` is deliberate. A shim has no let-go version of its
own: Go's minimal version selection resolves the placeholder to whatever
the consumer's `:lg-version` pins, so the project's pin stays
authoritative. A real version here would set a floor and silently bump an
older pin. The cost is that `shim/` does not build on its own — `go vet`
inside it needs a `replace` or a `go.work` — it compiles through the
runtime module lgx generates. Known to work from let-go
`f26eb497299760e93ce430302f13ab3a954eab64`, the first commit carrying the
merged interop work; consumers pin that or newer with `:lg-runtime :built`:

```clojure
{:lg-runtime :built
 :lg-version "f26eb497299760e93ce430302f13ab3a954eab64"}
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
