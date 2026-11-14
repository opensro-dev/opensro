/*
===========================================================================

contributor.go - the Contributor title (port-only, not native, #3358)

Owner, 2026-11-12: every Discord member holding the Contributor role
carries a yellow "Contributor" title in game, seen the same way by every
viewer. Owner, 2026-11-14 (#3488): on one account per member - their
first by default, which they can move to another of their own
("/contributor here") - since invite keys link any number of accounts
to one Discord id. The v1.150 client has no title of its own to reuse (no Hwan or
honor title over a name; the textuisystem UIO_SECOND_HWAN rows are data
for later builds only), so the title is this port's own frame and the
browser client draws it.

Two files in the shard's authority directory, one owner each:

  - contributors.json, written by the operations sync: {"members":
    [{"member":"<label>","accounts":["<oldest>", ...]}], "accounts":
    [...]}, each role member's linked game accounts oldest first under a
    hashed label. A file with only the flat "accounts" list (a sync
    older than members) makes each account its own member. In production it lives in the sync's own directory
    and SRO_CONTRIBUTORS_FILE names it: the sync runs as the bot's
    user and never writes into the GameWorld's authority directory.
    Unset, it is the shard directory's contributors.json (a local
    world). This package only reads it, re-reading when its size or
    modification time changes. A missing file is an empty set; a
    corrupt one keeps the last good set and logs.
  - contributor-titles.json, {"hidden":["<account id>", ...],
    "chosen":{"<label>":"<account id>"}}, owned here: a contributor's
    own "/contributor off" and "/contributor here" choices. They are
    server settings, so every viewer sees the same state. A chosen
    account the member no longer has falls back to their first.

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
	// EnvContributorsFile names the operations sync's file by absolute
	// path; unset means MembersFileName in the shard directory.
	EnvContributorsFile = "SRO_CONTRIBUTORS_FILE"
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
	// refreshIntervalMs resends the roster to everyone even unchanged:
	// a client that dropped it (a scene reload, a frame lost before its
	// world module was ready) recovers within this bound.
	refreshIntervalMs = 30_000
	// Command is the chat word a contributor types.
	Command = "/contributor"
)

// The system lines answering the chat command.
const (
	answerNotMember = "Only members with the Contributor role on Discord carry the Contributor title."
	answerShown     = "Your Contributor title is shown. Type /contributor off to hide it."
	answerHidden    = "Your Contributor title is hidden. Type /contributor on to show it."
	answerElsewhere = "Your Contributor title is on another of your accounts. Type /contributor here to move it to this one."
	answerMoved     = "Your Contributor title is now on this account."
	answerUsage     = "Usage: /contributor on, /contributor off, or /contributor here."
	answerKept      = "Your Contributor title stays on your other account. Type /contributor here any time to move it to this one."
	answerBusy      = "The title setting could not be saved; please try again in a moment."
)

// flatMemberPrefix labels an account read from a flat "accounts" list:
// each is its own member.
const flatMemberPrefix = "account:"

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

/*
================
MembersPath

Where the sync's file is read: SRO_CONTRIBUTORS_FILE when set (it must
be absolute, so a relative value cannot quietly resolve against the
process's working directory), else the shard directory's file.
================
*/
func MembersPath(getenv func(string) string, dir string) (string, error) {
	path := strings.TrimSpace(getenv(EnvContributorsFile))
	if path == "" {
		return filepath.Join(dir, MembersFileName), nil
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("contributor titles: %s must be an absolute path, got %q", EnvContributorsFile, path)
	}
	return path, nil
}

// fileStamp is what decides that the sync's file changed.
type fileStamp struct {
	size int64
	mod  time.Time
}

