/*
===========================================================================

host.go - the round machine: timers, intake, winners

One occurrence runs its rounds back to back. A round takes entries
for its whole length; at the end the winner is drawn from the
entrants whose answer the kind judged correct, paid exactly the
file's reward through the reward door, and announced. An account at
its daily win cap is left out of the draw, so one account's many
characters cannot sweep a day; an unpaid win (a bag that refuses)
never reaches the audit, so it never counts against the cap either.
A finished occurrence is remembered for the day, so a restart or a
long tick gap never runs it twice.

Port-only, not native.

===========================================================================
*/
package eventbot

import (
	"fmt"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
Tick

Advances the schedule to now: opens the next due occurrence, opens
its rounds and closes spent ones. A disabled host never ticks.
================
*/
func (h *Host) Tick(now time.Time) {
	if !h.Enabled() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if day := now.Format("2006-01-02"); day != h.doneDay {
		h.doneDay = day
		h.done = map[string]bool{}
	}
	if h.live != nil && !now.Before(h.live.endsAt) {
		h.closeRound(now)
	}
	// A world kind posts its hints on its own cadence while its
	// hider stands.
	if h.live != nil && h.live.hider != nil && !now.Before(h.live.nextHint) {
		if wk, ok := kindByName(h.live.event.Kind); ok {
			if wk, ok := wk.(WorldKind); ok {
				h.announce(wk.Hint(h.live.event, h.live.spot, now.Sub(h.live.endsAt.Add(-h.live.event.RoundLength))))
				h.live.nextHint = now.Add(wk.HintEvery())
			}
		}
	}
	if h.occ != nil && h.live == nil {
		if h.occ.round > h.occ.event.Rounds {
			h.announce(fmt.Sprintf("%s is over. Thanks for playing!", h.occ.event.DisplayTitle()))
			h.occ = nil
		} else {
			h.openRound(now)
		}
	}
	if h.occ == nil {
		h.beginNextOccurrence(now)
	}
}

/*
================
beginNextOccurrence

Starts today's earliest occurrence whose start has come and which
has not run yet. Occurrences never overlap: a start that passed
while another event ran begins when the host frees up.
================
*/
func (h *Host) beginNextOccurrence(now time.Time) {
	for _, event := range h.schedule.Events {
		start := event.startOn(now)
		key := event.Kind + "@" + start.Format("2006-01-02 15:04")
		if now.Before(start) || h.done[key] {
			continue
		}
		h.done[key] = true
		h.occ = &occurrenceState{event: event, starts: start, round: 1}
		h.openRound(now)
		return
	}
}

/*
================
openRound
================
*/
func (h *Host) openRound(now time.Time) {
	event := h.occ.event
	kind, _ := kindByName(event.Kind)
	h.live = &liveRound{
		event:     event,
		round:     h.occ.round,
		endsAt:    now.Add(event.RoundLength),
		byName:    map[string]int{},
		byAccount: map[string]int{},
	}
	// A world kind hides before it announces: the hider stands in
	// the world when the first hint names its area. A kind that
	// draws its place or its monster (kinds.go's pickers) draws
	// through the host's pick; the monster pick is recorded on
	// the live event, so the spawn door and the hints name the
	// monster that actually stands.
	if wk, ok := kind.(WorldKind); ok {
		spot, picked := wk.SpotFor(event, h.occ.round)
		if picker, ok := kind.(SpotPicker); ok {
			spot, picked = picker.PickSpot(event, h.pick)
		}
		if picked {
			h.live.spot = spot
			spawnEvent := event
			if picker, ok := kind.(MonsterPicker); ok {
				if codename, ok := picker.PickMonster(event, h.pick); ok {
					spawnEvent.Hider = codename
					h.live.event.Hider = codename
				}
			}
			if h.deps.SpawnHider != nil {
				if hider, spawned := h.deps.SpawnHider(spawnEvent, spot, now); spawned {
					h.live.hider = &hider
					h.live.nextHint = now.Add(wk.HintEvery())
				}
			}
		}
	}
	h.announce(kind.OpenLine(event, h.occ.round))
	if wk, ok := kind.(WorldKind); ok && h.live.hider != nil {
		h.announce(wk.Hint(event, h.live.spot, 0))
	}
	h.occ.round++
}

/*
================
closeRound

Draws and pays the winner, then whispers and announces the result.
================
*/
func (h *Host) closeRound(now time.Time) {
	live := h.live
	h.live = nil
	despawnHider(live)
	kind, _ := kindByName(live.event.Kind)
	// A world kind never draws: only a reported finder wins, and
	// the timer running out means nobody found the hider.
	if _, ok := kind.(WorldKind); ok {
		h.announce(fmt.Sprintf("%s round %d ends with no winner.", live.event.DisplayTitle(), live.round))
		return
	}
	pool := make([]entry, 0, len(live.entries))
	for _, e := range live.entries {
		if kind.Judge(e.answer) {
			pool = append(pool, e)
		}
	}
	// The draw excludes accounts at the daily cap; what is left of
	// the pool decides. An unpaid win never counts (WinsToday reads
	// the grant audit), so a full bag costs the winner nothing.
	for len(pool) > 0 {
		winner := pool[h.pick(len(pool))]
		if h.deps.WinsToday != nil && h.deps.WinsToday(winner.accountID, now) >= MaxWinsPerAccountPerDay {
			pool = withoutAccount(pool, winner.accountID)
			continue
		}
		paid := h.deps.Grant != nil && h.deps.Grant(winner.division, winner.character, live.event.RewardCodename, live.event.RewardCount)
		h.whisperTo(winner, winLine(live, paid))
		if paid {
			h.announce(fmt.Sprintf("%s won %s round %d and takes %dx %s!",
				winner.name, live.event.DisplayTitle(), live.round, live.event.RewardCount, live.event.RewardCodename))
		} else {
			h.announce(fmt.Sprintf("%s won %s round %d - but the prize could not be delivered, so this win is not counted.",
				winner.name, live.event.DisplayTitle(), live.round))
		}
		return
	}
	h.announce(fmt.Sprintf("%s round %d ends with no winner.", live.event.DisplayTitle(), live.round))
}

/*
================
despawnHider

Removes a round's hider exactly once, whoever ends the round.
================
*/
func despawnHider(live *liveRound) {
	if live != nil && live.hider != nil {
		if live.hider.Despawn != nil {
			live.hider.Despawn()
		}
		live.hider = nil
	}
}

/*
================
ReportFound

A finder reached the hider: the composition root has already
checked the world side (the player's own selection on the hider,
in range, alive, not in a stall) and asks the host to judge the
claim. Under the host's lock, exactly one claim wins: the finder
must have entered, its account must be under the daily cap, and
then the round ends - paid through the grant door, or announced
not counted when the bag refuses. A capped finder's round runs on.
False means the hunt goes on.
================
*/
func (h *Host) ReportFound(character *enterworld.Character) bool {
	if !h.Enabled() || character == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.live
	if live == nil || live.hider == nil {
		return false
	}
	kind, ok := kindByName(live.event.Kind)
	if !ok {
		return false
	}
	if _, world := kind.(WorldKind); !world {
		return false
	}
	// A kill round is never won by reaching the monster.
	if kill, ok := kind.(KillKind); ok && kill.WonByKill() {
		return false
	}
	at, entered := live.byName[strings.ToLower(character.Name)]
	if !entered {
		return false
	}
	winner := live.entries[at]
	now := h.now()
	if h.deps.WinsToday != nil && h.deps.WinsToday(winner.accountID, now) >= MaxWinsPerAccountPerDay {
		h.whisperTo(winner, "You found the hider! But your account has already won enough today, so the hunt goes on.")
		return false
	}
	h.live = nil
	despawnHider(live)
	paid := h.deps.Grant != nil && h.deps.Grant(winner.division, winner.character, live.event.RewardCodename, live.event.RewardCount)
	h.whisperTo(winner, winLine(live, paid))
	if paid {
		h.announce(fmt.Sprintf("%s found the hider and won %s round %d, taking %dx %s!",
			winner.name, live.event.DisplayTitle(), live.round, live.event.RewardCount, live.event.RewardCodename))
	} else {
		h.announce(fmt.Sprintf("%s found the hider in %s round %d - but the prize could not be delivered, so this win is not counted.",
			winner.name, live.event.DisplayTitle(), live.round))
	}
	return true
}

/*
================
LiveHider

The live round's hider, for the composition root's found-checks.
================
*/
func (h *Host) LiveHider() (Hider, bool) {
	if !h.Enabled() {
		return Hider{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.live == nil || h.live.hider == nil {
		return Hider{}, false
	}
	return *h.live.hider, true
}

/*
================
HiderLost

The hider is gone from the world (slain): the round ends with no
winner and the next round hides anew. False when no hider stands.
================
*/
func (h *Host) HiderLost() bool {
	if !h.Enabled() {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.live
	if live == nil || live.hider == nil {
		return false
	}
	h.live = nil
	despawnHider(live)
	h.announce(fmt.Sprintf("The hider was slain! %s round %d ends with no winner.", live.event.DisplayTitle(), live.round))
	return true
}

/*
================
LiveRoundWonByKill

The live round, if any, is a kill round (kinds.go's KillKind):
the composition root judges kills for it, never finds.
================
*/
func (h *Host) LiveRoundWonByKill() bool {
	if !h.Enabled() {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.live == nil {
		return false
	}
	kind, ok := kindByName(h.live.event.Kind)
	if !ok {
		return false
	}
	kill, ok := kind.(KillKind)
	return ok && kill.WonByKill()
}

/*
================
ReportSlain

A kill round's monster fell, owned by killerName (the world's kill
owner: the top-damage reward group's representative, the name the
unique-death notice announces): the round ends however it ends,
because the monster is gone. An entered killer under the daily cap takes the prize
through the grant door; a capped account's kill counts for nothing
and an outsider's kill pays nobody, both announced as no winner.
True means the round ended; false means no kill round is live.
================
*/
func (h *Host) ReportSlain(killerName string) bool {
	if !h.Enabled() || killerName == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.live
	if live == nil || live.hider == nil {
		return false
	}
	kind, ok := kindByName(live.event.Kind)
	if !ok {
		return false
	}
	kill, ok := kind.(KillKind)
	if !ok || !kill.WonByKill() {
		return false
	}
	h.live = nil
	despawnHider(live)
	at, entered := live.byName[strings.ToLower(killerName)]
	if !entered {
		h.announce(fmt.Sprintf("The unique was slain by %s, who had not entered %s round %d - no winner.",
			killerName, live.event.DisplayTitle(), live.round))
		return true
	}
	winner := live.entries[at]
	now := h.now()
	if h.deps.WinsToday != nil && h.deps.WinsToday(winner.accountID, now) >= MaxWinsPerAccountPerDay {
		h.whisperTo(winner, "You slew the unique! But your account has already won enough today, so this win does not count.")
		h.announce(fmt.Sprintf("%s slew the unique in %s round %d, but their account has already won enough today - no winner.",
			winner.name, live.event.DisplayTitle(), live.round))
		return true
	}
	paid := h.deps.Grant != nil && h.deps.Grant(winner.division, winner.character, live.event.RewardCodename, live.event.RewardCount)
	h.whisperTo(winner, winLine(live, paid))
	if paid {
		h.announce(fmt.Sprintf("%s slew the unique and won %s round %d, taking %dx %s!",
			winner.name, live.event.DisplayTitle(), live.round, live.event.RewardCount, live.event.RewardCodename))
	} else {
		h.announce(fmt.Sprintf("%s slew the unique in %s round %d - but the prize could not be delivered, so this win is not counted.",
			winner.name, live.event.DisplayTitle(), live.round))
	}
	return true
}

/*
================
Shutdown

The composition root is going down: the live hider leaves the
world with it. The host runs nothing after this.
================
*/
func (h *Host) Shutdown() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	despawnHider(h.live)
	h.live = nil
	h.occ = nil
}

/*
================
withoutAccount
================
*/
func withoutAccount(pool []entry, accountID string) []entry {
	out := pool[:0]
	for _, e := range pool {
		if e.accountID != accountID {
			out = append(out, e)
		}
	}
	return out
}

/*
================
winLine
================
*/
func winLine(live *liveRound, paid bool) string {
	if paid {
		return fmt.Sprintf("You won %s round %d! %dx %s was added to your bag.",
			live.event.DisplayTitle(), live.round, live.event.RewardCount, live.event.RewardCodename)
	}
	return fmt.Sprintf("You won %s round %d, but your bag could not take %dx %s, so the prize was not delivered and this win is not counted.",
		live.event.DisplayTitle(), live.round, live.event.RewardCount, live.event.RewardCodename)
}

/*
================
HandleWhisper

One whisper addressed to the bot. The chat lane acks it and delivers
the returned lines back as the bot's whispers. Outside a round the
answer says when the next event runs; inside one, the first whisper
enters (and, for a judged kind, is the answer - the first answer
stands) and every repeat is refused and never counted.
================
*/
func (h *Host) HandleWhisper(sender WhisperSender, text string) []string {
	if !h.Enabled() || sender.Character == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.live
	if live == nil {
		return []string{h.idleLine(h.now())}
	}
	name := sender.Character.Name
	if _, entered := live.byName[strings.ToLower(name)]; entered {
		return []string{"You are already in this round - one entry each, and your first answer stands."}
	}
	e := entry{
		accountID: sender.Character.AccountID,
		name:      name,
		division:  sender.Division,
		character: sender.Character,
		answer:    text,
	}
	live.byName[strings.ToLower(name)] = len(live.entries)
	live.entries = append(live.entries, e)
	live.byAccount[e.accountID]++
	if el, ok := kindByName(live.event.Kind); ok {
		if liner, ok := el.(EntryLiner); ok {
			return []string{liner.EntryLine(live.event, live.round)}
		}
	}
	return []string{fmt.Sprintf("You are in %s round %d! The winner is drawn when the round closes.",
		live.event.DisplayTitle(), live.round)}
}

/*
================
idleLine

What the bot answers outside a round.
================
*/
func (h *Host) idleLine(now time.Time) string {
	for _, occ := range h.schedule.Occurrences(now) {
		if !occ.Running {
			return fmt.Sprintf("No event is running now. Next: %s at %s - whisper me then to take part.",
				occ.Event.DisplayTitle(), occ.Start.Format(scheduleClock))
		}
	}
	return "No event is running now, and none is scheduled. Whisper me during an event to take part."
}

/*
================
ScheduledEvents

The feed rows while the host runs: each event's live-or-next
occurrence. A disabled host lists nothing.
================
*/
func (h *Host) ScheduledEvents(now time.Time) []Occurrence {
	if !h.Enabled() {
		return nil
	}
	return h.schedule.Occurrences(now)
}
