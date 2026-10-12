// Package shim wraps the Go OpenTelemetry SDK for let-go: traces and logs
// over OTLP/HTTP.
//
// Like the LiveKit wrapper, this package uses no generated bindings. The SDK
// is configured with functional options and returns structs and interfaces
// (providers, tracers, spans, log records), none of which lgx's
// -opaque-structs bindings can build or hold. The let-go side sees two
// opaque handles, *Handle and *Span, and plain maps for options.
//
// Configuration is the SDK's own environment (OTEL_EXPORTER_OTLP_ENDPOINT,
// _HEADERS, _COMPRESSION, OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES,
// OTEL_TRACES_SAMPLER, ...). The SDK lets an explicit option beat the
// environment, which is the wrong way round for an application default, so
// Start only passes an option for a signal whose own variables are unset.
package shim

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// shutdownTimeout bounds how long Shutdown waits for the final flush.
const shutdownTimeout = 5 * time.Second

// Handle is the let-go side's token for a started SDK. A no-op handle has
// providers but no exporter: spans get real ids (so correlation code runs
// the same everywhere) and nothing leaves the process.
type Handle struct {
	tp     *sdktrace.TracerProvider
	lp     *sdklog.LoggerProvider // nil on a no-op handle
	tracer trace.Tracer
	logger otellog.Logger // nil on a no-op handle

	once sync.Once
}

// Span is a started span plus the context that carries it: the context is
// how a child finds its parent and how a log record is tied to a trace.
type Span struct {
	span trace.Span
	ctx  context.Context
}

var errorHandlerOnce sync.Once

// installErrorHandler makes SDK-internal errors (a rejected token, an
// unreachable endpoint) visible in the process's stderr instead of being
// dropped. Once per process: the handler is global.
func installErrorHandler() {
	errorHandlerOnce.Do(func() {
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			fmt.Fprintln(os.Stderr, "otel:", err)
		}))
	})
}

// envSet reports whether any of the variables is set to a non-empty value.
func envSet(names ...string) bool {
	for _, n := range names {
		if os.Getenv(n) != "" {
			return true
		}
	}
	return false
}

// target is a parsed :endpoint option.
type target struct {
	hostPort string
	insecure bool
	basePath string // a path prefix on the endpoint, without the signal path
}

func parseEndpoint(raw string) (target, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return target{}, fmt.Errorf("otel: bad endpoint: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return target{}, fmt.Errorf("otel: bad endpoint %q: want http://host:port or https://host:port", raw)
	}
	return target{
		hostPort: u.Host,
		insecure: u.Scheme == "http",
		basePath: strings.TrimRight(u.Path, "/"),
	}, nil
}