/*
================
Registry

Who is a contributor, which one account carries each member's title,
who hid it, and what roster each session last received. One mutex: the
chat handler and the tick hook are the only callers, and each holds it
for a map lookup or a short pass.
================
*/
type Registry struct {
	mu          sync.Mutex
	membersPath string
	hiddenPath  string
	write       func(string, []byte) error
	// groups is each member's linked accounts, oldest first; memberOf
	// is its inverse; titled is the one account per member that carries
	// the title (resolveLocked).
	groups   map[string][]string
	memberOf map[string]string
	titled   map[string]bool
	// chosen is each member's "/contributor here" account (persisted).
	chosen map[string]string
	// seen is, per account, the title holder its owner last answered the
	// login prompt about (persisted); prompted is the sessions already
	// asked this entry, so a prompt is one per login, never per tick.
	seen         map[string]string
	prompted     map[string]bool
	hidden       map[string]bool
	stamp        fileStamp
	reloadDueMs  int64
	pushDueMs    int64
	refreshDueMs int64
	// roster is the payload every session in sent holds.
	roster []byte
	sent   map[string]bool
}

/*
================
Open

Reads the sync's file at membersPath (MembersPath) and the opt-out
file from dir. A corrupt opt-out file stops the boot, the mute book's
posture: silently showing titles people hid is worse than a loud
refusal. The sync's file never stops it (reloadLocked's rules).
================
*/
func Open(dir, membersPath string, write func(string, []byte) error) (*Registry, error) {
	r := &Registry{
		membersPath: membersPath,
		hiddenPath:  filepath.Join(dir, HiddenFileName),
		write:       write,
		groups:      map[string][]string{},
		memberOf:    map[string]string{},
		titled:      map[string]bool{},
		sent:        map[string]bool{},
		prompted:    map[string]bool{},
	}
	choices, err := readChoices(r.hiddenPath)
	if err != nil {
		return nil, err
	}
	r.hidden, r.chosen, r.seen = choices.hidden, choices.chosen, choices.seen
	r.reloadLocked()
	return r, nil
}

// choices is the opt-out file's contents.
type choices struct {
	hidden map[string]bool
	chosen map[string]string
	seen   map[string]string
}

// choicesFile is the opt-out file's JSON shape.
type choicesFile struct {
	Hidden []string          `json:"hidden"`
	Chosen map[string]string `json:"chosen,omitempty"`
	Seen   map[string]string `json:"seen,omitempty"`
}

/*
================
readChoices
================
*/
func readChoices(path string) (choices, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return choices{hidden: map[string]bool{}, chosen: map[string]string{}, seen: map[string]string{}}, nil
	}
	if err != nil {
		return choices{}, fmt.Errorf("contributor titles: %w", err)
	}
	var file choicesFile
	if err := json.Unmarshal(body, &file); err != nil {
		return choices{}, fmt.Errorf("contributor titles: %s: %w", path, err)
	}
	chosen := make(map[string]string, len(file.Chosen))
	for member, account := range file.Chosen {
		if boundedID(member) && boundedID(account) {
			chosen[member] = account
		}
	}
	seen := make(map[string]string, len(file.Seen))
	for account, holder := range file.Seen {
		if boundedID(account) && boundedID(holder) {
			seen[account] = holder
		}
	}
	return choices{hidden: accountSet(file.Hidden), chosen: chosen, seen: seen}, nil
}

