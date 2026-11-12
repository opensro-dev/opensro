/*
===========================================================================

contributor.go - the Contributor title (port-only, not native, #3358)

Owner, 2026-11-12: every Discord member holding the Contributor role
carries a yellow "Contributor" title in game, seen the same way by every
viewer. The v1.150 client has no title of its own to reuse (no Hwan or
honor title over a name; the textuisystem UIO_SECOND_HWAN rows are data
for later builds only), so the title is this port's own frame and the
browser client draws it.

Two files in the shard's authority directory, one owner each:

  - contributors.json, {"accounts":["<account id>", ...]}, written by
    the operations sync (Discord role members mapped to their linked
    game accounts). This package only reads it, re-reading when its
    size or modification time changes. A missing file is an empty
    set; a corrupt one keeps the last good set and logs.
  - contributor-titles.json, {"hidden":["<account id>", ...]}, owned
    here: a contributor's own "/contributor off" choice. It is a server
    setting, so every viewer sees the same state.

Gated by SRO_CONTRIBUTOR_TITLES; off (the default, native) builds no
registry, sends no frame and leaves "/contributor" ordinary chat text.

===========================================================================
*/
package contributor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// EnvContributorTitles turns the title on ("on", "1", "true").
	EnvContributorTitles = "SRO_CONTRIBUTOR_TITLES"
	// MembersFileName is the operations sync's file (read only here).
	MembersFileName = "contributors.json"
	// HiddenFileName is this package's opt-out file.
	HiddenFileName = "contributor-titles.json"
	// maxMembersFileBytes bounds a read of the sync's file; a Discord
	// role of a few thousand ids stays far below it.
	maxMembersFileBytes = 1 << 20
	// maxAccountIDBytes bounds one id in either file.
	maxAccountIDBytes = 128
	// reloadIntervalMs paces the stat of the sync's file: the sync runs
	// every few minutes, so a minute of lag is invisible.
	reloadIntervalMs = 60_000
	// pushIntervalMs paces the roster pass, the caravan star's pace: a
	// new arrival learns the roster within two seconds of entering.
	pushIntervalMs = 2000
	// Command is the chat word a contributor types.
	Command = "/contributor"
)

// The system lines answering the chat command.
const (
	answerNotMember = "Only members with the Contributor role on Discord carry the Contributor title."
	answerShown     = "Your Contributor title is shown. Type /contributor off to hide it."
	answerHidden    = "Your Contributor title is hidden. Type /contributor on to show it."
	answerUsage     = "Usage: /contributor on, or /contributor off."
	answerBusy      = "The title setting could not be saved; please try again in a moment."
)

/*
================
Enabled

The shard's word that the title exists (SRO_CONTRIBUTOR_TITLES).
================
*/
func Enabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvContributorTitles))) {
	case "on", "1", "true":
		return true
	}
	return false
}

// fileStamp is what decides that the sync's file changed.
type fileStamp struct {
	size int64
	mod  time.Time
}

/*
================
Registry

Who is a contributor, who hid the title, and what roster each session
last received. One mutex: the chat handler and the tick hook are the
only callers, and each holds it for a map lookup or a short pass.
================
*/
type Registry struct {
	mu          sync.Mutex
	membersPath string
	hiddenPath  string
	write       func(string, []byte) error
	members     map[string]bool
	hidden      map[string]bool
	stamp       fileStamp
	reloadDueMs int64
	pushDueMs   int64
	// roster is the payload every session in sent holds.
	roster []byte
	sent   map[string]bool
}

/*
================
Open

Reads both files from dir. A corrupt opt-out file stops the boot, the
mute book's posture: silently showing titles people hid is worse than a
loud refusal. The sync's file never stops it (Reload's rules).
================
*/
func Open(dir string, write func(string, []byte) error) (*Registry, error) {
	r := &Registry{
		membersPath: filepath.Join(dir, MembersFileName),
		hiddenPath:  filepath.Join(dir, HiddenFileName),
		write:       write,
		members:     map[string]bool{},
		hidden:      map[string]bool{},
		sent:        map[string]bool{},
	}
	hidden, err := readHidden(r.hiddenPath)
	if err != nil {
		return nil, err
	}
	r.hidden = hidden
	r.reloadLocked()
	return r, nil
}

