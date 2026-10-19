# sql

The driver-agnostic SQL layer for let-go: a
[next.jdbc](https://github.com/seancorfield/next-jdbc)-shaped API over
Go's `database/sql`. This package carries no driver - depend on
[`sqlite/`](../sqlite) or [`postgres/`](../postgres) instead, which pull
this layer in transitively and re-export its API. Depend on `sql`
directly only to build a new driver package.

## The API

```clojure
(sql/execute!     connectable [sql & params])        ; => [{...} ...] or [{:sql/update-count n}]
(sql/execute!     connectable [sql & params] opts)
(sql/execute-one! connectable [sql & params])        ; => {...}, nil, or {:sql/update-count n}
(sql/execute-one! connectable [sql & params] opts)
(sql/query        connectable [sql & params])        ; sugar for execute!
(sql/with-transaction [tx connectable] body...)      ; macro
```

Statements are `[sql-string & params]` vectors - the shape HoneySQL's
`format` returns. Parameter placeholder syntax belongs to the driver:
`?` for SQLite, `$1` for Postgres.

A *connectable* is the map `open` returns:

```clojure
{:sql/handle <boxed *sql.DB or *sql.Tx>
 :sql/opts   {:keys :unqualified}}
```

A bare boxed handle (e.g. straight from `sql/Open`) is accepted too and
treated as `{:sql/handle h :sql/opts {}}`. Opts merge per call: the
connectable's `:sql/opts` under, the call's opts over.

### Driver hook: `:sql/scan-row`

A driver package can add one more key to the connectable its `open`
returns:

```clojure
{:sql/handle   h
 :sql/opts     {...}
 :sql/scan-row f}   ; (f rows) -> the current row's values, in column order
```

`f` receives the boxed `*sql.Rows` already positioned on a row (`.Next`
has returned true) and returns anything `zipmap` accepts. Without the
key, `sql.shim/ScanRow` reads the row and the boxing layer converts
each value. `with-transaction` copies the key onto the `tx` connectable,
so reads inside a transaction convert the same way. The `duckdb`
package uses it to turn DuckDB's Go types (`*big.Int`, `time.Time`,
`duckdb.Decimal`, ...) into plain let-go values; `sqlite` and `postgres`
do not set it.

### Rows or an update count?

`execute!` runs anything, but `database/sql` splits execution into
`Query` (rows, no update count) and `Exec` (update count, no rows), and
the choice must be made before running the statement. `returns-rows?`
decides heuristically:

1. First keyword (past whitespace and SQL comments) in `SELECT`, `WITH`,
   `VALUES`, `TABLE`, `SHOW`, `EXPLAIN` - rows.
2. Otherwise, a `RETURNING` clause outside any string literal, quoted
   identifier, or comment - rows.
3. Otherwise - update count.

Known false positive: a `WITH ... INSERT` CTE matches rule 1 and takes
the rows path. The scanner recognizes standard `'...'` literals, `"..."`
identifiers, and `--`/`/* */` comments, but not driver-specific quoting -
a Postgres dollar-quoted string (`$$returning$$`) or a SQLite
bracket/backtick identifier containing the word `returning` fools rule 2.
`{:returns :rows}` or `{:returns :update-count}` in opts overrides the
heuristic outright; use it for anything the scan gets wrong.

### Result keys

Row keys are unqualified keywords by default (`:name`, not
`:people/name`). Automatic table qualification is a constraint of the
platform, not a style choice: Go's `sql.ColumnType` exposes the column
name and nothing about its table, so this layer cannot build
`:people/name` for any driver. (A custom `:keys` function, or a column
aliased to a name containing `/`, can still produce qualified keywords.)
The `:keys` option controls the transform from column name to keyword:

- `:unqualified` (default) - verbatim
- `:unqualified-lower` - lower-cased first (the postgres package's
  default, matching Postgres's case folding)
- any `String -> keyword` function - the escape hatch

### Transactions

```clojure
(sql/with-transaction [tx conn]
  (sql/execute! tx ["insert into ..."])
  (sql/execute! tx ["update ..."]))
```

Begins on the connection, binds `tx` as a connectable carrying the
parent's opts, commits on normal return (returning the body's value),
rolls back and rethrows on a throw. Two deliberate behaviors:

