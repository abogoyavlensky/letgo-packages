# postgres

PostgreSQL for let-go: a thin driver package over the shared
[`sql`](../sql) layer, using Go's `database/sql` and the pure-Go
[`github.com/jackc/pgx/v5`](https://pkg.go.dev/github.com/jackc/pgx/v5)
driver through its `stdlib` adapter.

```clojure
(require '[postgres.core :as db])

(let [conn (db/open "postgres://user:pass@localhost:5432/mydb")]
  (db/query conn ["select id, name from people where age > $1" 30])
  ;; => [{:id 1 :name "Ada"}]
  (db/close! conn))
```

The API is re-exported from [`sql.core`](../sql/README.md): `execute!`,
`execute-one!`, `query`, `with-transaction`. Two Postgres specifics:

- Parameter placeholders are `$1`, `$2`, ... (not `?`).
- `:keys` defaults to `:unqualified-lower`, because Postgres folds
  unquoted identifiers to lower case.

## Value types

Verified round-trips: integer, float, text, boolean (as real `true` /
`false`, unlike SQLite's `1`/`0`), and NULL as `nil`. Beyond those:

- `timestamptz` arrives as a boxed Go `time.Time`. It prints opaquely,
  but methods work: `(.Unix t)`, `(.String t)`. Compare through an epoch
  (`extract(epoch from ...)`) when you need a plain number.
- `numeric` arrives as a string (`"12.34"`), preserving exact decimal
  text rather than rounding through a float.
- `text[]` (and other arrays) arrive as the array's Postgres text form,
  e.g. `"{a,b}"`, not as a vector — `database/sql` has no portable array
  scanning. Parse it yourself or `unnest` in SQL.

See [`example/`](example/) for a full walkthrough and the
[sqlite README](../sqlite/README.md) for requirements and the
development workflow, which are the same here.
