# sqlite

SQLite for let-go: a thin driver package over the shared
[`sql`](../sql) layer, using Go's `database/sql` and the pure-Go
[`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) driver.
Pure Go means no cgo, so there is no C compiler to install and
cross-compiled builds stay clean.

## Requirements

- [lgx](https://github.com/abogoyavlensky/lgx) 0.2 or newer.
- The Go toolchain on `PATH` (`mise use -g go@latest`, or
  <https://go.dev/dl>). lgx builds a custom `lg` that links the driver;
  that build happens once and is cached under `~/.lgx/runtimes/`.
- A `:lg-version` pin in your `lgx.edn` - it is the let-go the custom
  runtime is built from.

## Use

```clojure
;; lgx.edn
{:paths ["src"]
 :main "main.lg"
 :lg-version "1.11.1"
 :deps {abogoyavlensky/letgo-sqlite {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                     :git/tag "sqlite-v0.1.0"
                                     :deps/root "sqlite"}}}
```

```clojure
(require '[sqlite.core :as db])

(let [conn (db/open "app.db")]                      ; or ":memory:"
  (db/execute! conn ["create table people (id integer primary key,
                                           name text not null,
                                           age integer)"])
  ;; => [{:sql/update-count 0}]

  (db/execute! conn ["insert into people (name, age) values (?, ?)" "Ada" 36])
  ;; => [{:sql/update-count 1}]

  (db/query conn ["select id, name from people where age > ?" 30])
  ;; => [{:id 1 :name "Ada"}]

  (db/execute-one! conn ["select name from people where id = ?" 1])
  ;; => {:name "Ada"}

  (db/with-transaction [tx conn]
    (db/execute! tx ["insert into people (name, age) values (?, ?)" "Grace" 45])
    (db/execute! tx ["update people set age = ? where name = ?" 37 "Ada"]))

  (db/close! conn))
```

Statements are `[sql-string & params]` vectors - the shape
[HoneySQL](https://github.com/seancorfield/honeysql)'s `format` returns,
so `(db/query conn (honey/format q))` works directly. Parameters and
results are ordinary let-go values, and SQL `NULL` is `nil`.

## API

The API is [next.jdbc](https://github.com/seancorfield/next-jdbc)-shaped
and comes from [`sql.core`](../sql); `sqlite.core` re-exports it, so one
require covers everything. See the [sql README](../sql/README.md) for
the full reference, including how `execute!` decides between rows and an
update count, and the `:returns` override.

| | |
|---|---|
| `(open path)`, `(open path opts)` | Open a database; `":memory:"` for an in-memory one. Returns a connectable. |
| `(close! conn)` | Close it. Returns the error as a value, `nil` on success. |
| `(execute! conn stmt)`, `(execute! conn stmt opts)` | Rows as a vector of maps, or `[{:sql/update-count n}]` for DML. |
| `(execute-one! conn stmt)`, `...opts)` | The first row, `nil` for an empty result, `{:sql/update-count n}` for DML. |
| `(query conn stmt)`, `(query conn stmt opts)` | Sugar for `execute!`. |
| `(with-transaction [tx conn] body...)` | Begin, run, commit; roll back and rethrow on a throw. Does not nest. |

Opts passed to `open` become the connection's defaults; opts passed to a
call merge over them.

### Result keys

Row keys are unqualified keywords (`:name`, not `:people/name`). This is
a constraint, not a choice: Go's `sql.ColumnType` carries no table name,
so table-qualified keys are unobtainable through `database/sql` for any
driver. The `:keys` option controls the transform:

- `:unqualified` (default) - column name to keyword, verbatim
- `:unqualified-lower` - lower-cased first
- any `String -> keyword` function

SQLite notes: the database has no boolean storage class, so an inserted
`true` reads back as `1`. TEXT columns return as native strings.

## Layout

```
sqlite/
├── lgx.edn        deps: the sql package (:local/root) + modernc.org/sqlite
├── src/sqlite/
│   └── core.lg    open / close! + re-exports of the sql.core API
└── example/       a runnable app used to verify the whole stack
```

The layers, from the bottom up:

```
modernc.org/sqlite   registers itself as the "sqlite" driver (link-only)
database/sql         generated bindings (from the sql package's coords)
sql.shim             the Go seams generation cannot cross (../sql/shim)
sql.core             the API - execute!, execute-one!, query, with-transaction
sqlite.core          this package - open, close!, re-exports
```

The `database/sql` bindings and the shim arrive transitively from the
`sql` package's `lgx.edn`; this package declares only the driver.

## Running the example

```
cd example
lgx run
```

The first run builds the custom runtime (a minute or so, once).

## Development

While the packages are unreleased, `sql/lgx.edn` points at the shim with
`{:go/local "shim"}` and `sql/shim/go.mod` requires let-go at a
placeholder `v0.0.0`, which the runtime module's `replace` governs.
Before tagging, both have to become real: the shim's require pinned to a
released let-go, and the coord switched to `{:go/version "vX.Y.Z"}`.
The pinned release must also carry let-go's `[]any` boxing fix (in
`integration/go-interop`, unreleased as of this writing) - without it,
scanned values arrive as opaque boxes.

To build against a let-go working tree rather than a release:

```
LGX_LETGO_REPLACE=/path/to/let-go lgx run
```
