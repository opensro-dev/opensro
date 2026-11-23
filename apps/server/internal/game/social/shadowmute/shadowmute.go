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

Add and Remove are the GM's /SHADOWMUTE and /UNSHADOWMUTE (gmcommand):
they edit the same file in place, keeping every line staff wrote by hand,
and swap the new snapshot in at once so the next chat line obeys it.

===========================================================================
*/
package shadowmute

import (
	"errors"
	"fmt"
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

// notePrefix opens the comment line Add writes above an account, so
// Remove can take that line away with the account.
const notePrefix = "# shadowmute "

// byteOrderMark is the UTF-8 mark an editor may save ahead of the first line.
const byteOrderMark = string(rune(0xFEFF))

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

	// write is the durable file writer Add and Remove publish through.
	write func(path string, payload []byte) error

	// mu serializes refreshes and edits and guards the file identity below.
	mu      sync.Mutex
	modTime time.Time
	size    int64
}

/*
================
Open

The list kept in dir, read once now so the first chat line after boot
already obeys it. write is the durable atomic writer Add and Remove
publish through (store.WriteFileAtomic in the server).
================
*/
func Open(dir string, write func(path string, payload []byte) error) *List {
	list := &List{path: filepath.Join(dir, FileName), now: time.Now, write: write}
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
	list.refreshLocked()
}

/*
================
refreshLocked
================
*/
func (list *List) refreshLocked() {
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

/*
================
Add

Puts the account on the list under a comment line naming who did it and
why. An account already on it is left alone and reports false. The file
is re-read first, so a hand edit made a moment ago is never lost.
================
*/
func (list *List) Add(accountID, note string) (bool, error) {
	folded := domain.FoldAccountID(strings.TrimSpace(accountID))
	if list == nil || folded == "" || strings.ContainsAny(folded, "\r\n#") {
		return false, errors.New("shadowmute: account id required")
	}
	list.mu.Lock()
	defer list.mu.Unlock()
	text, err := list.readLocked()
	if err != nil {
		return false, err
	}
	if parse(text)[folded] {
		return false, nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	// One line: a typed note can never start a new account line.
	note = strings.Join(strings.Fields(note), " ")
	text += notePrefix + folded + ": " + note + "\n" + folded + "\n"
	return true, list.publishLocked(text)
}

/*
================
Remove

Takes every line naming the account off the list, with the comment line
Add wrote for it. Lines staff wrote by hand stay. An account not on the
list reports false.
================
*/
func (list *List) Remove(accountID string) (bool, error) {
	folded := domain.FoldAccountID(strings.TrimSpace(accountID))
	if list == nil || folded == "" {
		return false, errors.New("shadowmute: account id required")
	}
	list.mu.Lock()
	defer list.mu.Unlock()
	text, err := list.readLocked()
	if err != nil {
		return false, err
	}
	if !parse(text)[folded] {
		return false, nil
	}
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, byteOrderMark))
		if domain.FoldAccountID(trimmed) == folded || strings.HasPrefix(trimmed, notePrefix+folded+":") {
			continue
		}
		kept = append(kept, line)
	}
	return true, list.publishLocked(strings.Join(kept, "\n"))
}

/*
================
readLocked

The file as it stands; a missing file is an empty list.
================
*/
func (list *List) readLocked() (string, error) {
	data, err := os.ReadFile(list.path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("shadowmute: read %s: %w", list.path, err)
	}
	return string(data), nil
}

/*
================
publishLocked

Writes the new text and loads it at once: the snapshot never lags a
change the GM was told succeeded.
================
*/
func (list *List) publishLocked(text string) error {
	if err := list.write(list.path, []byte(text)); err != nil {
		return fmt.Errorf("shadowmute: save %s: %w", list.path, err)
	}
	list.modTime, list.size = time.Time{}, -1
	list.refreshLocked()
	return nil
}
