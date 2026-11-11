/*
===========================================================================

announce.go - shard notices for a seal drop, a high plus and a job kill

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
Each kind has its own flag, off by default here (native: no notice); the
GameWorld's Nomad job turns them on:

	SRO_ANNOUNCE_SOX_DROP  a seal drop notice, worded by its tier (#1640;
	                       sealDropText: the Sun's line is the loudest)
	SRO_ANNOUNCE_PLUS      "<player> advanced <item> to +N" (N >= 7)
	SRO_ANNOUNCE_PLUS_TIERS  the plus line leads "Great success!" at +10
	                       and "LEGENDARY!" at +11 and above (#1739)
	SRO_ANNOUNCE_JOB_KILL  "[<killer alias>] killed [<victim alias>]" (job aliases, never character names)
	                       and (#3151) "[<murderer>] murdered [<victim>]
	                       (Murderer Lv N)" for an open-field murder: character
	                       names (neither side wears a suit to alias), N the
	                       murderer's new PK level - the total the client
	                       calls the PK level (0x3647) - written MAX at its
	                       cap; same flag, channel, cap and pair window as
	                       the job-kill line.
	SRO_ANNOUNCE_MOST_WANTED  "[<alias>] is MOST WANTED Lv <N>" on a new wanted level, and
	                          "[<killer alias>] claimed bounty of [<victim alias>] MOST WANTED Lv <N>"
	SRO_ALCHEMY_BREAK_NOTICE  "<alias> broke <item>" when a reinforcement destroys the
	                          item of a character who opted in on the account page
	                          (#1738, #1604 P7.2); no alias shows "a player",
	                          never a character name, and one character is
	                          rate-capped on top of the per-minute shard cap.
	SRO_ANNOUNCE_GACHA_WIN  "<player> won <prize> xN from Magic Pang" on a
	                          committed Magic Pop win (#2852); the native
	                          0x30C0 banner is unchanged either way.
	SRO_ANNOUNCE_PLUS_FAIL  "<alias> failed +N on <item>: ..." for an
	                          opted-in character's failed reinforce at
	                          SRO_ANNOUNCE_PLUS_FAIL_MIN (default 9) or
	                          above (#2852; #1738's alias rule: never a
	                          character name; rides the break channel).

A notice is the native notification packet the operator notices use
(wire.NotificationFrame), sent to every session of the division and kept
in the website's chat feed. Hooks post from inside character doors, so a
notice is queued and the next tick sends it, outside every door. Each kind
is capped shard-wide per minute, and a job kill repeats for the same pair
only after jobKillRepeatMs, so a farm cannot flood chat.

SRO_JOB_KILL_NOTICE_REPEAT (port-only, not native; owner decision
2026-10-15, so the balance advisor can see a farmed pair) lifts the pair
window: every job kill between the same pair is announced, still under the
per-minute shard cap. Off here keeps the window.

===========================================================================
*/
package action

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
)

// The three announcement flags ("on", "1", "true").
const (
	EnvAnnounceSoxDrop = "SRO_ANNOUNCE_SOX_DROP"
	EnvAnnouncePlus    = "SRO_ANNOUNCE_PLUS"
	// EnvAnnouncePlusTiers tiers the plus notice's lead word (#1739).
	EnvAnnouncePlusTiers = "SRO_ANNOUNCE_PLUS_TIERS"
	EnvAnnounceJobKill   = "SRO_ANNOUNCE_JOB_KILL"
	// EnvJobKillNoticeRepeat announces every kill of a pair (no window).
	EnvJobKillNoticeRepeat = "SRO_JOB_KILL_NOTICE_REPEAT"
	// EnvAnnounceMostWanted announces wanted levels and claimed bounties
	// (owner decision 2026-10-15, for the balance advisor).
	EnvAnnounceMostWanted = "SRO_ANNOUNCE_MOST_WANTED"
	// EnvAlchemyBreakNotice announces an opted-in character's destroyed
	// reinforcement item by job alias (owner-approved vote 2026-10-26,
	// #1604 P7.2 / #1738).
	EnvAlchemyBreakNotice = "SRO_ALCHEMY_BREAK_NOTICE"
	// EnvAnnounceGachaWin adds an Event-feed line naming the winner of a
	// committed Magic Pop win (#2852). The native win banner (0x30C0)
	// carries no winner and is sent either way.
	EnvAnnounceGachaWin = "SRO_ANNOUNCE_GACHA_WIN"
	// EnvAnnouncePlusFail announces an opted-in character's failed
	// reinforce at or above EnvAnnouncePlusFailMin, by job alias
	// (#2852; failures keep #1738's alias-only, opt-in rule).
	EnvAnnouncePlusFail = "SRO_ANNOUNCE_PLUS_FAIL"
	// EnvAnnouncePlusFailMin is the lowest attempted plus (the plus
	// before the attempt, +1) a failure notice announces.
	EnvAnnouncePlusFailMin = "SRO_ANNOUNCE_PLUS_FAIL_MIN"
)

