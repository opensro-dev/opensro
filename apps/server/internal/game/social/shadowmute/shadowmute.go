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
empty list, which is native behaviour.

===========================================================================
*/
package shadowmute

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
)

// FileName is the list's name inside the shard's authority directory.
const FileName = "chat-shadow-mutes.txt"

// recheckInterval bounds how often a chat line stats the file.
const recheckInterval = 5 * time.Second

/*
================
List
================
*/
type List struct {
	path string
	now  func() time.Time

	mu        sync.Mutex
	accounts  map[string]bool
	modTime   time.Time
	size      int64
	checkedAt time.Time
}

/*
================
Open

The list kept in dir. Nothing is read until the first Muted.
================
*/
func Open(dir string) *List {
	return &List{path: filepath.Join(dir, FileName), now: time.Now}
}

/*
================
Muted

Whether the account's chat is shadow-muted. A nil list mutes no one.
================
*/
func (list *List) Muted(accountID string) bool {
	if list == nil || accountID == "" {
		return false
	}
	list.mu.Lock()
	defer list.mu.Unlock()
	list.refreshLocked()
	return list.accounts[domain.FoldAccountID(accountID)]
}

/*
================
refreshLocked

Re-reads the file when its size or modification time moved. A read error
keeps the last good list: a moderation decision must not lapse because
the file was briefly unreadable mid-edit.
================
*/
func (list *List) refreshLocked() {
	now := list.now()
	if !list.checkedAt.IsZero() && now.Sub(list.checkedAt) < recheckInterval {
		return
	}
	list.checkedAt = now
	info, err := os.Stat(list.path)
	if os.IsNotExist(err) {
		list.accounts, list.modTime, list.size = nil, time.Time{}, 0
		return
	}
	if err != nil || (info.ModTime().Equal(list.modTime) && info.Size() == list.size && list.accounts != nil) {
		return
	}
	data, err := os.ReadFile(list.path)
	if err != nil {
		log.WithError(err).Warn("shadowmute: list unreadable; keeping the previous one")
		return
	}
	list.accounts = parse(string(data))
	list.modTime, list.size = info.ModTime(), info.Size()
	log.WithField("accounts", len(list.accounts)).Info("shadowmute: list loaded")
}

/*
================
parse
================
*/
func parse(text string) map[string]bool {
	accounts := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		accounts[domain.FoldAccountID(line)] = true
	}
	return accounts
}