func stringMap(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func newResource(ctx context.Context, opts map[string]any) (*resource.Resource, error) {
	// Sources later in the list win, so the option defaults come first and
	// the environment (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES) overrides.
	var attrs []attribute.KeyValue
	if s, ok := opts["service-name"].(string); ok && s != "" {
		attrs = append(attrs, attribute.String("service.name", s))
	}
	attrs = append(attrs, attrList(opts["resource"])...)
	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
}

// Start builds the tracer and logger providers. Without an :endpoint option
// and without any OTEL_EXPORTER_OTLP_*ENDPOINT variable it returns a no-op
// handle. It fails only on a malformed endpoint or resource.
func Start(optsVal vm.Value) (*Handle, error) {
	installErrorHandler()
	opts := asMap(optsVal)
	endpoint, _ := opts["endpoint"].(string)
	configured := endpoint != "" || envSet(
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")

	if !configured {
		tp := sdktrace.NewTracerProvider()
		return &Handle{tp: tp, tracer: tp.Tracer("otel")}, nil
	}

	var tgt target
	if endpoint != "" {
		var err error
		if tgt, err = parseEndpoint(endpoint); err != nil {
			return nil, err
		}
	}
	headers := stringMap(opts["headers"])

	ctx := context.Background()
	res, err := newResource(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("otel: resource: %w", err)
	}

	// Traces. An option only when this signal's own variables are unset.
	var topts []otlptracehttp.Option
	if endpoint != "" && !envSet("OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") {
		topts = append(topts, otlptracehttp.WithEndpoint(tgt.hostPort))
		if tgt.insecure {
			topts = append(topts, otlptracehttp.WithInsecure())
		}
		if tgt.basePath != "" {
			topts = append(topts, otlptracehttp.WithURLPath(tgt.basePath+"/v1/traces"))
		}
	}
	if len(headers) > 0 && !envSet("OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_TRACES_HEADERS") {
		topts = append(topts, otlptracehttp.WithHeaders(headers))
	}
	texp, err := otlptracehttp.New(ctx, topts...)
	if err != nil {
		return nil, fmt.Errorf("otel: trace exporter: %w", err)
	}

	// Logs, by the same rule.
	var lopts []otlploghttp.Option
	if endpoint != "" && !envSet("OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") {
		lopts = append(lopts, otlploghttp.WithEndpoint(tgt.hostPort))
		if tgt.insecure {
			lopts = append(lopts, otlploghttp.WithInsecure())
		}
		if tgt.basePath != "" {
			lopts = append(lopts, otlploghttp.WithURLPath(tgt.basePath+"/v1/logs"))
		}
	}
	if len(headers) > 0 && !envSet("OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_LOGS_HEADERS") {
		lopts = append(lopts, otlploghttp.WithHeaders(headers))
	}
	lexp, err := otlploghttp.New(ctx, lopts...)
	if err != nil {
		_ = texp.Shutdown(ctx)
		return nil, fmt.Errorf("otel: log exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(texp),
		sdktrace.WithResource(res),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(lexp)),
		sdklog.WithResource(res),
	)
	return &Handle{
		tp:     tp,
		lp:     lp,
		tracer: tp.Tracer("otel"),
		logger: lp.Logger("otel"),
	}, nil
}

// Enabled reports whether the handle exports anything: false for the no-op
// handle Start returns when no endpoint is configured.
func Enabled(h *Handle) bool { return h != nil && h.lp != nil }

// Shutdown flushes and stops both providers, waiting at most five seconds.
// A failing flush is printed to stderr rather than returned: it must not
// crash a process that is already exiting. The second call does nothing.
func Shutdown(h *Handle) {
	if h == nil {
		return
	}
	h.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := h.tp.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "otel: shutdown traces:", err)
		}
		if h.lp != nil {
			if err := h.lp.Shutdown(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "otel: shutdown logs:", err)
			}
		}
	})
}

// StartSpan starts a span. opts: parent (a *Span or nil: a new root),
// kind ("server", "client" or "internal"), attrs.
func StartSpan(h *Handle, name string, optsVal vm.Value) *Span {
	opts := asMap(optsVal)
	ctx := context.Background()
	if p, ok := opts["parent"].(*Span); ok && p != nil {
		ctx = p.ctx
	}
	kind := trace.SpanKindInternal
	switch opts["kind"] {
	case "server":
		kind = trace.SpanKindServer
	case "client":
		kind = trace.SpanKindClient
	}
	ctx, span := h.tracer.Start(ctx, name,
		trace.WithSpanKind(kind),
		trace.WithAttributes(attrList(opts["attrs"])...))
	return &Span{span: span, ctx: ctx}
}

// EndSpan ends a span; ending twice is harmless.
func EndSpan(s *Span) {
	if s != nil {
		s.span.End()
	}
}

// SetAttrs adds attributes to a span.
func SetAttrs(s *Span, attrsVal vm.Value) {
	if s != nil {
		s.span.SetAttributes(attrList(ToGo(attrsVal))...)
	}
}

// SetError marks the span's status as Error with the message.
func SetError(s *Span, msg string) {
	if s != nil {
		s.span.SetStatus(codes.Error, msg)
	}
}

// Log emits a log record. level: "debug", "info", "warn" or "error".
// opts: span, whose trace and span ids the record carries.
func Log(h *Handle, level, body string, attrsVal, optsVal vm.Value) {
	if h == nil || h.logger == nil {
		return
	}
	ctx := context.Background()
	if s, ok := asMap(optsVal)["span"].(*Span); ok && s != nil {
		ctx = s.ctx
	}
	var sev otellog.Severity
	switch level {
	case "debug":
		sev = otellog.SeverityDebug
	case "warn":
		sev = otellog.SeverityWarn
	case "error":
		sev = otellog.SeverityError
	default:
		sev = otellog.SeverityInfo
	}
	var r otellog.Record
	now := time.Now()
	r.SetTimestamp(now)
	r.SetObservedTimestamp(now)
	r.SetSeverity(sev)
	r.SetSeverityText(strings.ToUpper(level))
	r.SetBody(attribute.StringValue(body))
	r.AddAttributes(attrList(ToGo(attrsVal))...)
	h.logger.Emit(ctx, r)
}

