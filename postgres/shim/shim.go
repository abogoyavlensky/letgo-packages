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
	"time"

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

// deadlineDelay is how long a connection waits for the server to answer a
// cancel request before it is closed after all.
const deadlineDelay = 10 * time.Second

// Open returns a pool for dsn (a URL or key=value DSN). With cancelRequest,
// a cancelled context sends the server a cancel request and the connection
// stays usable; without it, pgx's default applies and the connection is
// closed. Either way a DSN without connect_timeout gets a 10 s one.
func Open(dsn string, cancelRequest bool) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.ConnectTimeout == 0 {
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
