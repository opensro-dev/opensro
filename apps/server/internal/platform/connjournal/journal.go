/*
===========================================================================

journal.go - the connection journal: one row per entered session

Part 1 of the player census (opensro#2716, console#9): who connected,
from where, for how long. Each GameWorld owns one SQLite journal beside
its authority, never inside the character store, so recording a
connection moves no compatibility number. A row opens when a session
enters the world and closes when the session ends; a crash leaves the
row open, and the next Open closes it at its last checkpoint, marked
estimated - the same recovery the history journal keeps.

No raw address is stored, and none is queued: RecordEntered hashes the
client address with the shard's key (key.go) before the entry leaves
the caller. The journal keeps the full address hash (ipKey) and the
hash of its network - /24 for IPv4, /48 for IPv6 - so correlation can
ask "same address" and "same network" without either answer naming a
place. Rows are pruned 30 days after they close; aggregates (#2717)
are what is kept beyond that.

This is a server rule native does not have (port-only, not native): it
runs only behind SRO_CONNECTION_JOURNAL, default off (wiring_census.go).

===========================================================================
*/
package connjournal

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// EnvConnectionJournal is the flag that turns the census on. Off (the
// native posture) means no journal is opened and nothing is recorded.
const EnvConnectionJournal = "SRO_CONNECTION_JOURNAL"

// queueCapacity bounds the entered/ended records waiting for the one
// writer; past it a record is dropped and counted, never a stall of
// the transport lane that produced it (the history journal's rule).
const queueCapacity = 1024

// checkpointPeriod is how often open rows advance their seen mark and
// closed rows past retention are pruned.
const checkpointPeriod = 15 * time.Second

// retentionDays is how long a closed row is kept.
const retentionDays = 30

// maxUserAgentBytes bounds the User-Agent a row stores: a browser's
// header is evidence, not a document.
const maxUserAgentBytes = 256

/*
================
Entry

One entered session as the transport reports it. Address is the real
client address (the transport resolved the edge's forwarding at
accept); the journal hashes it in RecordEntered and never stores it.
Build and Screen stay empty until a client presents them on the wire;
no client does today, and this journal adds no client field.
================
*/
type Entry struct {
	Session   string
	Account   string
	Character string
	Shard     string
	Transport string
	Address   string
	UserAgent string
	Build     string
	Screen    string
}

/*
================
storedRow

An Entry with its address already hashed: the only form that is
queued or written.
================
*/
type storedRow struct {
	session   string
	account   string
	character string
	shard     string
	transport string
	ipKey     string
	netKey    string
	userAgent string
	build     string
	screen    string
}

// write is one queued change: a row to open, or (row nil) a session
// whose open row closes. at is when the change was recorded.
type write struct {
	row     *storedRow
	session string
	at      int64
}

/*
================
Journal

One writer serializes every change; RecordEntered and CloseSession
only enqueue. Dropped and failed counts stay visible in Health: a
lost census row is an operator fact, not a silent gap.
================
*/
type Journal struct {
	db    *sql.DB
	shard string
	key   []byte
	now   func() time.Time

	mu      sync.RWMutex
	closed  bool
	queue   chan write
	stop    chan struct{}
	done    chan struct{}
	dropped atomic.Uint64
	failed  atomic.Uint64
}

// millis is the journal's one time form: UTC milliseconds.
func millis(t time.Time) int64 { return t.UTC().UnixMilli() }

/*
================
Open

Creates the journal beside the shard's authority and closes whatever
a crashed run left open at its last checkpoint, estimated. The key is
the shard's own (key.go); it never leaves this process.
================
*/
func Open(path, shard string, key []byte) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	j := &Journal{db: db, shard: shard, key: append([]byte(nil), key...), now: time.Now,
		queue: make(chan write, queueCapacity), stop: make(chan struct{}), done: make(chan struct{})}
	if err := j.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = db.Close()
		return nil, err
	}
	go j.run()
	return j, nil
}

