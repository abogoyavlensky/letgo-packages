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

| Package | What it wraps |
|---|---|
| [`sqlite/`](sqlite/) | SQLite, via `database/sql` and the pure-Go `modernc.org/sqlite` driver |
