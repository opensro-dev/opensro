/*
===========================================================================

announce.go - shard notices for a seal drop, a high plus and a job kill

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
Each kind has its own flag, off by default here (native: no notice); the
GameWorld's Nomad job turns them on:

	SRO_ANNOUNCE_SOX_DROP  "<player> has dropped <item> (<seal>) from <monster>"
	SRO_ANNOUNCE_PLUS      "<player> advanced <item> to +N" (N >= 7)
	SRO_ANNOUNCE_JOB_KILL  "[<killer alias>] killed [<victim alias>]" (job aliases, never character names)
	SRO_ANNOUNCE_MOST_WANTED  "[<alias>] is MOST WANTED LV <N>" on a new wanted level, and
	                          "[<killer alias>] claimed bounty of [<victim alias>] MOST WANTED LV <N>"

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
	"strings"
	"sync"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
)

// The three announcement flags ("on", "1", "true").
const (
	EnvAnnounceSoxDrop = "SRO_ANNOUNCE_SOX_DROP"
	EnvAnnouncePlus    = "SRO_ANNOUNCE_PLUS"
	EnvAnnounceJobKill = "SRO_ANNOUNCE_JOB_KILL"
	// EnvJobKillNoticeRepeat announces every kill of a pair (no window).
	EnvJobKillNoticeRepeat = "SRO_JOB_KILL_NOTICE_REPEAT"
	// EnvAnnounceMostWanted announces wanted levels and claimed bounties
	// (owner decision 2026-10-15, for the balance advisor).
	EnvAnnounceMostWanted = "SRO_ANNOUNCE_MOST_WANTED"
)

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
)

// sealCodenameSuffix marks a v1.150 rare (Seal of Star/Moon/Sun) item.
const sealCodenameSuffix = "_RARE"

// sealNoticeNames name a sealed item's tier in the drop notice with the
// client's own words: the English PARAM_RARE_FIRST/SECOND/THIRD rows of
// textuisystem that ItemTooltip_AppendRareGrade (558FF0) shows, indexed by
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
	JobKillRepeat          bool
	MostWanted             bool
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
	return Announcements{SoxDrop: on(EnvAnnounceSoxDrop), Plus: on(EnvAnnouncePlus), JobKill: on(EnvAnnounceJobKill),
		JobKillRepeat: on(EnvJobKillNoticeRepeat), MostWanted: on(EnvAnnounceMostWanted)}
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
2-9 seal or advanced item is unknown on most viewers).
================
*/
func (rt *Runtime) announce(division string, kind announceKind, text string, link wire.NoticeLink, refObjID uint32, nowMs int64) {
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
		return
	}
	a.windows[key] = append(recent, nowMs)
	text = clampNotice(text)
	// Port-only: each kind has its own channel, so a player can hide it
	// (wire.JobKillNoticeChannel, wire.DropPlusNoticeChannel).
	frame := wire.DropPlusNotificationFrame(text, link)
	if kind == announceJobKill || kind == announceMostWanted {
		frame = wire.JobKillNotificationFrame(text)
	}
	frames := make([]simulation.Frame, 0, 2)
	for _, ref := range rt.itemReferencesByID([]uint32{refObjID}) {
		frames = append(frames, simulation.Frame{Opcode: ref.Opcode, Payload: ref.Payload})
	}
	frames = append(frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
	a.queued = append(a.queued, simulation.DivisionFrames{DivisionID: division, Frames: frames})
	a.feed = append(a.feed, announcedLine{division, text})
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
			rt.PublicChat.Record(line.division, chatfeed.KindNotice, "", "", line.text)
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
		if items != nil {
			if ref, found := items.ItemRefByCodename(drop.Codename); found {
				if degree := enterworld.EquipmentDegree(ref); degree > 0 && degree < minDegree {
					continue
				}
				if ref.Name != "" {
					name = ref.Name
					if tier, sealed := enterworld.EquipmentSeal(ref); sealed {
						name += " (" + sealNoticeNames[tier] + ")"
					}
				}
			}
		}
		link := wire.NoticeLink{Player: drop.DroppedBy, Body: groundInventoryItem(drop, groundStack(drop)).Body().Encode()}
		rt.announce(division, announceSoxDrop, drop.DroppedBy+" has dropped "+name+" from "+monsterName, link, drop.RefObjID, nowMs)
	}
}

/*
================
Runtime.announcePlus

Item 2: a reinforcement that succeeded into +7 or above, after its commit,
naming a sealed item's seal as the drop notice does.
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
		text := fmt.Sprintf("%s advanced %s%s to +%d", player, name, seal, item.Plus)
		// The seal is the part to give up: clamping would cut the plus off
		// the end instead.
		if len(utf16.Encode([]rune(text))) > noticeMaxUnits {
			text = fmt.Sprintf("%s advanced %s to +%d", player, name, item.Plus)
		}
		rt.announce(division, announcePlus, text, wire.NoticeLink{Player: player, Body: item.Body().Encode()}, item.RefObjID, nowMs)
		return
	}
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
	return []mostWantedNotice{{division, fmt.Sprintf("[%s] is MOST WANTED LV %d", jobKillAlias(killer, job), level), nowMs}}
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
	text := fmt.Sprintf("Bounty of [%s] MOST WANTED LV %d was claimed", victimAlias, level)
	if jobNames[killerJob] != "" {
		text = fmt.Sprintf("[%s] claimed bounty of [%s] MOST WANTED LV %d", jobKillAlias(killer, killerJob), victimAlias, level)
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

// noticeMaxUnits is wire.NotificationFrame's UTF-16 budget (753A6F).
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
