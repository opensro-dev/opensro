/*
===========================================================================

shadowmute.go - the staff's chat shadow-mute list

Port-only, not native: a moderation tool the original does not have. An
account on the list still chats and sees its own lines, but nobody else
receives them: not the public channel, not its history or the website
feed, not whisper, party, guild, union or stall peers. The sender's
acknowledgement is unchanged, so the player cannot tell.

The list is a private file in the shard's authority directory
(chat-shadow-mutes.txt): one account id per line, ASCII case ignored as
logins ignore it, blank lines and # comments skipped. Staff edit it on
the host; the GameWorld re-reads it when it changes, at most once per
recheckInterval, so a change needs no restart. A missing file is an
empty list, which is native behaviour, so edit the file in place (or
write a copy and rename it over): deleting it lifts every mute.

Muted never touches the file: it reads the current snapshot, and when
that is stale it starts one background refresh. The Global Chatting item
asks from inside a store door, where file I/O must not run.

===========================================================================
*/
package shadowmute

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
)

// FileName is the list's name inside the shard's authority directory.
const FileName = "chat-shadow-mutes.txt"

// recheckInterval bounds how often the file is stat'ed.
const recheckInterval = 5 * time.Second

/*
================
List
================
*/
type List struct {
	path string
	now  func() time.Time

	// accounts is the snapshot Muted reads; checkedAt (Unix nanoseconds)
	// is when the file was last looked at; refreshing single-flights the
	// background refresh.
	accounts   atomic.Pointer[map[string]bool]
	checkedAt  atomic.Int64
	refreshing atomic.Bool

	// mu serializes refreshes and guards the file identity below.
	mu      sync.Mutex
	modTime time.Time
	size    int64
}

/*
================
Open

The list kept in dir, read once now so the first chat line after boot
already obeys it.
================
*/
func Open(dir string) *List {
	list := &List{path: filepath.Join(dir, FileName), now: time.Now}
	list.refresh()
	return list
}

/*
================
Muted

Whether the account's chat is shadow-muted, from the current snapshot. A
nil list mutes no one.
================
*/
func (list *List) Muted(accountID string) bool {
	if list == nil || accountID == "" {
		return false
	}
	if list.now().UnixNano()-list.checkedAt.Load() >= int64(recheckInterval) && list.refreshing.CompareAndSwap(false, true) {
		go func() {
			defer list.refreshing.Store(false)
			list.refresh()
		}()
	}
	accounts := list.accounts.Load()
	return accounts != nil && (*accounts)[domain.FoldAccountID(accountID)]
}

/*
================
refresh

Re-reads the file when its size or modification time moved. A read error
keeps the last good list: a moderation decision must not lapse because
the file was briefly unreadable mid-edit. A missing file empties it.
================
*/
func (list *List) refresh() {
	list.mu.Lock()
	defer list.mu.Unlock()
	list.checkedAt.Store(list.now().UnixNano())
	info, err := os.Stat(list.path)
	if os.IsNotExist(err) {
		list.accounts.Store(nil)
		list.modTime, list.size = time.Time{}, 0
		return
	}
	if err != nil || (info.ModTime().Equal(list.modTime) && info.Size() == list.size && list.accounts.Load() != nil) {
		return
	}
	data, err := os.ReadFile(list.path)
	if err != nil {
		log.WithError(err).Warn("shadowmute: list unreadable; keeping the previous one")
		return
	}
	accounts := parse(string(data))
	list.accounts.Store(&accounts)
	list.modTime, list.size = info.ModTime(), info.Size()
	log.WithField("accounts", len(accounts)).Info("shadowmute: list loaded")
}

/*
================
parse

One account per line; a leading byte-order mark (an editor's UTF-8
save) is not part of the first id.
================
*/
func parse(text string) map[string]bool {
	accounts := map[string]bool{}
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		accounts[domain.FoldAccountID(line)] = true
	}
	return accounts
}
