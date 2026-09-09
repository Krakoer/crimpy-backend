package testutil

import (
	"context"
	"crimpy/backend/internal/db"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrInjectedFault is what a FaultyDBTX answers with in place of running a
// statement the fault matches. Tests compare against it rather than a message.
var ErrInjectedFault = errors.New("injected query fault")

// SQLMatcher reports whether a statement is one the fault applies to. It reads
// the SQL text pgx is handed, which for a sqlc query is the generated constant.
type SQLMatcher func(sql string) bool

// QueryNamed matches the one statement sqlc generated for a named query, by the
// marker sqlc keeps at the top of the constant. Matching on the name is what
// lets a fault be aimed at a single query without standing in for the database
// anywhere else, so a handler under test keeps running against real rows.
func QueryNamed(name string) SQLMatcher {
	if name == "" {
		panic("testutil: QueryNamed needs a query name")
	}
	marker := fmt.Sprintf("-- name: %s :", name)
	return func(sql string) bool { return strings.Contains(sql, marker) }
}

// FaultyDBTX is a db.DBTX that fails the statements a matcher names and
// delegates every other call, unchanged, to the DBTX it wraps. A test that asks
// for one fault therefore keeps real behaviour, and real errors, everywhere
// else, and nothing builds one unless a test does.
type FaultyDBTX struct {
	inner   db.DBTX
	matches SQLMatcher

	mu       sync.Mutex
	injected int
}

var _ db.DBTX = (*FaultyDBTX)(nil)

// NewFaultyDBTX wraps inner so that the statements matches names fail. Both
// arguments are required: a fault seam with nothing behind it, or with nothing
// to match, would quietly test the wrong thing.
func NewFaultyDBTX(inner db.DBTX, matches SQLMatcher) *FaultyDBTX {
	if inner == nil {
		panic("testutil: NewFaultyDBTX needs a DBTX to wrap")
	}
	if matches == nil {
		panic("testutil: NewFaultyDBTX needs a matcher")
	}
	return &FaultyDBTX{inner: inner, matches: matches}
}

// FailingQueries is what a handler test injects: sqlc queries over the real
// pool with one statement failing. The FaultyDBTX comes back so the test can
// assert the fault landed.
func FailingQueries(inner db.DBTX, matches SQLMatcher) (*db.Queries, *FaultyDBTX) {
	faulty := NewFaultyDBTX(inner, matches)
	return db.New(faulty), faulty
}

// Injected counts the statements failed so far. A test asserts it, because a
// matcher naming a query the handler never runs would otherwise read as a
// passing test of nothing.
func (f *FaultyDBTX) Injected() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.injected
}

func (f *FaultyDBTX) fails(sql string) bool {
	if !f.matches(sql) {
		return false
	}
	f.mu.Lock()
	f.injected++
	f.mu.Unlock()
	return true
}

func (f *FaultyDBTX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if f.fails(sql) {
		return pgconn.CommandTag{}, ErrInjectedFault
	}
	return f.inner.Exec(ctx, sql, args...)
}

func (f *FaultyDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if f.fails(sql) {
		return nil, ErrInjectedFault
	}
	return f.inner.Query(ctx, sql, args...)
}

func (f *FaultyDBTX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if f.fails(sql) {
		return faultyRow{}
	}
	return f.inner.QueryRow(ctx, sql, args...)
}

// faultyRow hands the injected error to whoever scans it, which is how a
// QueryRow call fails: pgx reports the error from Scan, not from the call.
type faultyRow struct{}

func (faultyRow) Scan(dest ...any) error { return ErrInjectedFault }