// announcePlusFailMinDefault is the attempted plus a failure notice
// starts at when SRO_ANNOUNCE_PLUS_FAIL_MIN names none (#2852).
const announcePlusFailMinDefault = 9

const (
	// announcementsPerMinute caps the seal-drop and plus notices per
	// division; the job-kill and Most Wanted caps are tunable
	// (announceCap, game/tunables) and default to the same six.
	announcementsPerMinute = 6
	announceWindowMs       = 60_000
	// jobKillPairPrune is the pair count past which stale pairs are dropped.
	jobKillPairPrune = 1024
	// announcePlusMin is the lowest plus a reinforcement notice announces
	// (owner decision 2026-10-18: +7 and above, with no upper ceiling).
	announcePlusMin = 7
	// breakNoticePerCharacterMs is #1738's per-character window on top of
	// the break kind's per-minute shard cap: one character's repeated
	// breaks show at most one notice per ten minutes.
	breakNoticePerCharacterMs = 10 * 60_000
	// announcePlusGreatAt is the plus whose tiered notice leads
	// "Great success!"; from announcePlusLegendaryAt it leads
	// "LEGENDARY!" (#1739, owner decision 2026-10-26).
	announcePlusGreatAt     = 10
	announcePlusLegendaryAt = 11
)

// sealCodenameSuffix marks a v1.150 rare (Seal of Star/Moon/Sun) item.
const sealCodenameSuffix = "_RARE"

// sealNoticeNames name a sealed item's tier in the drop notice with the
// client's own words: the English PARAM_RARE_FIRST/SECOND/THIRD rows of
// textuisystem that the client's ItemTooltip_AppendRareGrade (558FF0) shows, indexed by
// enterworld.EquipmentSeal's tier ((ItemClass - 1) % 3).
var sealNoticeNames = [enterworld.SealTiers]string{
	enterworld.SealStar: "Seal Of Star",
	enterworld.SealMoon: "Seal Of Moon",
	enterworld.SealSun:  "Seal Of Sun",
}

// announceKind indexes the three notices.
type announceKind uint8

const (
	announceSoxDrop announceKind = iota
	announcePlus
	announceJobKill
	// announceMostWanted has its own per-minute bucket, so a run of wanted
	// notices never starves the kill notices; it shares their channel.
	announceMostWanted
	// announceBreak has its own per-minute bucket and its own channel, so
	// a run of breaks never starves the other notices.
	announceBreak
	// announceGachaWin has its own bucket, so a run of Magic Pop wins
	// never starves the drop and plus notices it shares a channel with.
	announceGachaWin
	// announcePlusFail has its own bucket; its frame is the break
	// channel's, like announceBreak's (it links nothing either).
	announcePlusFail
	// announceHalloweenWave has its own bucket, so a run of drops never
	// starves a wave's notice (port-only, halloween_waves.go).
	announceHalloweenWave
	announceKinds
)

/*
================
Announcements

Which notices the shard sends. The zero value is native: none.
JobKillRepeat drops the pair window (EnvJobKillNoticeRepeat).
================
*/
type Announcements struct {
	SoxDrop, Plus, JobKill bool
	PlusTiers              bool
	JobKillRepeat          bool
	MostWanted             bool
	// Break announces an opted-in character's destroyed item (#1738).
	Break bool
	// GachaWin adds an Event-feed line naming a Magic Pop winner
	// (#2852); the native win banner is sent either way.
	GachaWin bool
	// PlusFail announces an opted-in character's failed reinforce by
	// job alias (#2852), from PlusFailMin attempted plus up.
	PlusFail    bool
	PlusFailMin int
}

