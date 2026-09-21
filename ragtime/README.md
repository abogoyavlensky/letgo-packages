# ragtime

Schema migrations for let-go with [ragtime](https://github.com/weavejester/ragtime)'s
database-independent core, over the shared [`sql`](../sql) layer. The
let-go counterpart of `ragtime.next-jdbc`: a `DataStore` that keeps the
applied migration ids in a table, a `Migration` that runs SQL up and down,
and ragtime's `migrate`/`rollback` re-exported so an app needs one require.
It works with any driver package - [`sqlite`](../sqlite) or
[`postgres`](../postgres) - because everything goes through `sql.core`.

```clojure
;; lgx.edn
{:paths ["src"]
 :main "main.lg"
 :lg-runtime :built
 :lg-version "1.13.0"
 :deps {abogoyavlensky/letgo-sqlite {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                     :git/tag "sqlite-v0.1.0"
                                     :deps/root "sqlite"}
        abogoyavlensky/letgo-ragtime {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                      :git/tag "ragtime-v0.1.0"
                                      :deps/root "ragtime"}}}
```

```clojure
(require '[ragtime.letgo :as rt]
         '[sqlite.core :as sqlite])

(def migrations
  [(rt/sql-migration {:id "001-create-people"
                      :up ["create table people (id integer primary key, name text)"]
                      :down ["drop table people"]})
   (rt/sql-migration {:id "002-index-people-name"
                      :up ["create index people_name_idx on people (name)"]
                      :down ["drop index people_name_idx"]})])

(let [conn (sqlite/open "app.db")
      config {:datastore (rt/sql-database conn)
              :migrations migrations}]
  (rt/migrate config)      ; Applying 001-create-people ...
  (rt/rollback config)     ; Rolling back 002-index-people-name
  (rt/rollback config 2))  ; the last two
```

The API:

- `(sql-database conn)` / `(sql-database conn {:migrations-table "..."})` -
  a `DataStore` over any `sql.core` connectable. Creates the bookkeeping
  table (default `ragtime_migrations`) when it is missing.
- `(sql-migration {:id "..." :up [stmt ...] :down [stmt ...]})` - a
  `Migration`. Each `stmt` is what `sql.core/execute!` takes: a SQL string,
  or `[sql & params]`. Format your DDL however you like - HoneySQL maps
  through `(sql/format ...)`, plain strings, `.sql` files you read yourself -
  the package only sees statements. Each direction runs in one
  `with-transaction`, so a failing statement leaves the schema as it was.
- `migrate` and `rollback` are `ragtime.repl`'s, unchanged: the config map
  takes ragtime's `:strategy` and `:reporter` too.

## How it stores state

One table, `(seq integer primary key, id text not null unique)`, read in
`seq` order. Two things about its SQL are deliberate, so that the same
statements run on sqlite and postgres:

- **No parameters.** Placeholders differ per driver (`?` on sqlite, `$1` on
  postgres) and a connectable does not say which driver it is, so the
  migration id goes into the statement as a quoted literal with `'`
  doubled. Ids are file-name-shaped strings; the escape covers the one
  character that matters.
- **No autoincrement, no clock.** sqlite's `integer primary key` auto-fills
  and postgres wants `serial`; `ragtime.next-jdbc`'s `created_at` wants a
  timestamp. The insert fills `seq` itself with
  `select coalesce(max(seq), 0) + 1`.

## Strategies

ragtime's default is `raise-error`: an applied id that is not in your
history is a conflict, and `migrate` throws. `ragtime.strategy/apply-new`
skips the check and `rebase` rolls back to the divergence first; pass one
as `:strategy` in the config.

## Layout

```
ragtime/
├── lgx.edn                deps: sql (:local/root) and ragtime core (git,
│                          :deps/root "core/src", the JDBC modules excluded)
├── src/ragtime/
│   └── letgo.lg           sql-database, sql-migration, migrate, rollback
├── test/ragtime/          the store, transactions, strategies, quoting
└── example/               a two-step history up, back one, and up again
```

## Development

```
cd ragtime && lgx test       # over sqlite in-memory; the :test context brings the driver
cd ragtime/example && lgx run
```

The package pins `:lg-runtime :built` and `:lg-version` for its own tests;
a consumer's runtime decision is its own. No Go code of its own, so a
release is the package tag alone (`ragtime-vX.Y.Z`) - see "Releasing" in
the [repo README](../README.md).
