# sqlite

SQLite for let-go, through Go's `database/sql` and the pure-Go
[`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) driver.
Pure Go means no cgo, so there is no C compiler to install and
cross-compiled builds stay clean.

## Requirements

- [lgx](https://github.com/abogoyavlensky/lgx) 0.2 or newer.
- The Go toolchain on `PATH` (`mise use -g go@latest`, or
  <https://go.dev/dl>). lgx builds a custom `lg` that links the driver;
  that build happens once and is cached under `~/.lgx/runtimes/`.
- A `:lg-version` pin in your `lgx.edn` — it is the let-go the custom
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
  (db/execute! conn ["insert into people (name, age) values (?, ?)" "Ada" 36])
  ;; => {:rows-affected 1}

  (db/query conn ["select id, name from people where age > ?" 30])
  ;; => [{:id 1 :name "Ada"}]

  (db/close! conn))
```

Statements are `[sql-string & params]` vectors — the shape
[HoneySQL](https://github.com/seancorfield/honeysql)'s `format` returns,
so `(db/query conn (honey/format q))` works directly. `query` returns a
fully realized vector of column-keyword → value maps; parameters and
results are ordinary let-go values, and SQL `NULL` is `nil`.

## API

| | |
|---|---|
| `(open path)` | Open a database. `":memory:"` for an in-memory one. |
| `(close! db)` | Close it. Returns the error as a value, `nil` on success. |
| `(query db [sql & params])` | Rows as a vector of keyword-keyed maps. |
| `(execute! db [sql & params])` | Run for effect → `{:rows-affected n}`. |

The raw generated `sql` namespace is available too — `sql/Open`, and
method dispatch on the boxed handles (`(.Ping db)`, `(.Begin db)`) — for
anything the veneer does not cover.

## Layout

```
sqlite/
├── lgx.edn        the three Go deps: database/sql, the driver, the shim
├── src/sqlite/
│   └── core.lg    the veneer — open / close! / query / execute!
├── shim/          a small Go module: the seams generation cannot cross
└── example/       a runnable app used to verify the whole stack
```

`shim/` exists because two things do not survive automatic binding
generation:

- **`Scan`'s out-parameters.** `(*sql.Rows).Scan` writes through
  caller-allocated pointers, and let-go values are immutable. `ScanRow`
  allocates the destinations, calls `Scan`, and returns the row as a
  value.
- **Variadic method application.** Methods are not first-class values in
  let-go, so a wrapper holding its parameters in a vector cannot spread
  them into `(.Query db q p1 p2)`. `Query`/`Exec` take a slice and
  spread it in Go.

Both also convert values at the boundary, which is load-bearing: let-go
walks a Go slice by its *static* element type, so a `[]any` arrives as
opaque boxed values that compare equal to nothing, and a parameter
vector containing `nil` fails to convert at all.

## Running the example

```
cd example
lgx run
```

The first run builds the custom runtime (a minute or so, once).

## Development

While the wrapper is unreleased, `lgx.edn` points at the shim with
`{:go/local "shim"}` and `shim/go.mod` requires let-go at a placeholder
`v0.0.0`, which the root module's `replace` governs. Before tagging,
both have to become real: the shim's require pinned to a released let-go,
and the coord switched to `{:go/version "vX.Y.Z"}`.

To build against a let-go working tree rather than a release:

```
LGX_LETGO_REPLACE=/path/to/let-go lgx run
```