/*
================
AnnouncementsFromEnv
================
*/
func AnnouncementsFromEnv() Announcements {
	on := func(name string) bool {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "on", "1", "true":
			return true
		}
		return false
	}
	failMin := announcePlusFailMinDefault
	if text := strings.TrimSpace(os.Getenv(EnvAnnouncePlusFailMin)); text != "" {
		if parsed, err := strconv.Atoi(text); err == nil && parsed >= 1 {
			failMin = parsed
		}
	}
	return Announcements{SoxDrop: on(EnvAnnounceSoxDrop), Plus: on(EnvAnnouncePlus), PlusTiers: on(EnvAnnouncePlusTiers),
		JobKill: on(EnvAnnounceJobKill), JobKillRepeat: on(EnvJobKillNoticeRepeat), MostWanted: on(EnvAnnounceMostWanted),
		Break: on(EnvAlchemyBreakNotice), GachaWin: on(EnvAnnounceGachaWin), PlusFail: on(EnvAnnouncePlusFail),
		PlusFailMin: failMin}
}

/*
================
announcer

The queue and the rate limits. One per Runtime.
================
*/
type announcer struct {
	mu      sync.Mutex
	queued  []simulation.DivisionFrames
	feed    []announcedLine
	windows map[announceWindowKey][]int64
	pairs   map[string]int64
	// breaks is #1738's per-character window: division and character name
	// to the last break notice's time. The name keys the window only; it
	// never leaves the server (the notice names the alias or "a player").
	breaks map[string]int64
}

type announceWindowKey struct {
	division string
	kind     announceKind
}

type announcedLine struct {
	division, text string
}

/*
================
Runtime.announce

Queues one notice when its kind is under the per-minute cap. link is what
a drop or plus notice links (wire.DropPlusNotificationFrame); a job kill
links nothing, so its aliases never lead to a character. refObjID is the
linked item's reference (0 when the notice links no item): its reference
rows ride ahead of the notice in the same division frames, because a
viewer who was never sent that reference cannot render the link
(BUG-084 - the bootstrap preloads only Degree-1 references, so a Degree
2-9 seal or advanced item is unknown on most viewers). Returns true when
the line went out and false when the kind's bucket dropped it, so a
caller folding another line into this one only folds on a real post.
================
*/
func (rt *Runtime) announce(division string, kind announceKind, text string, link wire.NoticeLink, refObjID uint32, nowMs int64) bool {
	limit := announceCap(tunables.Current(), kind)
	a := &rt.announcer
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.windows == nil {
		a.windows = map[announceWindowKey][]int64{}
	}
	key := announceWindowKey{division, kind}
	recent := a.windows[key][:0]
	for _, at := range a.windows[key] {
		if nowMs-at < announceWindowMs {
			recent = append(recent, at)
		}
	}
	if int64(len(recent)) >= limit {
		a.windows[key] = recent
		return false
	}
	a.windows[key] = append(recent, nowMs)
	text = clampNotice(text)
	// Port-only: each kind has its own channel, so a player can hide it
	// (wire.JobKillNoticeChannel, wire.DropPlusNoticeChannel,
	// wire.BreakNoticeChannel).
	frame := wire.DropPlusNotificationFrame(text, link)
	switch kind {
	case announceJobKill, announceMostWanted:
		frame = wire.JobKillNotificationFrame(text)
	case announceBreak, announcePlusFail:
		frame = wire.BreakNotificationFrame(text)
	}
	frames := make([]simulation.Frame, 0, 2)
	if refObjID != 0 {
		for _, ref := range rt.itemReferencesByID([]uint32{refObjID}) {
			frames = append(frames, simulation.Frame{Opcode: ref.Opcode, Payload: ref.Payload})
		}
	}
	frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
	a.queued = append(a.queued, simulation.DivisionFrames{DivisionID: division, Frames: frames})
	a.feed = append(a.feed, announcedLine{division, text})
	return true
}

/*
================
Runtime.drainAnnouncements

The tick step that sends the queued notices, outside every door.
================
*/
func (rt *Runtime) drainAnnouncements(int64) []simulation.DivisionFrames {
	a := &rt.announcer
	a.mu.Lock()
	out, feed := a.queued, a.feed
	a.queued, a.feed = nil, nil
	a.mu.Unlock()
	if rt.PublicChat != nil {
		for _, line := range feed {
			rt.PublicChat.Record(line.division, chatfeed.KindEvent, "", "", line.text)
		}
	}
	return out
}