/*
================
initialize

The connections table, and the crash recovery: a row still open when
the journal opens belongs to a process that stopped without ending
its sessions, so it closes at its last checkpoint (seen, advanced
every checkpointPeriod while the session lived), marked estimated.
================
*/
func (j *Journal) initialize() error {
	_, err := j.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS connections (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 session TEXT NOT NULL, account TEXT NOT NULL, character TEXT NOT NULL, shard TEXT NOT NULL,
 started INTEGER NOT NULL, seen INTEGER NOT NULL, ended INTEGER NOT NULL DEFAULT 0,
 estimated INTEGER NOT NULL DEFAULT 0, transport TEXT NOT NULL DEFAULT '',
 ip_key TEXT NOT NULL DEFAULT '', net_key TEXT NOT NULL DEFAULT '',
 build TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', screen TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS connections_session ON connections(session);
CREATE INDEX IF NOT EXISTS connections_account ON connections(account, started);
CREATE INDEX IF NOT EXISTS connections_ip_key ON connections(ip_key, started);
CREATE INDEX IF NOT EXISTS connections_started ON connections(started);
UPDATE connections SET ended=seen, estimated=1 WHERE ended=0;`)
	return err
}

/*
================
RecordEntered

Opens the session's row. Entering again on the same session (another
character after a return to selection) first closes the open row at
the entry instant: a row is one character's entered span. The address
is hashed here, under the caller's key, and never queued raw.
================
*/
func (j *Journal) RecordEntered(entry Entry) bool {
	if j == nil || entry.Session == "" {
		return false
	}
	ipKey, netKey := keyHashes(j.key, entry.Address)
	row := &storedRow{session: entry.Session, account: entry.Account, character: entry.Character,
		shard: entry.Shard, transport: entry.Transport, ipKey: ipKey, netKey: netKey,
		userAgent: bounded(entry.UserAgent, maxUserAgentBytes), build: entry.Build, screen: entry.Screen}
	return j.enqueue(write{row: row, session: entry.Session, at: millis(j.now())})
}

/*
================
CloseSession

Closes the session's open row at now. A session that ends without
entering has no row; the close is then a no-op on the writer.
================
*/
func (j *Journal) CloseSession(session string) bool {
	if j == nil || session == "" {
		return false
	}
	return j.enqueue(write{session: session, at: millis(j.now())})
}

/*
================
enqueue

Never block a transport lane on disk. Capacity failure is counted and
exposed in Health; it is not silently presented as a kept row.
================
*/
func (j *Journal) enqueue(w write) bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.closed {
		return false
	}
	select {
	case j.queue <- w:
		return true
	default:
		j.dropped.Add(1)
		return false
	}
}

/*
================
run
================
*/
func (j *Journal) run() {
	defer close(j.done)
	ticker := time.NewTicker(checkpointPeriod)
	defer ticker.Stop()
	for {
		select {
		case w := <-j.queue:
			if err := j.apply(w); err != nil {
				j.failed.Add(1)
			}
		case <-ticker.C:
			if err := j.checkpoint(); err != nil {
				j.failed.Add(1)
			}
		case <-j.stop:
			for len(j.queue) > 0 {
				if err := j.apply(<-j.queue); err != nil {
					j.failed.Add(1)
				}
			}
			if err := j.checkpoint(); err != nil {
				j.failed.Add(1)
			}
			return
		}
	}
}

/*
================
apply

One queued change. Opening first closes the session's still-open row
at the same instant (a character switch), then inserts: the writer is
the only goroutine here, so the pair cannot interleave.
================
*/
func (j *Journal) apply(w write) error {
	if w.row != nil {
		if _, err := j.db.Exec(`UPDATE connections SET ended=? WHERE session=? AND ended=0`, w.at, w.session); err != nil {
			return err
		}
		_, err := j.db.Exec(`INSERT INTO connections(session,account,character,shard,started,seen,transport,ip_key,net_key,build,user_agent,screen)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			w.row.session, w.row.account, w.row.character, w.row.shard, w.at, w.at,
			w.row.transport, w.row.ipKey, w.row.netKey, w.row.build, w.row.userAgent, w.row.screen)
		return err
	}
	_, err := j.db.Exec(`UPDATE connections SET ended=? WHERE session=? AND ended=0`, w.at, w.session)
	return err
}

/*
================
checkpoint

Open rows advance their seen mark (the crash recovery's estimate), and
rows closed longer than the retention are pruned. Open rows are never
pruned: a session still entered is still evidence.
================
*/
func (j *Journal) checkpoint() error {
	now := millis(j.now())
	if _, err := j.db.Exec(`UPDATE connections SET seen=? WHERE ended=0`, now); err != nil {
		return err
	}
	cutoff := now - int64(retentionDays)*24*int64(time.Hour/time.Millisecond)
	_, err := j.db.Exec(`DELETE FROM connections WHERE ended!=0 AND ended<?`, cutoff)
	return err
}

/*
================
Close

Drains the queue, checkpoints once, and closes the database.
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

The counts an operator read can show: records dropped at a full queue,
records the writer failed to store, and the rows currently open.
================
*/
func (j *Journal) Health() map[string]any {
	var open int64
	err := j.db.QueryRow(`SELECT count(*) FROM connections WHERE ended=0`).Scan(&open)
	health := map[string]any{"dropped": j.dropped.Load(), "failed": j.failed.Load(), "open": open,
		"shard": j.shard, "retentionDays": retentionDays}
	if err != nil {
		health["lastError"] = err.Error()
	}
	return health
}

/*
================
keyHashes

The two keyed hashes of one client address: the address itself and
its network, /24 for IPv4 and /48 for IPv6. An unkeyed hash of an IPv4
address falls to a brute force of the address space, so both are
HMACs under the shard's key, domain-separated so an address hash can
never be mistaken for a network hash. An address that does not parse
(a test's stand-in peer) hashes to nothing rather than to a guess.
================
*/
func keyHashes(key []byte, address string) (ipKey string, netKey string) {
	host := address
	if parsed, _, err := net.SplitHostPort(address); err == nil {
		host = parsed
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", ""
	}
	ipKey = keyedHash(key, "ip", ip.String())
	if v4 := ip.To4(); v4 != nil {
		netKey = keyedHash(key, "net", v4.Mask(net.CIDRMask(24, 32)).String())
	} else {
		netKey = keyedHash(key, "net", ip.Mask(net.CIDRMask(48, 128)).String())
	}
	return ipKey, netKey
}

/*
================
keyedHash
================
*/
func keyedHash(key []byte, kind, value string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(kind + ":" + value))
	return hex.EncodeToString(mac.Sum(nil))
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
Enabled

Whether SRO_CONNECTION_JOURNAL turns the census on (the kill-stats
flag's spelling: on, 1, true). Off is the native posture.
================
*/
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvConnectionJournal))) {
	case "on", "1", "true":
		return true
	}
	return false
}