/*
================
readHidden
================
*/
func readHidden(path string) (map[string]bool, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("contributor titles: %w", err)
	}
	var file struct {
		Hidden []string `json:"hidden"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, fmt.Errorf("contributor titles: %s: %w", path, err)
	}
	return accountSet(file.Hidden), nil
}

/*
================
accountSet

The non-empty, bounded ids of a file as a set.
================
*/
func accountSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && len(id) <= maxAccountIDBytes {
			set[id] = true
		}
	}
	return set
}

/*
================
reloadLocked

Re-reads the sync's file when its stamp moved. Missing means nobody
(the role emptied or the sync never ran); unreadable or corrupt keeps
the last good set, since a half-written or broken file must not strip
every title at once.
================
*/
func (r *Registry) reloadLocked() {
	info, err := os.Stat(r.membersPath)
	if errors.Is(err, os.ErrNotExist) {
		r.members, r.stamp = map[string]bool{}, fileStamp{}
		return
	}
	if err != nil {
		log.WithError(err).Warn("contributor titles: members file unreadable; keeping the last set")
		return
	}
	stamp := fileStamp{size: info.Size(), mod: info.ModTime()}
	if stamp == r.stamp {
		return
	}
	if info.Size() > maxMembersFileBytes {
		log.Warnf("contributor titles: members file is %d bytes (limit %d); keeping the last set", info.Size(), maxMembersFileBytes)
		return
	}
	body, err := os.ReadFile(r.membersPath)
	if err != nil {
		log.WithError(err).Warn("contributor titles: members file unreadable; keeping the last set")
		return
	}
	var file struct {
		Accounts []string `json:"accounts"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		log.WithError(err).Warn("contributor titles: members file corrupt; keeping the last set")
		return
	}
	r.members, r.stamp = accountSet(file.Accounts), stamp
	log.Infof("contributor titles: %d contributor accounts", len(r.members))
}

/*
================
Shown

Whether the account's characters carry the title now.
================
*/
func (r *Registry) Shown(accountID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.members[accountID] && !r.hidden[accountID]
}

/*
================
Command

The chat intake: "/contributor", "/contributor on" or "/contributor off"
from the speaker's account. handled is false for any other line, which
stays ordinary chat. The choice is saved before it is reported, and a
failed save leaves the old choice in force.
================
*/
func (r *Registry) Command(accountID, message string) (answer string, handled bool) {
	fields := strings.Fields(strings.ToLower(message))
	if len(fields) == 0 || fields[0] != Command {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if accountID == "" || !r.members[accountID] {
		return answerNotMember, true
	}
	if len(fields) == 1 {
		if r.hidden[accountID] {
			return answerHidden, true
		}
		return answerShown, true
	}
	if len(fields) != 2 || (fields[1] != "on" && fields[1] != "off") {
		return answerUsage, true
	}
	hide := fields[1] == "off"
	if r.hidden[accountID] == hide {
		if hide {
			return answerHidden, true
		}
		return answerShown, true
	}
	next := make(map[string]bool, len(r.hidden)+1)
	for id := range r.hidden {
		next[id] = true
	}
	if hide {
		next[accountID] = true
	} else {
		delete(next, accountID)
	}
	if err := r.saveLocked(next); err != nil {
		log.WithError(err).Warn("contributor titles: opt-out save failed")
		return answerBusy, true
	}
	r.hidden = next
	// The next roster pass sends the changed roster to everyone.
	r.pushDueMs = 0
	if hide {
		return answerHidden, true
	}
	return answerShown, true
}

/*
================
saveLocked
================
*/
func (r *Registry) saveLocked(hidden map[string]bool) error {
	ids := make([]string, 0, len(hidden))
	for id := range hidden {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	body, err := json.MarshalIndent(struct {
		Hidden []string `json:"hidden"`
	}{ids}, "", "\t")
	if err != nil {
		return err
	}
	return r.write(r.hiddenPath, append(body, '\n'))
}

/*
================
Online

One entered session as the roster pass sees it.
================
*/
type Online struct {
	SessionID string
	AccountID string
	Name      string
}

/*
================
Push

What the roster pass sends this tick. The roster is the names of the
online characters whose account shows the title. When it differs from
the last one, everyone gets it (all true, targets nil); otherwise only
sessions that never received it do (a new arrival). Nil payload off the
pace or when nobody needs a frame. Sessions that left are forgotten so
a re-entry is sent the roster again.
================
*/
func (r *Registry) Push(online []Online, nowMs int64) (payload []byte, targets []string, all bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if nowMs < r.pushDueMs {
		return nil, nil, false
	}
	r.pushDueMs = nowMs + pushIntervalMs
	if nowMs >= r.reloadDueMs {
		r.reloadDueMs = nowMs + reloadIntervalMs
		r.reloadLocked()
	}
	names := []string{}
	present := make(map[string]bool, len(online))
	for _, o := range online {
		present[o.SessionID] = true
		if r.members[o.AccountID] && !r.hidden[o.AccountID] {
			names = append(names, o.Name)
		}
	}
	for id := range r.sent {
		if !present[id] {
			delete(r.sent, id)
		}
	}
	roster := EncodeRoster(names)
	if string(roster) != string(r.roster) {
		r.roster = roster
		r.sent = make(map[string]bool, len(online))
		for id := range present {
			r.sent[id] = true
		}
		return roster, nil, true
	}
	for _, o := range online {
		if !r.sent[o.SessionID] {
			r.sent[o.SessionID] = true
			targets = append(targets, o.SessionID)
		}
	}
	if len(targets) == 0 {
		return nil, nil, false
	}
	return roster, targets, false
}