/*
================
announceCap

A kind's notices per division a minute, from the action's snapshot.
================
*/
func announceCap(s *tunables.Settings, kind announceKind) int64 {
	switch kind {
	case announceJobKill:
		return s.Int(tunables.JobKillNoticesPerMinute)
	case announceMostWanted:
		return s.Int(tunables.MostWantedNoticesPerMinute)
	}
	return announcementsPerMinute
}

/*
================
jobKillRepeatMs

How long one killer/victim pair stays quiet, from the action's snapshot.
================
*/
func jobKillRepeatMs(s *tunables.Settings) int64 {
	return s.Int(tunables.JobKillRepeatMinutes) * 60_000
}

/*
================
Runtime.jobKillPairQuiet

Whether a killer/victim pair may be announced again, recording it when so.
================
*/
func (rt *Runtime) jobKillPairQuiet(killer, victim string, nowMs int64) bool {
	repeatMs := jobKillRepeatMs(tunables.Current())
	a := &rt.announcer
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pairs == nil {
		a.pairs = map[string]int64{}
	}
	key := strings.ToLower(killer) + "\x00" + strings.ToLower(victim)
	if at, seen := a.pairs[key]; seen && nowMs-at < repeatMs {
		return false
	}
	if len(a.pairs) >= jobKillPairPrune {
		for pair, at := range a.pairs {
			if nowMs-at >= repeatMs {
				delete(a.pairs, pair)
			}
		}
	}
	a.pairs[key] = nowMs
	return true
}

/*
================
Runtime.announceSealDrops

Item 1: one notice per seal (rare) item a monster's committed death dropped,
naming the drop's owner and the item's seal. Called after the kill's commit.
================
*/
func (rt *Runtime) announceSealDrops(division, monsterName string, drops []grounditem.Item, nowMs int64) {
	if !rt.Announce.SoxDrop {
		return
	}
	minDegree := int(tunables.Current().Int(tunables.SoxNoticeMinDegree))
	items := rt.deps.ItemReferences()
	for _, drop := range drops {
		if drop.DroppedBy == "" || !strings.HasSuffix(drop.Codename, sealCodenameSuffix) {
			continue
		}
		name := drop.Codename
		tier, sealed := 0, false
		if items != nil {
			if ref, found := items.ItemRefByCodename(drop.Codename); found {
				if degree := enterworld.EquipmentDegree(ref); degree > 0 && degree < minDegree {
					continue
				}
				if ref.Name != "" {
					name = ref.Name
				}
				tier, sealed = enterworld.EquipmentSeal(ref)
			}
		}
		link := wire.NoticeLink{Player: drop.DroppedBy, Body: groundInventoryItem(drop, groundStack(drop)).Body().Encode()}
		rt.announce(division, announceSoxDrop, sealDropText(drop.DroppedBy, name, tier, sealed, monsterName), link, drop.RefObjID, nowMs)
	}
}

/*
================
sealDropText

#1640 (P4, owner decision 2026-10-26: "Colour by tier + Sun prefix"):
Star and Moon keep the notice's original sentence; only the Sun is
worded apart, leading with its name in capitals, the loudest line,
and dropping the parenthesis. The client colours the line by tier
and reads this lead (and the old parenthesis, on lines from a server
without this change) for its Sun highlight. An item whose seal cannot
be read keeps the plain sentence. Same flag, same cap, no place named.
================
*/
func sealDropText(player, itemName string, tier int, sealed bool, monster string) string {
	if !sealed {
		return player + " has dropped " + itemName + " from " + monster
	}
	if tier == enterworld.SealSun {
		return strings.ToUpper(sealNoticeNames[tier]) + ": " + player + " has dropped " + itemName + " from " + monster
	}
	return player + " has dropped " + itemName + " (" + sealNoticeNames[tier] + ") from " + monster
}

/*
================
plusNoticeLead

The louder lead a tiered plus notice opens with (#1739, owner decision
2026-10-26): "Great success!" at +10, "LEGENDARY!" at +11 and above.
Below +10, or with the tiers off, there is no lead, so the plain line
stays byte for byte. Port-only, not native.
================
*/
func plusNoticeLead(plus uint8, tiers bool) string {
	if !tiers {
		return ""
	}
	switch {
	case plus >= announcePlusLegendaryAt:
		return "LEGENDARY! "
	case plus >= announcePlusGreatAt:
		return "Great success! "
	}
	return ""
}

