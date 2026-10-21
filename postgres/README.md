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

- `bytea` arrives as a string that has lost its bytes: the `sql` shim
  turns every `[]byte` into a string, which suits text but not binary
  data. Encode in SQL (`encode(col, 'hex')`) when you need the value.

## Cancellation

A query run through `sql.shim/QueryCtx` or `ExecCtx` stops when its
`Ctx` is cancelled (see the [sql README](../sql/README.md#sessions-and-cancellation)).
What happens to the connection afterwards is chosen at `open`:

```clojure
(db/open url {:postgres/cancel :request})
```

| `:postgres/cancel` | The server | The connection |
|---|---|---|
| `:close` (default) | is sent a cancel request | is closed: a dedicated session (`sql.shim/Conn`) answers `driver: bad connection` from then on |
| `:request` | is sent a cancel request | stays usable, session state and all, as after Ctrl+C in `psql`; closed only if the server has not answered within 10 s |

`:close` is pgx's own behaviour and stays the default. Use `:request`
for an interactive client that holds sessions, where a cancel must not
drop the user's temp tables, `SET`s or open transaction. pgx picks the
mode through a Go function in its config, which no DSN can express, so
the package's shim (`postgres.shim/Open`) builds that config.

## Connect timeout

Unless the DSN, `PGCONNECT_TIMEOUT` or a service file sets
`connect_timeout`, connecting
is bounded by 10 s. pgx's own default is none, which leaves a ping or
first query to an unreachable host hanging for minutes. An explicit
`connect_timeout=0` still means no timeout.

## Tests

`lgx test` runs the driver tests when `POSTGRES_TEST_URL` points at a
server, and skips them otherwise:

```sh
docker run --rm -d --name lgp-pg -e POSTGRES_PASSWORD=pg -p 127.0.0.1::5432 postgres:17-alpine
POSTGRES_TEST_URL="postgres://postgres:pg@$(docker port lgp-pg 5432)/postgres" lgx test
```

The connect-timeout tests need `python3`.

See [`example/`](example/) for a full walkthrough and the
[sqlite README](../sqlite/README.md) for requirements and the
development workflow, which are the same here.
