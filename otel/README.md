# otel

Traces and logs for let-go over OTLP/HTTP, through the Go
[OpenTelemetry](https://opentelemetry.io) SDK. A request becomes a span, a
warning becomes a log record tied to that span, and both go to any OTLP
receiver (an OpenTelemetry Collector, Tracelet, Jaeger, a vendor).

**Status: works.** Checked against a fake receiver (`lgx test`) and against a
real Tracelet (the example): spans arrive with their
parent/child structure, logs carry the trace and span ids, and the resource
attributes from the environment are filed under the right service. Built on
otel and its SDK v1.47.0. The log exporter (`otlploghttp` v0.23.0) is still
pre-1.0, so its API may move; the shim pins it exactly. Protobuf only, no
OTLP/JSON, no metrics.

## Requirements

- [lgx](https://github.com/abogoyavlensky/lgx) 0.4.2 or newer, let-go 1.13.0
  or newer.
- The Go toolchain on `PATH`: lgx builds a custom `lg` that links the SDK.
  The first build downloads the module graph; after that the runtime is
  cached.
- No C toolchain: everything here is pure Go (`CGO_ENABLED=0` works).

## Use

```clojure
;; lgx.edn
{:lg-runtime :built
 :lg-version "1.13.0"
 :deps {abogoyavlensky/letgo-otel {:git/url "https://github.com/abogoyavlensky/letgo-packages"
                                   :git/tag "otel-v0.1.0"
                                   :deps/root "otel"}}}
```

```clojure
(ns main
  (:require [otel.core :as otel]))

(let [h (otel/start! {:service-name "my-app"})]
  (otel/in-span h "GET /things/:id" {:kind :server :attrs {"http.route" "/things/:id"}}
    (fn [root]
      (otel/in-span h "db.lookup" {:parent root :kind :client}
        (fn [_] :found))
      (otel/log! h :warn "slow lookup" {"ms" 5} {:span root})))
  (otel/shutdown! h))
```

Run it with the destination in the environment:

```
OTEL_EXPORTER_OTLP_ENDPOINT=https://tracelet.example.com \
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer tl_..." \
OTEL_RESOURCE_ATTRIBUTES="deployment.environment.name=staging,service.version=abc1234" \
./app
```

`example/` is a small program that sends one trace and two log lines to
whatever those variables point at.

## API

Handles are opaque. `attrs` is a map with string or keyword keys and string,
integer, float or boolean values; anything else is dropped, never thrown on.

| Function | Behaviour |
|---|---|
| `(start! opts)` | Build the tracer and logger providers; returns a handle. `opts`: `:service-name`, `:endpoint`, `:headers`, `:resource`. A no-op handle when no endpoint is configured. Throws only on a malformed endpoint. |
| `(enabled? h)` | True when the handle exports; false for the no-op handle. |
| `(shutdown! h)` | Flush and stop both providers, waiting at most 5 s. A failing flush is printed to stderr, not thrown. Safe on a no-op handle and when called twice. |
| `(start-span h name opts)` | `opts`: `:parent` (a span or nil), `:kind` (`:server`, `:client`, `:internal`; default `:internal`), `:attrs`. |
| `(end-span! span)` | Ends it. Twice is harmless. |
| `(set-attrs! span attrs)`, `(set-error! span message)` | `set-error!` sets status Error with the message. |
| `(in-span h name opts f)` | Starts a span (child of `:parent`), calls `(f span)`, ends it in a `finally`; an exception sets the error status and is rethrown. Returns `f`'s value. |
| `(log! h level body attrs opts)` | `level` is `:debug`, `:info`, `:warn` or `:error`. `opts`: `:span` ties the record to that span's trace and span ids. |
| `(trace-id span)`, `(span-id span)` | Lowercase hex (32 and 16 characters). |

## Configuration

The SDK's own environment variables configure it, so the same lines work for
any SDK: `OTEL_EXPORTER_OTLP_ENDPOINT` (and the `_TRACES_`/`_LOGS_`
variants), `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_COMPRESSION`,
`OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_TRACES_SAMPLER`.

**A standard variable always wins over the matching option**, per signal, so
an application default can never override a deployment's destination or
credential. The Go SDK itself lets an explicit option beat the environment;
this package passes `:endpoint` and `:headers` to an exporter only when that
exporter's own variables are unset. `:service-name` and `:resource` are
defaults that `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` override.

`:endpoint` takes a URL (`http://host:port`, or `https://...`; a path prefix
is kept) and the exporters add `/v1/traces` and `/v1/logs`.

Values of `OTEL_EXPORTER_OTLP_HEADERS` are percent-decoded by the SDK, and
`Authorization=Bearer tl_...` with the space in it works as written.

## The no-op rule

With no `:endpoint` option and none of `OTEL_EXPORTER_OTLP_ENDPOINT`,
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` or `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`
set, `start!` returns a working handle that exports nothing: spans still get
real ids, every function accepts it. Callers never branch on "is telemetry
on"; a laptop and a test suite run unchanged.

## Explicit parents

A span's parent is an argument (`:parent`), never read from a dynamic var.
let-go 1.13.0 runs every HTTP handler on one shared dynamic-binding stack, so
a "current span" kept in a binding would leak between concurrent requests.
Keep the request span in the request map and pass it down.

## Errors

SDK-internal errors (a rejected token, an unreachable endpoint) are printed
to stderr as `otel: <error>` so they show up in the process's log instead of
failing silently. Exports are batched: nothing is sent until the batch fills,
the delay passes (5 s by default) or `shutdown!` runs, so call `shutdown!`
before the process exits.