/*
================
Runtime.announcePlus

Item 2: a reinforcement that succeeded into +7 or above, after its commit,
naming a sealed item's seal as the drop notice does. With the plus tiers
on, +10 and above lead louder (plusNoticeLead); the line's seal colour
rides the item link as before, tiers or not.
================
*/
func (rt *Runtime) announcePlus(division, player string, outcome alchemy.Outcome, nowMs int64) {
	if !rt.Announce.Plus || !outcome.Success {
		return
	}
	for _, item := range outcome.Items {
		if item.Slot != outcome.Target || item.Plus < announcePlusMin {
			continue
		}
		name, seal := item.Codename, ""
		if items := rt.deps.ItemReferences(); items != nil {
			if ref, found := items.ItemRefByCodename(item.Codename); found && ref.Name != "" {
				name = ref.Name
				if tier, sealed := enterworld.EquipmentSeal(ref); sealed {
					seal = " (" + sealNoticeNames[tier] + ")"
				}
			}
		}
		lead := plusNoticeLead(item.Plus, rt.Announce.PlusTiers)
		text := lead + fmt.Sprintf("%s advanced %s%s to +%d", player, name, seal, item.Plus)
		// The seal is the part to give up: clamping would cut the plus off
		// the end instead. A tier lead is given up next, for the same reason.
		if len(utf16.Encode([]rune(text))) > noticeMaxUnits {
			text = lead + fmt.Sprintf("%s advanced %s to +%d", player, name, item.Plus)
		}
		if lead != "" && len(utf16.Encode([]rune(text))) > noticeMaxUnits {
			text = fmt.Sprintf("%s advanced %s to +%d", player, name, item.Plus)
		}
		rt.announce(division, announcePlus, text, wire.NoticeLink{Player: player, Body: item.Body().Encode()}, item.RefObjID, nowMs)
		return
	}
}

/*
================
Runtime.breakNoticeQuiet

Whether this character's last break-channel notice is at least
breakNoticePerCharacterMs old (#1738: the per-character cap under the
break kind's per-minute shard cap). A caller that goes on to post
records the line with breakNoticeRecord.
================
*/
func (rt *Runtime) breakNoticeQuiet(division, name string, nowMs int64) bool {
	a := &rt.announcer
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.breaks == nil {
		a.breaks = map[string]int64{}
	}
	key := division + "\x00" + name
	if at, seen := a.breaks[key]; seen && nowMs-at < breakNoticePerCharacterMs {
		return false
	}
	if len(a.breaks) >= jobKillPairPrune {
		for key, at := range a.breaks {
			if nowMs-at >= breakNoticePerCharacterMs {
				delete(a.breaks, key)
			}
		}
	}
	return true
}

/*
================
Runtime.breakNoticeRecord

Starts the character's breakNoticePerCharacterMs window, called by a
line that is posting (the break line records at its gate, the failure
line only once its post is confirmed, so a bucket-dropped failure
never consumes the window its folded break line would need).
================
*/
func (rt *Runtime) breakNoticeRecord(division, name string, nowMs int64) {
	a := &rt.announcer
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.breaks == nil {
		a.breaks = map[string]int64{}
	}
	a.breaks[division+"\x00"+name] = nowMs
}

/*
================
Runtime.announceBreak

#1738 (port-only, not native; owner-approved vote 2026-10-26, #1604
P7.2): a reinforcement that destroyed the item, after its commit, when
the shard flag is on AND the character opted in on the account page
(character.BreakNotice). The notice names the character's job alias -
set or not, worn or not; alchemy happens in town, out of the suit - or
"a player" when they set none, never the character name, and links
nothing, so the alias cannot lead to a character (the job-kill notice's
rule). The destroyed item is named by its reference name and seal, as
the plus notice names it; codename is the item the outcome destroyed,
read from the bag before the commit because a destroyed item is not in
outcome.Items. A notice only: the break's odds and outcome are the
reinforcement's own, untouched.
================
*/
func (rt *Runtime) announceBreak(division string, character *enterworld.Character, codename string, outcome alchemy.Outcome, nowMs int64) {
	if !rt.Announce.Break || character == nil || !character.BreakNotice {
		return
	}
	if outcome.Success || !outcome.Destroyed {
		return
	}
	if !rt.breakNoticeQuiet(division, character.Name, nowMs) {
		return
	}
	rt.breakNoticeRecord(division, character.Name, nowMs)
	name, seal := rt.announceItemName(codename)
	who := announceAlias(character)
	text := fmt.Sprintf("%s broke %s%s", who, name, seal)
	// The seal is the part to give up, as in the plus notice.
	if len(utf16.Encode([]rune(text))) > noticeMaxUnits {
		text = fmt.Sprintf("%s broke %s", who, name)
	}
	rt.announce(division, announceBreak, text, wire.NoticeLink{}, 0, nowMs)
}

