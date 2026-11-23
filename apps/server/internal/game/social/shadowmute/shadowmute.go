/*
===========================================================================

shadowmute.go - the staff's chat shadow-mute lists

Port-only, not native: a moderation tool the original does not have. A
shadow-muted line still reaches its author, who sees it and its
acknowledgement as ever, but nobody else receives it: not the public
channel, not its history or the website feed, not whisper, party,
guild, union or stall peers. The player cannot tell.

Two private files in the shard's authority directory decide it, and a
line is muted when either does:

	chat-shadow-mutes.txt       one account id per line, ASCII case
	                            ignored as logins ignore it
	chat-shadow-mute-words.txt  one word or phrase per line; a line whose
	                            text contains one is muted

Both skip blank lines and # comments. The word list is matched on folded
text: case is ignored and everything but letters and digits is dropped
on both sides, so spacing, dots and capitals do not slip past an entry.
An entry shorter than minWordRunes after folding is ignored with a
warning, so a stray one-letter line cannot silence every player. The
words themselves are staff data: they live in the private operations
repository and are installed on the host, never in this tree.

Staff edit the files on the host; the GameWorld re-reads them when they
change, at most once per recheckInterval, so a change needs no restart.
A missing file is an empty list, which is native behaviour, so edit a
file in place (or write a copy and rename it over): deleting one lifts
every mute it held.

Muted never touches the files: it reads the current snapshot, and when
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
	"unicode"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
)

// FileName and WordsFileName are the lists' names inside the shard's
// authority directory.
const (
	FileName      = "chat-shadow-mutes.txt"
	WordsFileName = "chat-shadow-mute-words.txt"
)

// recheckInterval bounds how often the files are stat'ed.
const recheckInterval = 5 * time.Second

// minWordRunes is the shortest folded word entry obeyed: a shorter one
// would match nearly every line.
const minWordRunes = 3

/*
================
List
================
*/
type List struct {
	accounts listFile
	words    listFile
	now      func() time.Time

	// checkedAt (Unix nanoseconds) is when the files were last looked at;
	// refreshing single-flights the background refresh.
	checkedAt  atomic.Int64
	refreshing atomic.Bool
}

/*
================
listFile

One watched list file and the snapshot Muted reads from it.
================
*/
type listFile struct {
	path  string
	parse func(text string) map[string]bool

	entries atomic.Pointer[map[string]bool]

	// mu serializes refreshes and guards the file identity below.
	mu      sync.Mutex
	modTime time.Time
	size    int64
}

/*
================
Open

The lists kept in dir, read once now so the first chat line after boot
already obeys them.
================
*/
func Open(dir string) *List {
	list := &List{
		accounts: listFile{path: filepath.Join(dir, FileName), parse: parseAccounts},
		words:    listFile{path: filepath.Join(dir, WordsFileName), parse: parseWords},
		now:      time.Now,
	}
	list.refresh()
	return list
}

/*
================
Muted

Whether a line from the account with this text is shadow-muted, from the
current snapshot: the account is listed, or the text contains a listed
word. A nil list mutes nothing.
================
*/
func (list *List) Muted(accountID, text string) bool {
	if list == nil {
		return false
	}
	if list.now().UnixNano()-list.checkedAt.Load() >= int64(recheckInterval) && list.refreshing.CompareAndSwap(false, true) {
		go func() {
			defer list.refreshing.Store(false)
			list.refresh()
		}()
	}
	accounts := list.accounts.entries.Load()
	if accountID != "" && accounts != nil && (*accounts)[domain.FoldAccountID(accountID)] {
		return true
	}
	words := list.words.entries.Load()
	if text == "" || words == nil || len(*words) == 0 {
		return false
	}
	folded := foldText(text)
	for word := range *words {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}

/*
================
refresh
================
*/
func (list *List) refresh() {
	list.checkedAt.Store(list.now().UnixNano())
	list.accounts.refresh()
	list.words.refresh()
}

/*
================
listFile.refresh

Re-reads the file when its size or modification time moved. A read error
keeps the last good list: a moderation decision must not lapse because
the file was briefly unreadable mid-edit. A missing file empties it.
================
*/
func (file *listFile) refresh() {
	file.mu.Lock()
	defer file.mu.Unlock()
	info, err := os.Stat(file.path)
	if os.IsNotExist(err) {
		file.entries.Store(nil)
		file.modTime, file.size = time.Time{}, 0
		return
	}
	if err != nil || (info.ModTime().Equal(file.modTime) && info.Size() == file.size && file.entries.Load() != nil) {
		return
	}
	data, err := os.ReadFile(file.path)
	if err != nil {
		log.WithError(err).WithField("file", filepath.Base(file.path)).Warn("shadowmute: list unreadable; keeping the previous one")
		return
	}
	entries := file.parse(string(data))
	file.entries.Store(&entries)
	file.modTime, file.size = info.ModTime(), info.Size()
	log.WithFields(log.Fields{"file": filepath.Base(file.path), "entries": len(entries)}).Info("shadowmute: list loaded")
}

/*
================
lines

A list's meaningful lines: a leading byte-order mark (an editor's UTF-8
save) is not part of the first one, and blank lines and # comments are
skipped.
================
*/
func lines(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

/*
================
parseAccounts
================
*/
func parseAccounts(text string) map[string]bool {
	accounts := map[string]bool{}
	for _, line := range lines(text) {
		accounts[domain.FoldAccountID(line)] = true
	}
	return accounts
}

/*
================
parseWords

Each entry folded as chat text is folded. A line that folds to fewer
than minWordRunes is skipped and logged rather than obeyed.
================
*/
func parseWords(text string) map[string]bool {
	words := map[string]bool{}
	skipped := 0
	for _, line := range lines(text) {
		word := foldText(line)
		if utf8.RuneCountInString(word) < minWordRunes {
			skipped++
			continue
		}
		words[word] = true
	}
	if skipped > 0 {
		log.WithField("skipped", skipped).Warn("shadowmute: word entries too short after folding were ignored")
	}
	return words
}

/*
================
foldText

Lower case, letters and digits only: "A.b C" and "abc" fold alike.
================
*/
func foldText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
