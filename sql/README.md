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
the rows path. `{:returns :rows}` or `{:returns :update-count}` in opts
overrides the heuristic outright; use it for anything the keyword match
gets wrong.

### Result keys

Row keys are unqualified keywords (`:name`, never `:people/name`). This
is a constraint of the platform, not a style choice: Go's
`sql.ColumnType` exposes the column name and nothing about its table,
so table-qualified keys cannot be built for any driver. The `:keys`
option controls the transform from column name to keyword:

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
generation:

- **`Scan`'s out-parameters.** `(*sql.Rows).Scan` writes through
  caller-allocated pointers, and let-go values are immutable. `ScanRow`
  allocates the destinations, calls `Scan`, and returns the row as a
  value.
- **Variadic method application.** Methods are not first-class values in
  let-go, so a wrapper holding its parameters in a vector cannot spread
  them into `(.Query db q p1 p2)`. `Query`/`Exec` take a slice and
  spread it in Go.
- **A common type for `*sql.DB` and `*sql.Tx`.** `database/sql` declares
  none, so the shim's `Connectable` interface is what lets `execute!`
  work identically inside and outside a transaction.

Values cross the boundary as plain `[]any` in both directions; let-go's
boxing layer converts elements by their dynamic type, so scanned columns
arrive as native strings, ints, floats, and `nil` for NULL. This needs
the `[]any` boxing fix from let-go's `integration/go-interop` branch
(unreleased as of this writing) - see the tagging notes in the
[repo README](../README.md).