/*
================
Runtime.announceItemName

An item's reference name and its " (Seal Of ...)" suffix, as the plus
and break notices name one; the codename itself when no reference or
name answers.
================
*/
func (rt *Runtime) announceItemName(codename string) (name, seal string) {
	name = codename
	if items := rt.deps.ItemReferences(); items != nil {
		if ref, found := items.ItemRefByCodename(codename); found && ref.Name != "" {
			name = ref.Name
			if tier, sealed := enterworld.EquipmentSeal(ref); sealed {
				seal = " (" + sealNoticeNames[tier] + ")"
			}
		}
	}
	return name, seal
}

/*
================
announceAlias

How an alias notice names its character (#1738): the job alias, or
"a player" when they set none. Never the character name.
================
*/
func announceAlias(character *enterworld.Character) string {
	if who := strings.TrimSpace(character.Job.Alias); who != "" {
		return who
	}
	return "a player"
}

/*
================
Runtime.announcePlusFail

#2852 (port-only, not native): a failed reinforce whose attempted plus
(the plus before the attempt, +1, outcome.From) reaches
SRO_ANNOUNCE_PLUS_FAIL_MIN, when the shard flag is on AND the character
opted in on the account page - the break notice's opt-in
(character.BreakNotice) and its privacy rule: the job alias or
"a player", never the character name, and the line links nothing (the
break channel), so the alias cannot lead to a character. The wording
names the outcome: destroyed, saved by its Immortal option, or dropped
to its new plus. Returns true only when the line actually posted (the
kind's shard bucket can still drop it), so the caller folds a destroyed
item's break line into this one instead of posting both, and only then.
The line also keeps the break channel's per-character window, shared
with the break line, and spends it only on a post. A notice only: the
reinforcement's outcome stands either way.
================
*/
func (rt *Runtime) announcePlusFail(division string, character *enterworld.Character, codename string, outcome alchemy.Outcome, nowMs int64) bool {
	if !rt.Announce.PlusFail || character == nil || !character.BreakNotice || outcome.Success {
		return false
	}
	attempted := int(outcome.From) + 1
	if attempted < rt.Announce.PlusFailMin {
		return false
	}
	if !rt.breakNoticeQuiet(division, character.Name, nowMs) {
		return false
	}
	name, seal := rt.announceItemName(codename)
	who := announceAlias(character)
	var text string
	switch {
	case outcome.Destroyed:
		text = fmt.Sprintf("%s failed +%d on %s%s: destroyed", who, attempted, name, seal)
	case outcome.ImmortalSaved:
		text = fmt.Sprintf("%s failed +%d on %s%s: saved by Immortal", who, attempted, name, seal)
	default:
		final := 0
		for _, item := range outcome.Items {
			if item.Slot == outcome.Target {
				final = int(item.Plus)
			}
		}
		text = fmt.Sprintf("%s failed +%d on %s%s: dropped to +%d", who, attempted, name, seal, final)
	}
	// The seal is the part to give up, as in the break notice.
	if len(utf16.Encode([]rune(text))) > noticeMaxUnits {
		text = strings.Replace(text, seal, "", 1)
	}
	posted := rt.announce(division, announcePlusFail, text, wire.NoticeLink{}, 0, nowMs)
	if posted {
		rt.breakNoticeRecord(division, character.Name, nowMs)
	}
	return posted
}