/*
================
boundedID
================
*/
func boundedID(id string) bool {
	return id != "" && len(id) <= maxAccountIDBytes && strings.TrimSpace(id) == id
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
		r.groups, r.stamp = map[string][]string{}, fileStamp{}
		r.resolveLocked()
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
		Members *[]struct {
			Member   string   `json:"member"`
			Accounts []string `json:"accounts"`
		} `json:"members"`
		Accounts []string `json:"accounts"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		log.WithError(err).Warn("contributor titles: members file corrupt; keeping the last set")
		return
	}
	groups := map[string][]string{}
	if file.Members != nil {
		for _, row := range *file.Members {
			if !boundedID(row.Member) {
				continue
			}
			for _, id := range row.Accounts {
				id = strings.TrimSpace(id)
				if id != "" && len(id) <= maxAccountIDBytes {
					groups[row.Member] = append(groups[row.Member], id)
				}
			}
		}
	} else {
		for id := range accountSet(file.Accounts) {
			groups[flatMemberPrefix+id] = []string{id}
		}
	}
	r.groups, r.stamp = groups, stamp
	r.resolveLocked()
	log.Infof("contributor titles: %d contributors", len(r.groups))
}

/*
================
resolveLocked

Rebuilds memberOf and titled from groups and chosen: each member's
title sits on its chosen account while that account is still one of
its own, else on its first (oldest). An account listed under two
members (it should not happen) stays with the first member read, in
label order, so the answer never depends on map order.
================
*/
func (r *Registry) resolveLocked() {
	labels := make([]string, 0, len(r.groups))
	for member := range r.groups {
		labels = append(labels, member)
	}
	sort.Strings(labels)
	memberOf := map[string]string{}
	titled := map[string]bool{}
	for _, member := range labels {
		owned := []string{}
		for _, id := range r.groups[member] {
			if _, taken := memberOf[id]; !taken {
				memberOf[id] = member
				owned = append(owned, id)
			}
		}
		if len(owned) == 0 {
			continue
		}
		pick := owned[0]
		if chosen, ok := r.chosen[member]; ok && memberOf[chosen] == member {
			pick = chosen
		}
		titled[pick] = true
	}
	r.memberOf, r.titled = memberOf, titled
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
	return r.titled[accountID] && !r.hidden[accountID]
}

/*
================
Command

The chat intake: "/contributor", "/contributor on", "/contributor off"
or "/contributor here" from the speaker's account. handled is false for
any other line, which stays ordinary chat. "here" moves the member's
title to the speaker's account; on and off act only on the account that
carries it. A choice is saved before it is reported, and a failed save
leaves the old choice in force.
================
*/
func (r *Registry) Command(accountID, message string) (answer string, handled bool) {
	fields := strings.Fields(strings.ToLower(message))
	if len(fields) == 0 || fields[0] != Command {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	member, linked := r.memberOf[accountID]
	if accountID == "" || !linked {
		return answerNotMember, true
	}
	if len(fields) == 2 && fields[1] == "here" {
		return r.moveLocked(member, accountID), true
	}
	if len(fields) == 2 && fields[1] == "keep" {
		return r.keepLocked(member, accountID), true
	}
	if len(fields) > 2 || (len(fields) == 2 && fields[1] != "on" && fields[1] != "off") {
		return answerUsage, true
	}
	if !r.titled[accountID] {
		return answerElsewhere, true
	}
	if len(fields) == 1 {
		if r.hidden[accountID] {
			return answerHidden, true
		}
		return answerShown, true
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
moveLocked

"/contributor here": the member's title moves to accountID, which is
one of its own. The account's own shown or hidden state comes with it.
================
*/
func (r *Registry) moveLocked(member, accountID string) string {
	state := answerShown
	if r.hidden[accountID] {
		state = answerHidden
	}
	if r.titled[accountID] {
		return state
	}
	next := make(map[string]string, len(r.chosen)+1)
	for label, id := range r.chosen {
		next[label] = id
	}
	next[member] = accountID
	// Moving answers this account's prompt: it now holds the title.
	seen := r.seenWithLocked(accountID, accountID)
	if err := r.saveAllLocked(r.hidden, next, seen); err != nil {
		log.WithError(err).Warn("contributor titles: choice save failed")
		return answerBusy
	}
	r.chosen, r.seen = next, seen
	r.resolveLocked()
	// The next roster pass sends the changed roster to everyone.
	r.pushDueMs = 0
	return answerMoved + " " + state
}

/*
================
keepLocked

"/contributor keep": the login prompt's "Keep it there" (or the title
holder's OK). Records that this account answered the prompt about the
current holder, so it does not come back until the holder changes.
================
*/
func (r *Registry) keepLocked(member, accountID string) string {
	holder := r.holderLocked(member)
	answer := answerKept
	if holder == accountID {
		answer = answerShown
		if r.hidden[accountID] {
			answer = answerHidden
		}
	}
	if holder == "" || r.seen[accountID] == holder {
		return answer
	}
	seen := r.seenWithLocked(accountID, holder)
	if err := r.saveAllLocked(r.hidden, r.chosen, seen); err != nil {
		log.WithError(err).Warn("contributor titles: prompt answer save failed")
		return answerBusy
	}
	r.seen = seen
	return answer
}

/*
================
seenWithLocked

A copy of seen with one more answer: account answered about holder.
================
*/
func (r *Registry) seenWithLocked(accountID, holder string) map[string]string {
	next := make(map[string]string, len(r.seen)+1)
	for account, answered := range r.seen {
		next[account] = answered
	}
	next[accountID] = holder
	return next
}

/*
================
holderLocked

The account that carries member's title now, or "" when none does.
================
*/
func (r *Registry) holderLocked(member string) string {
	for _, id := range r.groups[member] {
		if r.memberOf[id] == member && r.titled[id] {
			return id
		}
	}
	return ""
}

/*
================
Prompts

The login prompt (port-only, not native; owner, 2026-11-14): each
entered session of a contributor whose account has not answered about
its member's current holder is asked once per entry. PromptElsewhere
goes to an account without the title ([Move it here] [Keep it there]),
PromptHolder to the one that carries it ([OK]). The client answers
with "/contributor here" or "/contributor keep" on chat, so no new
C->S frame exists. Sessions that left are forgotten, so the next login
asks again while the answer is still missing.
================
*/
func (r *Registry) Prompts(online []Online) []Prompt {
	r.mu.Lock()
	defer r.mu.Unlock()
	present := make(map[string]bool, len(online))
	var prompts []Prompt
	for _, o := range online {
		present[o.SessionID] = true
		if r.prompted[o.SessionID] {
			continue
		}
		member, linked := r.memberOf[o.AccountID]
		if !linked {
			continue
		}
		holder := r.holderLocked(member)
		if holder == "" || r.seen[o.AccountID] == holder {
			continue
		}
		r.prompted[o.SessionID] = true
		kind := PromptElsewhere
		if holder == o.AccountID {
			kind = PromptHolder
		}
		prompts = append(prompts, Prompt{SessionID: o.SessionID, Kind: kind})
	}
	for id := range r.prompted {
		if !present[id] {
			delete(r.prompted, id)
		}
	}
	return prompts
}

/*
================
saveLocked
================
*/
func (r *Registry) saveLocked(hidden map[string]bool) error {
	return r.saveAllLocked(hidden, r.chosen, r.seen)
}

/*
================
saveAllLocked

Writes every choice: the opt-outs sorted, the moves keyed by member,
the answered prompts keyed by account.
================
*/
func (r *Registry) saveAllLocked(hidden map[string]bool, chosen, seen map[string]string) error {
	ids := make([]string, 0, len(hidden))
	for id := range hidden {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	body, err := json.MarshalIndent(choicesFile{Hidden: ids, Chosen: chosen, Seen: seen}, "", "\t")
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
the last one, or the refresh bound passed, everyone gets it (all true,
targets nil); otherwise only sessions that never received it do (a new
arrival). Nil payload off the
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
		if r.titled[o.AccountID] && !r.hidden[o.AccountID] {
			names = append(names, o.Name)
		}
	}
	for id := range r.sent {
		if !present[id] {
			delete(r.sent, id)
		}
	}
	roster := EncodeRoster(names)
	if string(roster) != string(r.roster) || nowMs >= r.refreshDueMs {
		r.roster = roster
		r.refreshDueMs = nowMs + refreshIntervalMs
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
