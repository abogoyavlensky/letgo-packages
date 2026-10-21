// Package shim opens Postgres pools the way an interactive client needs
// them, which the DSN alone cannot express.
//
// pgx decides what a cancelled context does to a connection through
// pgconn.Config.BuildContextWatcherHandler, a Go function. Its default
// expires the socket deadline and closes the connection (after sending a
// cancel request), so a dedicated *sql.Conn is dead after any cancel: its
// temp tables, SET values and open transaction are gone. psql keeps the
// session after Ctrl+C. CancelRequestContextWatcherHandler does the same:
// it sends the cancel request and leaves the connection usable. let-go
// cannot build that function value, so Open sets it here.
package shim

import (
	"database/sql"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

// defaultConnectTimeout bounds connecting when the DSN sets no
// connect_timeout. pgx's own default dialer has none, so an unreachable
// host would hang the first query or ping for minutes.
const defaultConnectTimeout = 10 * time.Second

// connectTimeoutUnset reports whether nothing set connect_timeout, so the
// default may apply. A nonzero parsed value came from somewhere (the DSN,
// PGCONNECT_TIMEOUT, a service file) and is kept. A zero is ambiguous:
// pgx parses an explicit connect_timeout=0 as "no timeout", which must be
// kept too, so the DSN and the environment are checked for the setting
// itself. (An explicit 0 in a service file is the one case this cannot
// see; it gets the default.)
func connectTimeoutUnset(dsn string, cfg *pgx.ConnConfig) bool {
	if cfg.ConnectTimeout != 0 || os.Getenv("PGCONNECT_TIMEOUT") != "" {
		return false
	}
	return !dsnHasKey(dsn, "connect_timeout")
}

// dsnHasKey reports whether a URL's query or a keyword/value DSN names key.
// Keyword values are read with libpq's quoting ('...' with \' and \\), so
// text inside a quoted password does not count.
func dsnHasKey(dsn, key string) bool {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		return err == nil && u.Query().Has(key)
	}
	i, n := 0, len(dsn)
	skipSpace := func() {
		for i < n && unicode.IsSpace(rune(dsn[i])) {
			i++
		}
	}
	for {
		skipSpace()
		if i >= n {
			return false
		}
		start := i
		for i < n && dsn[i] != '=' && !unicode.IsSpace(rune(dsn[i])) {
			i++
		}
		k := dsn[start:i]
		skipSpace()
		if i >= n || dsn[i] != '=' {
			return false // malformed: pgx will have rejected it already
		}
		i++
		skipSpace()
		if i < n && dsn[i] == '\'' {
			for i++; i < n && dsn[i] != '\''; i++ {
				if dsn[i] == '\\' {
					i++
				}
			}
			i++
		} else {
			for i < n && !unicode.IsSpace(rune(dsn[i])) {
				if dsn[i] == '\\' {
					i++
				}
				i++
			}
		}
		if k == key {
			return true
		}
	}
}

// deadlineDelay is how long a connection waits for the server to answer a
// cancel request before it is closed after all.
const deadlineDelay = 10 * time.Second

// Open returns a pool for dsn (a URL or key=value DSN). With cancelRequest,
// a cancelled context sends the server a cancel request and the connection
// stays usable; without it, pgx's default applies and the connection is
// closed. Either way, unless the DSN, PGCONNECT_TIMEOUT or a service file
// sets connect_timeout (0 included, which means none), connecting is
// bounded by 10 s.
func Open(dsn string, cancelRequest bool) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if connectTimeoutUnset(dsn, cfg) {
		cfg.ConnectTimeout = defaultConnectTimeout
	}
	if cancelRequest {
		cfg.BuildContextWatcherHandler = func(c *pgconn.PgConn) ctxwatch.Handler {
			return &pgconn.CancelRequestContextWatcherHandler{
				Conn:               c,
				CancelRequestDelay: 0,
				DeadlineDelay:      deadlineDelay,
			}
		}
	}
	return stdlib.OpenDB(*cfg), nil
}

// init registers the namespace directly, not through rt.RegisterInstaller:
// see the same note in the sql shim.
func init() {
	ns := vm.NewNamespace("postgres.shim")
	ns.Def("Open", vm.MustBox(Open))
	rt.RegisterNS(ns)
}