// TraceID is the span's trace id as 32 lowercase hex characters.
func TraceID(s *Span) string { return s.span.SpanContext().TraceID().String() }

// SpanID is the span's id as 16 lowercase hex characters.
func SpanID(s *Span) string { return s.span.SpanContext().SpanID().String() }

// attrList converts a let-go map's entries to span attributes. Only strings,
// booleans, integers and floats are kept; anything else is dropped, so a bad
// attribute can never throw.
func attrList(v any) []attribute.KeyValue {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(m))
	for k, val := range m {
		switch t := val.(type) {
		case string:
			out = append(out, attribute.String(k, t))
		case bool:
			out = append(out, attribute.Bool(k, t))
		case int:
			out = append(out, attribute.Int64(k, int64(t)))
		case int64:
			out = append(out, attribute.Int64(k, t))
		case float64:
			out = append(out, attribute.Float64(k, t))
		}
	}
	return out
}

// ToGo lowers a let-go value to plain Go values. Copied from the livekit
// shim: the shims are independent Go modules, and sharing it would take a
// third one.
//
// Unbox alone is not enough: a map and a vector both unbox to themselves.
// A let-go map is a *vm.PersistentMap whose Seq yields vm.MapEntry, so a
// type switch on vm.Map alone silently turns every map into a list of
// pairs. An opaque handle (*Span) unboxes to itself, which is how a :parent
// or :span option survives the trip.
func ToGo(v vm.Value) any {
	if v == nil || v == vm.NIL {
		return nil
	}
	switch t := v.(type) {
	case vm.Keyword:
		return string(t)
	case vm.Symbol:
		return string(t)
	case vm.String:
		return string(t)
	}
	if t := v.Type(); t == vm.MapType || t == vm.SortedMapType {
		out := map[string]any{}
		if s, ok := v.(vm.Sequable); ok {
			for sq := s.Seq(); sq != nil; sq = sq.Next() {
				if e, ok := sq.First().(vm.MapEntry); ok {
					out[keyString(e.Key)] = ToGo(e.Value)
				}
			}
		}
		return out
	}
	if s, ok := v.(vm.Sequable); ok {
		sq := s.Seq()
		if sq != nil {
			if _, isEntry := sq.First().(vm.MapEntry); isEntry {
				out := map[string]any{}
				for ; sq != nil; sq = sq.Next() {
					e := sq.First().(vm.MapEntry)
					out[keyString(e.Key)] = ToGo(e.Value)
				}
				return out
			}
		}
		out := []any{}
		for ; sq != nil; sq = sq.Next() {
			out = append(out, ToGo(sq.First()))
		}
		return out
	}
	return v.Unbox()
}

// keyString renders a map key as a name: keywords lose their leading colon,
// so {:port 1} and {"port" 1} are the same option.
func keyString(k vm.Value) string {
	switch t := k.(type) {
	case vm.Keyword:
		return string(t)
	case vm.String:
		return string(t)
	case vm.Symbol:
		return string(t)
	}
	return fmt.Sprintf("%v", k.Unbox())
}

// asMap lowers a let-go map argument; anything else is an empty map.
func asMap(v vm.Value) map[string]any {
	if m, ok := ToGo(v).(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// init registers the namespace directly rather than through
// rt.RegisterInstaller: pkg/rt drains its installer queue during its own
// package init, which Go runs before this one, so anything queued from
// here would never run.
func init() {
	ns := vm.NewNamespace("otel.shim")
	ns.Def("Start", vm.MustBox(Start))
	ns.Def("Enabled", vm.MustBox(Enabled))
	ns.Def("Shutdown", vm.MustBox(Shutdown))
	ns.Def("StartSpan", vm.MustBox(StartSpan))
	ns.Def("EndSpan", vm.MustBox(EndSpan))
	ns.Def("SetAttrs", vm.MustBox(SetAttrs))
	ns.Def("SetError", vm.MustBox(SetError))
	ns.Def("Log", vm.MustBox(Log))
	ns.Def("TraceID", vm.MustBox(TraceID))
	ns.Def("SpanID", vm.MustBox(SpanID))
	rt.RegisterNS(ns)
}
