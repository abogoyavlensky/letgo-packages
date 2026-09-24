# duckdb

[DuckDB](https://duckdb.org) for let-go: an in-process analytical database,
as a thin driver package over the shared [`sql`](../sql) layer. It uses
Go's `database/sql` and the official
[`github.com/duckdb/duckdb-go/v2`](https://pkg.go.dev/github.com/duckdb/duckdb-go/v2)
driver, plus a small shim that returns DuckDB values as plain let-go
values (see [Value types](#value-types)).

## Requirements

**This is the one cgo driver in this repo.** There is no pure-Go DuckDB:
the driver links a prebuilt static `libduckdb`. That costs:

- **A C toolchain** on every machine that builds: gcc or clang on Linux,
  the Xcode command line tools on macOS.
- **Native builds only.** `lgx build --target`/`--all` builds with
  `CGO_ENABLED=0` and fails at build time (`build constraints exclude all
  Go files in .../duckdb-go-bindings/lib/<platform>`). Build each platform
  on that platform, e.g. in a per-OS CI matrix or a Linux Docker stage.
  Prebuilt libraries exist for linux and darwin on amd64/arm64.
- **A bigger binary.** About 90 MB against 16 MB for a stock `lg`. It
  links `libstdc++` and glibc dynamically, so deploy it on a glibc base
  image (debian-slim), not alpine or scratch.
- **A slower first build.** The custom runtime takes about 40 s to build
  the first time; later runs reuse the cache under `~/.lgx/runtimes/`.

Otherwise the same as the other drivers:

- [lgx](https://github.com/abogoyavlensky/lgx) 0.3.1 or newer, and the Go
  toolchain on `PATH`.
- `:lg-runtime :built` and a `:lg-version` pin (1.13.0 or newer) in your
  `lgx.edn`.

## Use

```clojure
;; lgx.edn
{:paths ["src"]
 :main "main.lg"
 :lg-runtime :built
 :lg-version "1.13.0"
 :deps {abogoyavlensky/letgo-duckdb {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                     :git/tag "duckdb-v0.1.0"
                                     :deps/root "duckdb"}}}
```

```clojure
(require '[duckdb.core :as db])

(let [conn (db/open "analytics.duckdb")]            ; or "" for in-memory
  (db/execute! conn ["create table events (ts timestamp, path varchar)"])
  (db/execute! conn ["insert into events values (?::timestamp, ?), (?::timestamp, ?)"
                     "2026-09-01 10:00:00" "/" "2026-09-01 11:00:00" "/docs"])

  (db/query conn ["select ts::date as day, count(*) as views
                   from events group by day"])
  ;; => [{:day "2026-09-01" :views 2}]

  ;; any DuckDB SQL, e.g. querying files in place
  (db/query conn ["select path, count(*) as n from read_csv('hits.csv') group by path"])
  (db/close! conn))
```

The API is re-exported from [`sql.core`](../sql/README.md): `execute!`,
`execute-one!`, `query`, `with-transaction`. Parameters use `?`.

A database file is locked by the process that opens it: open it once per
process and share the connectable, which is safe from concurrent
threads (it wraps a `database/sql` pool).

## Value types

`duckdb.shim/ScanRow` converts every value before it reaches let-go.
Dates and times become ISO-8601 strings shaped by the column's type, and
integers too large for an int64 become decimal strings rather than lossy
floats.

| DuckDB | let-go |
|---|---|
| BOOLEAN, VARCHAR, ENUM, integers that fit int64, FLOAT/DOUBLE | boolean, string, int, float |
| NULL | `nil` |
| HUGEINT, UHUGEINT, UBIGINT; **`sum()` over integers** (DuckDB returns HUGEINT) | int when it fits an int64, else a decimal string |
| DECIMAL | exact string: `"12.34"` |
| DATE | `"2026-09-23"` |
| TIME | `"10:11:12"` (fraction trimmed: `"10:11:12.5"`) |
| TIMETZ | `"10:11:12+02:00"` |
| TIMESTAMP, TIMESTAMP_S/_MS/_NS | `"2026-09-23T10:11:12"`, no zone |
| TIMESTAMPTZ | UTC, RFC 3339: `"2026-09-23T08:11:12Z"` |
| UUID | `"4b5c0e53-7a3f-4a8e-9a55-1f1d0c7f4e11"` |
| INTERVAL | `{:months 0 :days 0 :micros 5400000000}` |
| LIST, ARRAY | vector |
| STRUCT, JSON object | map with keyword keys: `{:a 1}` |
| MAP | map with converted keys: `{"k1" 1}` |
| JSON | the decoded value; numbers are floats (`{:a 1.0}`) |

Values inside a LIST, STRUCT or MAP are converted recursively, but carry
no column type, so two rules differ there:

- A nested date or time is a UTC RFC 3339 string
  (`["2026-09-23T10:11:12Z"]`), whatever its DuckDB type.
- A nested UUID arrives as 16 raw bytes. Cast it in SQL:
  `list_transform(ids, x -> x::varchar)`.

Left as the boxing layer produces them: BLOB (a string of the raw bytes),
BIT and UNION (opaque Go values). Cast in SQL when you need them.

Parameters go the other way unconverted: pass dates and timestamps as
strings and cast (`?::timestamp`).

## Ingest

Insert many rows per statement. Measured on 2000 rows: one autocommit
`insert` per row took about 1.5 s, one transaction around the same
inserts about 1.2 s, and 200-row `VALUES` batches 0.1 s. For a web app,
buffer incoming events and flush a multi-row insert every N events or
T seconds.

## Known limits

- No cross-compilation (above).
- Nested UUIDs and dates follow the nested rules above.
- No Appender API (DuckDB's bulk-load path). It needs a raw driver
  connection through a Go callback, which the sql layer does not
  expose; batched `VALUES` covers lightweight use.

## Layout

```
github.com/duckdb/duckdb-go/v2   registers itself as the "duckdb" driver (cgo, link-only)
database/sql                     generated bindings (from the sql package's coords)
sql.core                         the API - execute!, execute-one!, query, with-transaction
duckdb.shim                      this package's Go seam - ScanRow (./shim)
duckdb.core                      this package - open, close!, re-exports
```

`open` puts `duckdb.shim/ScanRow` on the connectable as `:sql/scan-row`,
the sql layer's hook for a driver-specific row reader; `with-transaction`
carries it into the transaction.

## Running the example and tests

```
cd example && lgx run      # end-to-end walkthrough with assertions
lgx test                   # the value table, from duckdb/
```

## Development

The shim ships as the tagged Go module `duckdb/shim/vX.Y.Z`, which
`lgx.edn` pins with `:go/version`. To edit it, flip that coord to
`{:go/local "shim"}` locally - see "Releasing" in the root README. Keep
the driver's `:go/version` in `lgx.edn` equal to the require in
`shim/go.mod`.