/*
================
Runtime.announceGachaWin

#2852 (port-only, not native): the Event-feed half of a committed
Magic Pop win - "<player> won <prize> xN from Magic Pang" on the
drop/plus channel, naming the winner the native 0x30C0 banner cannot
(its body is prize and quantity only, one body for every session).
The banner itself is sent by the caller either way; this line only
adds the Event tab and the website's event feed. The winner is linked
as a drop notice links its dropper; the prize is not, the win created
no item body to link.
================
*/
func (rt *Runtime) announceGachaWin(division, player string, rewardRefObjID, quantity uint32, nowMs int64) {
	if !rt.Announce.GachaWin {
		return
	}
	name := ""
	if items := rt.deps.ItemReferences(); items != nil {
		if ref, found := items.ItemRefByID(rewardRefObjID); found && ref != nil {
			name = ref.Name
			if name == "" {
				name = ref.Codename
			}
		}
	}
	if name == "" {
		name = fmt.Sprintf("item %d", rewardRefObjID)
	}
	text := fmt.Sprintf("%s won %s x%d from Magic Pang", player, name, quantity)
	rt.announce(division, announceGachaWin, text, wire.NoticeLink{Player: player}, rewardRefObjID, nowMs)
}

// jobNames are the dressed jobs' names (domain.JobTrader/Thief/Hunter).
var jobNames = map[uint8]string{domain.JobTrader: "Trader", domain.JobThief: "Thief", domain.JobHunter: "Hunter"}

/*
================
jobKillNotice

Item 3's facts, read inside the kill's door while both suits are worn:
the pair's character names key the repeat window, the job aliases are
what the notice shows.
================
*/
type jobKillNotice struct {
	division, killer, victim string
	killerAlias, victimAlias string
	nowMs                    int64
}

/*
================
jobKillAlias

The name a dressed player shows in a job-kill notice: the job alias
(owner decision 2026-10-15), never the character's name. A wearer without
an alias shows the job's name.
================
*/
func jobKillAlias(c *enterworld.Character, job uint8) string {
	if alias := strings.TrimSpace(c.Job.Alias); alias != "" {
		return alias
	}
	return jobNames[job]
}

/*
================
jobKillNoticeOf

A job kill (pk.DeathJob) between two dressed players. Read in
payPlayerKillInDoor; posted by postJobKillNotice once the kill commits.
================
*/
func jobKillNoticeOf(division string, killer, victim *enterworld.Character, nowMs int64) (jobKillNotice, bool) {
	killerJob, victimJob := enterworld.DressedJob(killer), enterworld.DressedJob(victim)
	if jobNames[killerJob] == "" || jobNames[victimJob] == "" {
		return jobKillNotice{}, false
	}
	return jobKillNotice{division, killer.Name, victim.Name,
		jobKillAlias(killer, killerJob), jobKillAlias(victim, victimJob), nowMs}, true
}

/*
================
Runtime.postJobKillNotice

Item 3: queues a committed job kill's notice by the pair's job aliases,
in the client's bracketed style (UIIT_MSG_ANYONE_DEAD_UNIC "[%s]has killed
[%s]."), unless the pair was announced within jobKillRepeatMs and the
repeat rule is off.
================
*/
func (rt *Runtime) postJobKillNotice(n jobKillNotice) {
	if !rt.Announce.JobKill {
		return
	}
	if !rt.Announce.JobKillRepeat && !rt.jobKillPairQuiet(n.killer, n.victim, n.nowMs) {
		return
	}
	rt.announce(n.division, announceJobKill, "["+n.killerAlias+"] killed ["+n.victimAlias+"]", wire.NoticeLink{}, 0, n.nowMs)
}

/*
================
murderNotice

An open-field murder's notice facts (#3151), read inside the kill's door
where the murder was booked: the pair's character names key the repeat
window and are what the notice shows (neither side wears a suit, so no
job alias exists to show), and level is the murderer's new PK level.
================
*/
type murderNotice struct {
	division, killer, victim string
	level                    int
	nowMs                    int64
}

/*
================
murderNoticeOf

An open-field murder (pk.DeathPlayer) whose booking just counted. Read
in payPlayerKillInDoor after pk.RecordMurder, so the level is the one
this murder made; posted by postMurderNotice once the kill commits.
================
*/
func murderNoticeOf(division string, killer, victim *enterworld.Character, nowMs int64) (murderNotice, bool) {
	if killer.PK == nil {
		return murderNotice{}, false
	}
	return murderNotice{division, killer.Name, victim.Name, int(killer.PK.TotalCount), nowMs}, true
}

