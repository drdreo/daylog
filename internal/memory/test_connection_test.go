package memory

import (
	"context"
	"database/sql"
)

// Test-only conveniences for deliberate corruption/lock/crash scenarios.
func (c *connection) Exec(q string, args ...any) (sql.Result, error) {
	return c.ExecContext(context.Background(), q, args...)
}
func (c *connection) Query(q string, args ...any) (*sql.Rows, error) {
	return c.QueryContext(context.Background(), q, args...)
}
func (c *connection) QueryRow(q string, args ...any) *sql.Row {
	return c.QueryRowContext(context.Background(), q, args...)
}
func (c *connection) Begin() (*sql.Tx, error) { return c.BeginTx(context.Background(), nil) }
