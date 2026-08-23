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

See [`example/`](example/) for a full walkthrough and the
[sqlite README](../sqlite/README.md) for requirements and the
development workflow, which are the same here.