/*
================
murderNoticeText

#3151's line on the job-kill notice's pattern: "[<murderer>] murdered
[<victim>] (Murderer Lv N)", with MAX at the PK level's cap (pk.MaxTotal,
the total the client calls the PK level on 0x3647).
================
*/
func murderNoticeText(n murderNotice) string {
	level := "MAX"
	if n.level < pk.MaxTotal {
		level = strconv.Itoa(n.level)
	}
	return fmt.Sprintf("[%s] murdered [%s] (Murderer Lv %s)", n.killer, n.victim, level)
}

/*
================
Runtime.postMurderNotice

#3151 (port-only, not native): queues a committed open-field murder's
notice under the same controls as the job-kill notices - the
SRO_ANNOUNCE_JOB_KILL flag, the job-kill channel and per-minute cap, and
the pair's quiet window unless the repeat rule lifts it. A companion's
kill books the murder on its owner but posts nothing, as the job-kill
notice does (payCOSKillInDoor collects no murder notice).
================
*/
func (rt *Runtime) postMurderNotice(n murderNotice) {
	if !rt.Announce.JobKill {
		return
	}
	if !rt.Announce.JobKillRepeat && !rt.jobKillPairQuiet(n.killer, n.victim, n.nowMs) {
		return
	}
	rt.announce(n.division, announceJobKill, murderNoticeText(n), wire.NoticeLink{}, 0, n.nowMs)
}

/*
================
mostWantedNotice

A Most Wanted notice read inside the kill's door, while the suits are
worn, and posted by postMostWantedNotice once the kill commits.
================
*/
type mostWantedNotice struct {
	division, text string
	nowMs          int64
}

/*
================
Runtime.mostWantedLevelNotice

A killer's new wanted level, by job alias. Only a dressed job kill raises
the level, so the killer always has one.
================
*/
func (rt *Runtime) mostWantedLevelNotice(division string, killer *enterworld.Character, level, nowMs int64) []mostWantedNotice {
	job := enterworld.DressedJob(killer)
	if !rt.Announce.MostWanted || jobNames[job] == "" {
		return nil
	}
	return []mostWantedNotice{{division, fmt.Sprintf("[%s] is MOST WANTED Lv %d", jobKillAlias(killer, job), level), nowMs}}
}

/*
================
Runtime.mostWantedBountyNotice

A claimed bounty, by job alias. A bounty pays on any stranger PvP kill, so
the killer may wear no suit and have no alias: then the claim names the
victim alone (owner decision 2026-10-15), never a character. A victim
without a suit cannot be wanted, and is never announced.
================
*/
func (rt *Runtime) mostWantedBountyNotice(division string, killer, victim *enterworld.Character, level, nowMs int64) []mostWantedNotice {
	killerJob, victimJob := enterworld.DressedJob(killer), enterworld.DressedJob(victim)
	if !rt.Announce.MostWanted || jobNames[victimJob] == "" {
		return nil
	}
	victimAlias := jobKillAlias(victim, victimJob)
	text := fmt.Sprintf("Bounty of [%s] MOST WANTED Lv %d was claimed", victimAlias, level)
	if jobNames[killerJob] != "" {
		text = fmt.Sprintf("[%s] claimed bounty of [%s] MOST WANTED Lv %d", jobKillAlias(killer, killerJob), victimAlias, level)
	}
	return []mostWantedNotice{{division, text, nowMs}}
}

/*
================
Runtime.postMostWantedNotice

Queues a committed Most Wanted notice on the job-kill channel, under its
own per-minute cap.
================
*/
func (rt *Runtime) postMostWantedNotice(n mostWantedNotice) {
	if !rt.Announce.MostWanted {
		return
	}
	rt.announce(n.division, announceMostWanted, n.text, wire.NoticeLink{}, 0, n.nowMs)
}

// noticeMaxUnits is wire.NotificationFrame's UTF-16 budget (client 753A6F).
const noticeMaxUnits = 100

/*
================
clampNotice

Cuts a notice to the notification packet's budget, which panics past it:
a long player, item and monster name together can exceed it.
================
*/
func clampNotice(text string) string {
	runes := []rune(text)
	for len(utf16.Encode(runes)) > noticeMaxUnits {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
