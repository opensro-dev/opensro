/*
===========================================================================

announce.go - shard notices for a seal drop, a high plus and a job kill

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
Each kind has its own flag, off by default here (native: no notice); the
GameWorld's Nomad job turns them on:

	SRO_ANNOUNCE_SOX_DROP  "<player> dropped <item> (<seal>) from <monster>"
	SRO_ANNOUNCE_PLUS      "<player> advanced <item> to +N" (N 8..11)
	SRO_ANNOUNCE_JOB_KILL  "[<killer alias>] killed [<victim alias>]" (job aliases, never character names)

A notice is the native notification packet the operator notices use
(wire.NotificationFrame), sent to every session of the division and kept
in the website's chat feed. Hooks post from inside character doors, so a
notice is queued and the next tick sends it, outside every door. Each kind
is capped shard-wide per minute, and a job kill repeats for the same pair
only after jobKillRepeatMs, so a farm cannot flood chat.

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
	"opensro.online/server/internal/game/world/simulation"
)

// The three announcement flags ("on", "1", "true").
const (
	EnvAnnounceSoxDrop = "SRO_ANNOUNCE_SOX_DROP"
	EnvAnnouncePlus    = "SRO_ANNOUNCE_PLUS"
	EnvAnnounceJobKill = "SRO_ANNOUNCE_JOB_KILL"
)

const (
	// announcementsPerMinute caps one kind of notice per division.
	announcementsPerMinute = 6
	announceWindowMs       = 60_000
	// jobKillRepeatMs is how long one killer/victim pair stays quiet.
	jobKillRepeatMs = 10 * 60_000
	// jobKillPairPrune is the pair count past which stale pairs are dropped.
	jobKillPairPrune = 1024
	// announcePlusMin and announcePlusMax bound the announced plus.
	announcePlusMin = 8
	announcePlusMax = 11
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
	announceKinds
)

/*
================
Announcements

Which notices the shard sends. The zero value is native: none.
================
*/
type Announcements struct {
	SoxDrop, Plus, JobKill bool
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
	return Announcements{SoxDrop: on(EnvAnnounceSoxDrop), Plus: on(EnvAnnouncePlus), JobKill: on(EnvAnnounceJobKill)}
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

Queues one notice when its kind is under the per-minute cap.
================
*/
func (rt *Runtime) announce(division string, kind announceKind, text string, nowMs int64) {
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
	if len(recent) >= announcementsPerMinute {
		a.windows[key] = recent
		return
	}
	a.windows[key] = append(recent, nowMs)
	text = clampNotice(text)
	frame := wire.NotificationFrame(text)
	if kind == announceJobKill {
		// Port-only: its own channel, so a player can hide job-kill
		// notices (wire.JobKillNoticeChannel).
		frame = wire.JobKillNotificationFrame(text)
	}
	a.queued = append(a.queued, simulation.DivisionFrames{DivisionID: division,
		Frames: []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload}}})
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
			rt.PublicChat.Record(line.division, chatfeed.KindNotice, "", line.text)
		}
	}
	return out
}

/*
================
Runtime.jobKillPairQuiet

Whether a killer/victim pair may be announced again, recording it when so.
================
*/
func (rt *Runtime) jobKillPairQuiet(killer, victim string, nowMs int64) bool {
	a := &rt.announcer
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pairs == nil {
		a.pairs = map[string]int64{}
	}
	key := strings.ToLower(killer) + "\x00" + strings.ToLower(victim)
	if at, seen := a.pairs[key]; seen && nowMs-at < jobKillRepeatMs {
		return false
	}
	if len(a.pairs) >= jobKillPairPrune {
		for pair, at := range a.pairs {
			if nowMs-at >= jobKillRepeatMs {
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
	items := rt.deps.ItemReferences()
	for _, drop := range drops {
		if drop.DroppedBy == "" || !strings.HasSuffix(drop.Codename, sealCodenameSuffix) {
			continue
		}
		name := drop.Codename
		if items != nil {
			if ref, found := items.ItemRefByCodename(drop.Codename); found && ref.Name != "" {
				name = ref.Name
				if tier, sealed := enterworld.EquipmentSeal(ref); sealed {
					name += " (" + sealNoticeNames[tier] + ")"
				}
			}
		}
		rt.announce(division, announceSoxDrop, drop.DroppedBy+" dropped "+name+" from "+monsterName, nowMs)
	}
}

/*
================
Runtime.announcePlus

Item 2: a reinforcement that succeeded into +8..+11, after its commit.
================
*/
func (rt *Runtime) announcePlus(division, player string, outcome alchemy.Outcome, nowMs int64) {
	if !rt.Announce.Plus || !outcome.Success {
		return
	}
	for _, item := range outcome.Items {
		if item.Slot != outcome.Target || item.Plus < announcePlusMin || item.Plus > announcePlusMax {
			continue
		}
		name := item.Codename
		if items := rt.deps.ItemReferences(); items != nil {
			if ref, found := items.ItemRefByCodename(item.Codename); found && ref.Name != "" {
				name = ref.Name
			}
		}
		rt.announce(division, announcePlus, fmt.Sprintf("%s advanced %s to +%d", player, name, item.Plus), nowMs)
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
[%s]."), unless the pair was announced within jobKillRepeatMs.
================
*/
func (rt *Runtime) postJobKillNotice(n jobKillNotice) {
	if !rt.Announce.JobKill || !rt.jobKillPairQuiet(n.killer, n.victim, n.nowMs) {
		return
	}
	rt.announce(n.division, announceJobKill, "["+n.killerAlias+"] killed ["+n.victimAlias+"]", n.nowMs)
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
