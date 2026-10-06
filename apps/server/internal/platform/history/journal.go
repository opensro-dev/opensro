/*
===========================================================================

journal.go - durable operator events, sessions and connected intervals

Each service owns one SQLite journal beside its authority. Gameplay enqueues
bounded records; one writer serializes them and checkpoints live intervals.
Lost records and write failures remain visible in health, never as clean exits.

===========================================================================
*/
package history

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const QueueCapacity = 4096
const CheckpointPeriod = 15 * time.Second

/*
================
Event

Client fields are evidence, not authority. Only transport lifecycle records
change connected intervals. ID permits idempotent retries of client reports.
================
*/
type Event struct {
	ID        string            `json:"id"`
	At        int64             `json:"at"`
	Service   string            `json:"service"`
	Build     string            `json:"build"`
	Shard     string            `json:"shard"`
	Account   string            `json:"account"`
	Character string            `json:"character"`
	Session   string            `json:"session"`
	Kind      string            `json:"kind"`
	Category  string            `json:"category"`
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Level     string            `json:"level"`
	Evidence  string            `json:"evidence"`
	Opcode    string            `json:"opcode,omitempty"`
	Stack     string            `json:"stack,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
	Lifecycle bool              `json:"-"`
	Attached  bool              `json:"-"`
	InWorld   bool              `json:"-"`
	receipt   chan error
}

/*
================
Journal
================
*/
type Journal struct {
	db                          *sql.DB
	service, shard, build, boot string
	mu                          sync.RWMutex
	closed                      bool
	queue                       chan Event
	stop                        chan struct{}
	done                        chan struct{}
	dropped                     atomic.Uint64
	failed                      atomic.Uint64
	errorMu                     sync.Mutex
	lastError                   string
}

/*
================
NewID
================
*/
func NewID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

/*
================
Open
================
*/
func Open(path, service, shard, build string) (*Journal, error) {
	boot, err := NewID()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	j := &Journal{db: db, service: service, shard: shard, build: build, boot: boot,
		queue: make(chan Event, QueueCapacity), stop: make(chan struct{}), done: make(chan struct{})}
	if err := j.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = db.Close()
		return nil, err
	}
	go j.run()
	j.Record(Event{Kind: "service_started", Category: "expected", Code: "startup", Message: "Service started"})
	return j, nil
}

/*
================
initialize

An unclosed session ends at its last durable checkpoint after a crash.
It is explicitly estimated; time between the checkpoint and crash is unknown.
================
*/
func (j *Journal) initialize() error {
	_, err := j.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS events (seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT UNIQUE NOT NULL, at INTEGER NOT NULL,
 account TEXT NOT NULL, character TEXT NOT NULL, session TEXT NOT NULL, kind TEXT NOT NULL,
 category TEXT NOT NULL, level TEXT NOT NULL, fingerprint TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS events_account_time ON events(account,at);
CREATE INDEX IF NOT EXISTS events_character_time ON events(character,at);
CREATE INDEX IF NOT EXISTS events_session_time ON events(session,at);
CREATE INDEX IF NOT EXISTS events_time ON events(at);
CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, account TEXT NOT NULL, character TEXT NOT NULL,
 shard TEXT NOT NULL, started INTEGER NOT NULL, seen INTEGER NOT NULL, transitioned INTEGER NOT NULL, ended INTEGER NOT NULL DEFAULT 0,
 attached INTEGER NOT NULL, category TEXT NOT NULL DEFAULT '', code TEXT NOT NULL DEFAULT '', estimated INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS sessions_account ON sessions(account);
CREATE INDEX IF NOT EXISTS sessions_character ON sessions(character);
CREATE TABLE IF NOT EXISTS intervals (id INTEGER PRIMARY KEY, session TEXT NOT NULL, account TEXT NOT NULL,
 character TEXT NOT NULL, start INTEGER NOT NULL, finish INTEGER NOT NULL, active INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS intervals_account ON intervals(account,start);
CREATE INDEX IF NOT EXISTS intervals_session ON intervals(session,active);
CREATE TABLE IF NOT EXISTS health (id INTEGER PRIMARY KEY CHECK(id=1), dropped INTEGER NOT NULL, failed INTEGER NOT NULL, pruned_before INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS logins (account TEXT PRIMARY KEY, at INTEGER NOT NULL);
INSERT OR IGNORE INTO health VALUES(1,0,0,0);
INSERT INTO events(id,at,account,character,session,kind,category,level,fingerprint,data)
 SELECT lower(hex(randomblob(16))),seen,account,character,id,'ended','unknown','','process_interrupted',
 json_object('at',seen,'account',account,'character',character,'session',id,'kind','ended','category','unknown',
 'code','process_interrupted','message','Previous process stopped without a final session event. End time is estimated from its last checkpoint.',
 'evidence','recovered','service','gameworld','build','unknown') FROM sessions WHERE ended=0;
UPDATE sessions SET ended=seen, attached=0, estimated=1, category='unknown', code='process_interrupted' WHERE ended=0;
UPDATE intervals SET active=0 WHERE active=1;`)
	return err
}

