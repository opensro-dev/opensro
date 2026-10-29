/*
===========================================================================

chatlog.go - the staff-only moderation chat log

Port-only, not native (the original keeps chat in its own database logs;
this is operator tooling, server-side only): every accepted public chat
line is appended to a daily file in the shard's authority directory -
UTC time, account id, character name, channel, and text, with
shadow-muted lines included and marked, so staff can review what a
player said after the in-memory transcript is gone.

The disk is owned by one writer goroutine. Record only hands an entry
to a bounded queue and never blocks, so a slow or full disk cannot stall
the tick, a session handler, or a store door, and never does file I/O
itself. Lines that cannot be queued or written are dropped and counted
(Dropped); the log is evidence, not a delivery channel, so losing a
line beats delaying the game.

Files are named chat-2006-01-02.log (UTC) and pruned past the retention
window (SRO_CHAT_LOG_RETAIN_DAYS, default 30). The directory is the
shard's authority directory, which only operators can read; nothing
here is reachable from the public site or the public API.

===========================================================================
*/
package chatlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// File names are FilePrefix + a UTC date + FileSuffix in the shard dir.
const (
	FilePrefix = "chat-"
	FileSuffix = ".log"
	dateLayout = "2006-01-02"
)

// RetainDaysEnv names the retention override; DefaultRetainDays applies
// when it is unset or unreadable.
const (
	RetainDaysEnv     = "SRO_CHAT_LOG_RETAIN_DAYS"
	DefaultRetainDays = 30
)

const (
	// queueSize bounds the writer's backlog; past it Record drops.
	queueSize = 1024
	// flushInterval bounds how long a line waits for the disk.
	flushInterval = 500 * time.Millisecond
	writeBuffer   = 32 * 1024
)

/*
================
Entry

One accepted chat line. Target is the whisper's recipient (the only
targeted lane); empty on the broadcast lanes.
================
*/
type Entry struct {
	At        time.Time
	AccountID string
	Character string
	Channel   string
	Target    string
	Muted     bool
	Text      string
}

// line is Entry's on-disk JSON shape, one object per line.
type line struct {
	TS        string `json:"ts"`
	Account   string `json:"account"`
	Character string `json:"character"`
	Channel   string `json:"channel"`
	Target    string `json:"target,omitempty"`
	Muted     bool   `json:"muted"`
	Text      string `json:"text"`
}

/*
================
Log

The moderation log for one shard directory. Record is safe from any
goroutine and on a nil Log (which records nothing).
================
*/
type Log struct {
	dir        string
	retainDays int
	now        func() time.Time

	queue   chan Entry
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
	dropped atomic.Int64
}

/*
================
Open

A log writing into dir, its writer already running and the retention
sweep already queued. A non-positive retainDays keeps the default.
================
*/
func Open(dir string, retainDays int) *Log {
	if retainDays < 1 {
		retainDays = DefaultRetainDays
	}
	l := &Log{
		dir:        dir,
		retainDays: retainDays,
		now:        time.Now,
		queue:      make(chan Entry, queueSize),
		done:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	go l.run()
	return l
}

/*
================
RetainDaysFromEnv

SRO_CHAT_LOG_RETAIN_DAYS, or the default when it is unset, unreadable
or not positive.
================
*/
func RetainDaysFromEnv() int {
	raw := strings.TrimSpace(os.Getenv(RetainDaysEnv))
	if raw == "" {
		return DefaultRetainDays
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 {
		log.WithField("value", raw).Warn("chatlog: " + RetainDaysEnv + " unreadable; keeping the default retention")
		return DefaultRetainDays
	}
	return days
}

/*
================
Record

Hand one line to the writer. Never blocks and never touches the disk,
so it is safe inside a store door: a caller whose line finds the queue
full drops it (counted), it does not wait. The entry is stamped from
the log's clock when the caller left At zero.
================
*/
func (l *Log) Record(entry Entry) {
	if l == nil {
		return
	}
	if entry.At.IsZero() {
		entry.At = l.now()
	}
	select {
	case l.queue <- entry:
	default:
		l.dropped.Add(1)
	}
}

/*
================
Dropped

Lines the log has lost (full queue or write failure) since it opened.
================
*/
func (l *Log) Dropped() int64 {
	if l == nil {
		return 0
	}
	return l.dropped.Load()
}

/*
================
Close

Stop the writer after it has flushed everything still queued. Safe to
call more than once; later Records still drop silently.
================
*/
func (l *Log) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() { close(l.done) })
	<-l.stopped
}

/*
================
fileName

The daily file's name for one UTC day.
================
*/
func fileName(day time.Time) string {
	return FilePrefix + day.UTC().Format(dateLayout) + FileSuffix
}

/*
================
run

The writer loop, the only goroutine that touches the disk: append each
entry to its UTC day's file (rotating at the date change), flush on the
ticker, and prune expired files at start and at every rotation. A write
failure warns (throttled), drops the line, and forgets the file so the
next entry retries the open - the loop never stops for the disk.
================
*/
func (l *Log) run() {
	defer close(l.stopped)
	var (
		file *os.File
		out  *bufio.Writer
		day  string
		errs int64
	)
	closeFile := func() {
		if out != nil {
			_ = out.Flush()
		}
		if file != nil {
			_ = file.Close()
		}
		file, out, day = nil, nil, ""
	}
	defer closeFile()
	warn := func(err error, what string) {
		errs++
		l.dropped.Add(1)
		if errs == 1 || errs%100 == 0 {
			log.WithError(err).WithField("dropped", l.dropped.Load()).Warn("chatlog: " + what + "; lines are being dropped")
		}
	}
	l.prune()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	write := func(entry Entry) {
		want := entry.At.UTC().Format(dateLayout)
		if want != day {
			closeFile()
			f, err := os.OpenFile(filepath.Join(l.dir, fileName(entry.At)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				warn(err, "cannot open the daily file")
				return
			}
			file, out, day = f, bufio.NewWriterSize(f, writeBuffer), want
			l.prune()
		}
		data, err := json.Marshal(line{
			TS:        entry.At.UTC().Format(time.RFC3339),
			Account:   entry.AccountID,
			Character: entry.Character,
			Channel:   entry.Channel,
			Target:    entry.Target,
			Muted:     entry.Muted,
			Text:      entry.Text,
		})
		if err == nil {
			_, err = out.Write(append(data, '\n'))
		}
		if err != nil {
			warn(err, "cannot write to the daily file")
			closeFile()
		}
	}
	for {
		select {
		case entry := <-l.queue:
			write(entry)
		case <-ticker.C:
			if out != nil {
				if err := out.Flush(); err != nil {
					warn(err, "cannot flush the daily file")
					closeFile()
				}
			}
		case <-l.done:
			for {
				select {
				case entry := <-l.queue:
					write(entry)
				default:
					return
				}
			}
		}
	}
}

/*
================
prune

Remove daily files whose UTC date is outside the retention window: at
most retainDays daily files survive, today's included. Foreign files -
including the shadow-mute list beside the log - are never touched, and
a file whose name does not parse as one of ours is left alone.
================
*/
func (l *Log) prune() {
	names, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	cutoff := l.now().UTC().AddDate(0, 0, -(l.retainDays - 1)).Format(dateLayout)
	for _, name := range names {
		file := name.Name()
		if name.IsDir() || !strings.HasPrefix(file, FilePrefix) || !strings.HasSuffix(file, FileSuffix) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(file, FilePrefix), FileSuffix)
		if _, err := time.Parse(dateLayout, day); err != nil {
			continue
		}
		if day < cutoff {
			_ = os.Remove(filepath.Join(l.dir, file))
		}
	}
}
