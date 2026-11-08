/*
===========================================================================

commit_batch.go - one commit's statements, built under the lock, applied after

A commit has two halves (#2802). Building it reads the in-memory planes,
encodes the dirty rows and decides what to write: that must run under
Store.mu, because the planes are the store's. Applying it runs the
statements and the SQLite commit: that needs only the database. The
commitBatch is the hand-off between them, an ordered list of statements
whose arguments are already plain values, so the apply half touches no
store state and can run on another goroutine.

Every encoder that writes inside a transaction takes a sqlExecer, which
both *sql.Tx (a door's own transaction) and *commitBatch (the commit)
satisfy, so one encoder serves both paths byte for byte.

===========================================================================
*/

package store

import (
	"database/sql"
	"time"
)

/*
================
sqlExecer

The statement half of *sql.Tx: what an encoder needs to write rows.
*sql.Tx and *commitBatch both satisfy it.
================
*/
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

/*
================
sqlPreparer

The prepare half of *sql.Tx. A row writer that holds one prepares its
repeated statement once (#1943 W3); a commitBatch has none, because its
apply prepares every distinct statement once by itself.
================
*/
type sqlPreparer interface {
	Prepare(query string) (*sql.Stmt, error)
}

/*
================
batchStatement

One recorded statement. args holds plain values only: strings, integers
and the like, never a slice an encoder reuses, so the statement still
reads the same bytes when it runs after the lock is released.
================
*/
type batchStatement struct {
	query string
	args  []any
}

/*
================
commitBatch

One commit attempt's statements in the order the build wrote them.
================
*/
type commitBatch struct {
	statements []batchStatement
}

/*
================
batchResult

What commitBatch.Exec returns: a recorded statement has not run, so it
has no row id and no row count. No encoder reads either.
================
*/
type batchResult struct{}

func (batchResult) LastInsertId() (int64, error) { return 0, nil }
func (batchResult) RowsAffected() (int64, error) { return 0, nil }

/*
================
commitBatch::Exec

Records the statement. The argument slice is copied: a variadic slice
belongs to the caller.
================
*/
func (b *commitBatch) Exec(query string, args ...any) (sql.Result, error) {
	b.statements = append(b.statements, batchStatement{query: query, args: append([]any(nil), args...)})
	return batchResult{}, nil
}

/*
================
batchApply

What one apply needs besides the batch: the database, the clock the
commit timing uses, and the test seam that runs inside the sqlite phase.
================
*/
type batchApply struct {
	db           *sql.DB
	now          func() time.Time
	beforeCommit func()
}

/*
================
commitBatch::apply

Runs the batch as one transaction. Each distinct statement is prepared
once and reused (#1943 W3, which the row writers did per statement before:
on the real driver prepare-once took a 16-row write from 625us to 235us).
Returns the time spent running statements and the time of the SQLite
commit itself, the two apply phases lock_timing.go reports.
================
*/
func (b *commitBatch) apply(a batchApply) (statements, commit time.Duration, err error) {
	begun := a.now()
	tx, err := a.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	prepared := map[string]*sql.Stmt{}
	defer func() {
		for _, stmt := range prepared {
			_ = stmt.Close()
		}
	}()
	for i := range b.statements {
		st := &b.statements[i]
		stmt, ok := prepared[st.query]
		if !ok {
			stmt, err = tx.Prepare(st.query)
			if err != nil {
				return 0, 0, err
			}
			prepared[st.query] = stmt
		}
		if _, err = stmt.Exec(st.args...); err != nil {
			return 0, 0, err
		}
	}
	statements = a.now().Sub(begun)
	committing := a.now()
	if a.beforeCommit != nil {
		a.beforeCommit()
	}
	err = tx.Commit()
	return statements, a.now().Sub(committing), err
}