- A failed commit throws. Go returns `Commit`'s error as a value; a
  layer that dropped it would let callers believe the write landed.
- A failed rollback is reported on stderr but never thrown, so it
  cannot mask the body's exception - the original failure is the
  useful one.

Nesting is unsupported and throws: `database/sql` has no savepoint API,
so a nested transaction would silently be a second independent one.

### Sessions and cancellation

`sql.core` runs on whatever handle it is given. For work that needs a
dedicated connection or a way to stop a running query - a database
client, a long report - the shim exposes `database/sql`'s context
methods:

```clojure
(require '[sql.shim :as shim])

(let [session (shim/Conn (:sql/handle conn))  ; a dedicated *sql.Conn
      ctx (shim/NewCtx)]                      ; a cancellable context
  ;; from another thread, at any time: (.Cancel ctx)
  (let [rows (shim/QueryCtx session ctx "select ..." [])]
    (shim/ColumnTypeNames rows)               ; => ["INTEGER" "TEXT"]
    ...)
  (.Cancel ctx)                               ; release it when done
  (.Close session))                           ; back to the pool
```

| | |
|---|---|
| `(shim/NewCtx)` | A cancellable context. `(.Cancel ctx)` is idempotent; `(.Canceled ctx)` reports whether it was called. Create it before the query starts so a cancel can arrive before the first row. |
| `(shim/QueryCtx c ctx q params)` `(shim/ExecCtx c ctx q params)` | `Query`/`Exec` under `ctx`. `c` is a `*sql.DB`, `*sql.Tx` or `*sql.Conn`. |
| `(shim/Conn db)` | Take a session from the pool. Session state (`BEGIN`, temp tables, `SET`) stays on it across calls. Close it to return it. |
| `(shim/ColumnTypeNames rows)` | Each column's database type name, in column order; `""` where the driver does not know it. |

Cancelling while the query executes interrupts it in the driver (sqlite
calls `sqlite3_interrupt`; pgx sends a protocol cancel request), and the
call fails promptly, usually with `context canceled`. Cancelling after
rows came back makes `database/sql` close them: `.Next` returns false and
`.Err` reports the context error. Check `(.Canceled ctx)` rather than
the message to tell a cancel from a failure.

`execute!` and `execute-one!` accept a session as the handle
(`{:sql/handle session}`). `with-transaction` does not: it calls
`.Begin`, which `*sql.Conn` lacks. Run `BEGIN` / `COMMIT` as statements
on the session instead.

## Layout

```
sql/
├── lgx.edn        :go/interop for database/sql + the shim (:go/local)
├── src/sql/
│   └── core.lg    the API above
├── test/          direct tests for the returns-rows? heuristic
└── shim/          a small Go module: the seams generation cannot cross
```

The shim exists because three things do not survive automatic binding
generation (and one that has no let-go form at all):

- **`Scan`'s out-parameters.** `(*sql.Rows).Scan` writes through
  caller-allocated pointers, and let-go values are immutable. `ScanRow`
  allocates the destinations, calls `Scan`, and returns the row as a
  value.
- **Variadic method application.** Methods are not first-class values in
  let-go, so a wrapper holding its parameters in a vector cannot spread
  them into `(.Query db q p1 p2)`. `Query`/`Exec` take a slice and
  spread it in Go.
- **A common type for `*sql.DB`, `*sql.Tx` and `*sql.Conn`.**
  `database/sql` declares none, so the shim's `Connectable` interface
  (the `...Context` methods all three share) is what lets `execute!`
  work identically on a pool, in a transaction, and on a session.
- **A `context.Context`.** let-go cannot build one, so `Ctx` wraps a
  cancellable context and `QueryCtx`/`ExecCtx`/`Conn` take it, or
  `context.Background()`, on the caller's behalf.

Values cross the boundary as plain `[]any` in both directions; let-go's
boxing layer converts elements by their dynamic type, so scanned columns
arrive as native strings, ints, floats, and `nil` for NULL. This needs
let-go's `[]any` boxing fix, released in let-go 1.13.0 - pin
`:lg-version "1.13.0"` or newer (see "Releasing" in the
[repo README](../README.md)).
