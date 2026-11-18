package traffic

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"modernc.org/sqlite"
)

// Configure every pooled connection: max_page_count is connection-local, and
// page_size can differ in an existing database. A one-off initialization PRAGMA
// would leave new connections able to bypass the index budget.
type quotaConnector struct {
	dsn   string
	bytes int64
}

func (c *quotaConnector) Driver() driver.Driver { return &sqlite.Driver{} }
func (c *quotaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Driver().Open(c.dsn)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (driver.Conn, error) { conn.Close(); return nil, err }
	query, ok := conn.(driver.QueryerContext)
	if !ok {
		return fail(errors.New("SQLite driver lacks bounded index configuration"))
	}
	rows, err := query.QueryContext(ctx, `PRAGMA page_size`, nil)
	if err != nil {
		return fail(err)
	}
	values := make([]driver.Value, 1)
	err = rows.Next(values)
	closeErr := rows.Close()
	if err != nil {
		return fail(err)
	}
	if closeErr != nil {
		return fail(closeErr)
	}
	pageSize, ok := values[0].(int64)
	if !ok || pageSize < 512 || pageSize > 65536 {
		return fail(errors.New("unsupported SQLite page size"))
	}
	exec, ok := conn.(driver.ExecerContext)
	if !ok {
		return fail(errors.New("SQLite driver lacks bounded index configuration"))
	}
	if _, err = exec.ExecContext(ctx, fmt.Sprintf(`PRAGMA max_page_count=%d`, c.bytes/pageSize), nil); err != nil {
		return fail(err)
	}
	return conn, nil
}