/*
================
SessionID
================
*/
func (j *Journal) SessionID(id string) string { return j.boot + ":" + id }

/*
================
Record

Never block a character or transport lock on disk. Capacity failure is counted
and exposed to the operator; it is not silently presented as complete history.
================
*/
func (j *Journal) Record(e Event) bool {
	if j == nil {
		return false
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.closed {
		return false
	}
	if e.ID == "" {
		id, err := NewID()
		if err != nil {
			j.failure(err)
			return false
		}
		e.ID = id
	}
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	e.Service = j.service
	if e.Build == "" {
		e.Build = j.build
	}
	if e.Shard == "" {
		e.Shard = j.shard
	}
	if e.Evidence == "" {
		e.Evidence = "server"
	}
	e.Message = bounded(e.Message, 2048)
	e.Stack = bounded(e.Stack, 8192)
	select {
	case j.queue <- e:
		return true
	default:
		j.dropped.Add(1)
		return false
	}
}

/*
================
bounded
================
*/
func bounded(s string, limit int) string {
	if len(s) > limit {
		return s[:limit]
	}
	return s
}

/*
================
failure
================
*/
func (j *Journal) failure(err error) {
	j.failed.Add(1)
	j.errorMu.Lock()
	if j.lastError != err.Error() {
		fmt.Fprintf(os.Stderr, "history persistence error: %v\n", err)
	}
	j.lastError = err.Error()
	j.errorMu.Unlock()
}

/*
================
run
================
*/
func (j *Journal) run() {
	defer close(j.done)
	ticker := time.NewTicker(CheckpointPeriod)
	defer ticker.Stop()
	for {
		select {
		case e := <-j.queue:
			j.consume(e)
		case <-ticker.C:
			if err := j.checkpoint(time.Now().UnixMilli()); err != nil {
				j.failure(err)
			}
		case <-j.stop:
			for len(j.queue) > 0 {
				j.consume(<-j.queue)
			}
			if err := j.checkpoint(time.Now().UnixMilli()); err != nil {
				j.failure(err)
			}
			return
		}
	}
}

/*
================
write
================
*/
func (j *Journal) write(e Event) error {
	if e.Kind == "log" && e.Session != "" && e.Account == "" {
		err := j.db.QueryRow(`SELECT account,character FROM sessions WHERE id=?`, e.Session).Scan(&e.Account, &e.Character)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tx, err := j.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`INSERT OR IGNORE INTO events(id,at,account,character,session,kind,category,level,fingerprint,data) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.At, e.Account, e.Character, e.Session, e.Kind, e.Category, e.Level, Fingerprint(e), string(data))
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		var account string
		if err := tx.QueryRow(`SELECT account FROM events WHERE id=?`, e.ID).Scan(&account); err != nil {
			return err
		}
		if account != e.Account {
			return fmt.Errorf("incident reference belongs to another account")
		}
		return tx.Commit()
	}
	if e.Kind == "login_succeeded" {
		if _, err := tx.Exec(`INSERT INTO logins(account,at) VALUES(?,?) ON CONFLICT(account) DO UPDATE SET at=MAX(at,excluded.at)`, e.Account, e.At); err != nil {
			return err
		}
	}
	if e.Lifecycle && e.Session != "" {
		if err := updateSession(tx, e); err != nil {
			return err
		}
	}
	return tx.Commit()
}

/*
================
updateSession
================
*/
func updateSession(tx *sql.Tx, e Event) error {
	var ended, seen int64
	var lastSeen int64
	var wasAttached bool
	var character string
	err := tx.QueryRow(`SELECT ended,transitioned,character,seen,attached FROM sessions WHERE id=?`, e.Session).Scan(&ended, &seen, &character, &lastSeen, &wasAttached)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if ended != 0 || e.At < seen {
		return nil
	}
	if e.Character == "" {
		e.Character = character
	}
	if e.Kind != "ended" || wasAttached || lastSeen == 0 {
		lastSeen = e.At
	}
	if _, err := tx.Exec(`INSERT INTO sessions(id,account,character,shard,started,seen,transitioned,attached) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET account=excluded.account, character=excluded.character, seen=excluded.seen, transitioned=excluded.transitioned, attached=excluded.attached`,
		e.Session, e.Account, e.Character, e.Shard, e.At, lastSeen, e.At, e.Attached); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE intervals SET finish=MAX(start,?),active=0 WHERE session=? AND active=1`, e.At, e.Session); err != nil {
		return err
	}
	if e.Kind == "ended" {
		_, err = tx.Exec(`UPDATE sessions SET ended=?,category=?,code=?,attached=0 WHERE id=?`, e.At, e.Category, e.Code, e.Session)
		return err
	}
	if e.Attached && e.InWorld && e.Character != "" {
		_, err = tx.Exec(`INSERT INTO intervals(session,account,character,start,finish,active) VALUES(?,?,?,?,?,1)`, e.Session, e.Account, e.Character, e.At, e.At)
	}
	return err
}

/*
================
checkpoint

Raw events retain 90 days (at most 200,000 rows). Session summaries and
intervals are retained for all-time totals. The boundary is visible in health.
================
*/
func (j *Journal) checkpoint(now int64) error {
	cutoff := now - int64(90*24*time.Hour/time.Millisecond)
	dropped, failed := j.dropped.Swap(0), j.failed.Swap(0)
	tx, err := j.db.Begin()
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE sessions SET seen=MAX(seen,?) WHERE ended=0 AND attached=1`, []any{now}},
			{`UPDATE intervals SET finish=MAX(finish,?) WHERE active=1`, []any{now}},
			{`UPDATE health SET pruned_before=MAX(pruned_before,COALESCE((SELECT MAX(at) FROM events WHERE at<? OR seq <= (SELECT COALESCE(MAX(seq),0)-200000 FROM events)),0)) WHERE id=1`, []any{cutoff}},
			{`DELETE FROM events WHERE at<? OR seq <= (SELECT COALESCE(MAX(seq),0)-200000 FROM events)`, []any{cutoff}},
			{`UPDATE health SET dropped=dropped+?,failed=failed+? WHERE id=1`, []any{dropped, failed}},
		} {
			if _, err = tx.Exec(statement.sql, statement.args...); err != nil {
				break
			}
		}
		if err == nil {
			err = tx.Commit()
		}
	}
	if err != nil {
		j.dropped.Add(dropped)
		j.failed.Add(failed)
	}
	return err
}

/*
================
RecordConfirmed

HTTP acknowledgement follows the SQLite commit. A lost acknowledgement can
be retried with the same reference without creating another incident.
================
*/
func (j *Journal) RecordConfirmed(ctx context.Context, e Event) error {
	e.receipt = make(chan error, 1)
	if !j.Record(e) {
		return fmt.Errorf("history queue unavailable")
	}
	select {
	case err := <-e.receipt:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

/*
================
consume
================
*/
func (j *Journal) consume(e Event) {
	err := j.write(e)
	if err != nil {
		j.failure(err)
	}
	if e.receipt != nil {
		e.receipt <- err
	}
}

/*
================
Close
================
*/
func (j *Journal) Close() error {
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return nil
	}
	j.closed = true
	close(j.stop)
	j.mu.Unlock()
	<-j.done
	return j.db.Close()
}

/*
================
Health
================
*/
func (j *Journal) Health() map[string]any {
	var dropped, failed uint64
	var pruned int64
	err := j.db.QueryRow(`SELECT dropped,failed,pruned_before FROM health WHERE id=1`).Scan(&dropped, &failed, &pruned)
	j.errorMu.Lock()
	message := j.lastError
	j.errorMu.Unlock()
	if err != nil {
		message = fmt.Sprint(err)
	}
	return map[string]any{"dropped": dropped + j.dropped.Load(), "failed": failed + j.failed.Load(), "lastError": message,
		"prunedBefore": pruned, "pending": len(j.queue), "checkpointSeconds": CheckpointPeriod.Seconds(), "service": j.service, "build": j.build}
}
