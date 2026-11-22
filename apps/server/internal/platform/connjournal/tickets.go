/*
===========================================================================

tickets.go - world tickets per account per hour (#3307)

Every world connection costs a transport admission ticket and every
world entry an EnterWorld ticket (agent/api/ticket_pace.go). A client
that loops - mint, connect, drop, mint again - showed in the 11-12 live
investigation only as a guess joined from three stores. This keeps the
fact: per account and UTC hour, the tickets minted and the mints the
pacer refused, of each kind.

Counting is in memory and costs one map write; the journal's writer
flushes the hour buckets in one transaction at each checkpoint, so a
mint never waits on disk. A crash loses at most one checkpoint period
(15 s) of counts - a counter, not evidence a session row depends on.
The buckets held between flushes are bounded; past the bound a count is
dropped and counted in Health, never a stall.

Port-only, not native, behind SRO_CONNECTION_JOURNAL with the rest of
the census: the account names stay where the census keeps its people.

===========================================================================
*/
package connjournal

import (
	"database/sql"
	"sort"
	"time"
)

// ticketHourMs is a bucket's width: one UTC hour.
const ticketHourMs = int64(time.Hour / time.Millisecond)

// maxTicketBuckets bounds the account-hours held between two flushes:
// one checkpoint period of the busiest hour, with room.
const maxTicketBuckets = 8192

// ticketSchema is the bucket table, created with the connections table.
const ticketSchema = `
CREATE TABLE IF NOT EXISTS ticket_hours (
 hour_ms INTEGER NOT NULL, account TEXT NOT NULL,
 transport_minted INTEGER NOT NULL DEFAULT 0, transport_paced INTEGER NOT NULL DEFAULT 0,
 enterworld_minted INTEGER NOT NULL DEFAULT 0, enterworld_paced INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY (hour_ms, account));`

/*
================
TicketCounts

What one account was handed in one hour: tickets minted, and mints the
pacer refused (paced), of each kind. RecordTicket takes it as an
increment.
================
*/
type TicketCounts struct {
	TransportMinted  uint32 `json:"transportMinted"`
	TransportPaced   uint32 `json:"transportPaced"`
	EnterWorldMinted uint32 `json:"enterWorldMinted"`
	EnterWorldPaced  uint32 `json:"enterWorldPaced"`
}

// add folds an increment into the counts.
func (c *TicketCounts) add(more TicketCounts) {
	c.TransportMinted += more.TransportMinted
	c.TransportPaced += more.TransportPaced
	c.EnterWorldMinted += more.EnterWorldMinted
	c.EnterWorldPaced += more.EnterWorldPaced
}

/*
================
TicketHour

One account's bucket as a read returns it.
================
*/
type TicketHour struct {
	HourMs int64 `json:"hourMs"`
	TicketCounts
}

// ticketKey names one bucket.
type ticketKey struct {
	hourMs  int64
	account string
}

/*
================
RecordTicket

Counts an account's ticket outcome in the current hour. Safe from any
goroutine; never touches disk.
================
*/
func (j *Journal) RecordTicket(account string, counts TicketCounts) bool {
	if j == nil || account == "" {
		return false
	}
	now := millis(j.now())
	key := ticketKey{hourMs: now - now%ticketHourMs, account: account}
	j.ticketMu.Lock()
	defer j.ticketMu.Unlock()
	bucket, held := j.tickets[key]
	if !held && len(j.tickets) >= maxTicketBuckets {
		j.ticketsDropped.Add(1)
		return false
	}
	bucket.add(counts)
	j.tickets[key] = bucket
	return true
}

/*
================
flushTickets

The writer's half: takes the buckets counted since the last flush and
adds them to the table in one transaction. A failed write puts them
back, so the next checkpoint retries instead of losing them.
================
*/
func (j *Journal) flushTickets() error {
	j.ticketMu.Lock()
	pending := j.tickets
	j.tickets = make(map[ticketKey]TicketCounts, len(pending))
	j.ticketMu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	if err := writeTicketBuckets(j.db, pending); err != nil {
		j.ticketMu.Lock()
		for key, counts := range pending {
			bucket := j.tickets[key]
			bucket.add(counts)
			j.tickets[key] = bucket
		}
		j.ticketMu.Unlock()
		return err
	}
	return nil
}

/*
================
writeTicketBuckets
================
*/
func writeTicketBuckets(db *sql.DB, pending map[ticketKey]TicketCounts) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for key, counts := range pending {
		if _, err := tx.Exec(`INSERT INTO ticket_hours
 (hour_ms, account, transport_minted, transport_paced, enterworld_minted, enterworld_paced)
 VALUES (?,?,?,?,?,?)
 ON CONFLICT(hour_ms, account) DO UPDATE SET
 transport_minted = transport_minted + excluded.transport_minted,
 transport_paced = transport_paced + excluded.transport_paced,
 enterworld_minted = enterworld_minted + excluded.enterworld_minted,
 enterworld_paced = enterworld_paced + excluded.enterworld_paced`,
			key.hourMs, key.account, counts.TransportMinted, counts.TransportPaced,
			counts.EnterWorldMinted, counts.EnterWorldPaced); err != nil {
			return err
		}
	}
	return tx.Commit()
}

/*
================
TicketHours

The account's buckets from sinceMs on, oldest first: the flushed hours,
with the counts still waiting for the next flush folded in, so a read
never trails the pacer by a checkpoint.
================
*/
func (j *Journal) TicketHours(account string, sinceMs int64) ([]TicketHour, error) {
	if j == nil {
		return nil, nil
	}
	rows, err := j.db.Query(`SELECT hour_ms, transport_minted, transport_paced, enterworld_minted, enterworld_paced
 FROM ticket_hours WHERE account = ? AND hour_ms >= ?`, account, sinceMs-sinceMs%ticketHourMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byHour := map[int64]TicketCounts{}
	for rows.Next() {
		var hour int64
		var counts TicketCounts
		if err := rows.Scan(&hour, &counts.TransportMinted, &counts.TransportPaced,
			&counts.EnterWorldMinted, &counts.EnterWorldPaced); err != nil {
			return nil, err
		}
		byHour[hour] = counts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	j.ticketMu.Lock()
	for key, counts := range j.tickets {
		if key.account == account && key.hourMs+ticketHourMs > sinceMs {
			bucket := byHour[key.hourMs]
			bucket.add(counts)
			byHour[key.hourMs] = bucket
		}
	}
	j.ticketMu.Unlock()
	out := make([]TicketHour, 0, len(byHour))
	for hour, counts := range byHour {
		out = append(out, TicketHour{HourMs: hour, TicketCounts: counts})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].HourMs < out[b].HourMs })
	return out, nil
}
