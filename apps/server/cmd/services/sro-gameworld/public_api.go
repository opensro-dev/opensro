/*
===========================================================================

public_api.go - wiring the community site's public read API (port-only)

Starts publicstats on its own loopback listener (SRO_PUBLIC_API_ADDR,
"off" disables it), hands it read-only sources (characters, kills, uniques,
the online count and tick counters, the fortress and maintenance calendar),
and records unique kills:
the action runtime's hook only enqueues (it runs inside a character door,
where a store call would re-enter the door), and one goroutine commits the
queue to the authority store. Shutdown closes the listener and drains the
queue before the store closes.

Port-only, not native: the original has no public read API.

===========================================================================
*/
package main

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/sealboard"
	"opensro.online/server/internal/transport"
	"opensro.online/server/internal/transport/worldsession"
)

// uniqueKillQueue bounds the kills waiting for their store commit.
const uniqueKillQueue = 256

// maxPublicWriteTokenBytes bounds the token file.
const maxPublicWriteTokenBytes = 4 << 10

/*
================
publicAPI

The running service and the kill recorder it shares a lifetime with.
================
*/
type publicAPI struct {
	service *publicstats.Service
	kills   chan domain.UniqueKill
	done    chan struct{}
	once    sync.Once

	// mu guards closed: the hook never sends on the queue once Close has
	// closed it, whatever order shutdown runs in.
	mu     sync.Mutex
	closed bool
}

/*
================
installPublicAPI

Wires the recorder and starts the listener. A listener that cannot start is
logged and skipped: the site API never stops the game from booting.
================
*/
func installPublicAPI(gameplay *gameplayPlane, hub *transport.Hub, authority *store.Store, shard string, sealBoard *sealboard.Service) *publicAPI {
	api := &publicAPI{kills: make(chan domain.UniqueKill, uniqueKillQueue), done: make(chan struct{})}
	go api.recordKills(authority, shard)
	gameplay.items.RecordUniqueKill = func(division string, kill domain.UniqueKill) {
		if division != shard {
			return
		}
		api.enqueue(kill)
	}

	addr := strings.TrimSpace(os.Getenv(publicstats.EnvAddr))
	if strings.EqualFold(addr, "off") {
		log.Infof("public api: disabled (%s=off)", publicstats.EnvAddr)
		return api
	}
	service := publicstats.New(publicSources(gameplay, hub, authority, shard, sealBoard))
	if err := service.Start(addr); err != nil {
		log.WithError(err).Warn("public api: listener not started; the community site reads its cache")
		return api
	}
	api.service = service
	log.Infof("public api: serving on %s", service.Addr())
	return api
}

// publicWindowEvents names the fortress lane's windows on the calendar.
var publicWindowEvents = map[string]struct{ kind, title string }{
	"AllowSiegeRequest": {"fortress-registration", "Fortress war registration"},
	"SiegeProgressing":  {"fortress", "Fortress war"},
}

/*
================
publicSchedule

P8's events: the fortress registration and war windows from the lane that
runs them, and a maintenance release while its gate file is present (the
release tool writes it at the release's start and removes it at the end,
so the start is the file's time and the end is open).
================
*/
func publicSchedule(lane *siege.Lane, now time.Time) []publicstats.ScheduledEvent {
	var events []publicstats.ScheduledEvent
	if path := strings.TrimSpace(os.Getenv(agentapi.EnvMaintenanceGate)); path != "" {
		if info, err := os.Stat(path); err == nil {
			events = append(events, publicstats.ScheduledEvent{Kind: "maintenance", Title: "Maintenance", Start: info.ModTime(), Running: true})
		}
	}
	for _, window := range lane.Windows(now.UnixMilli()) {
		names, ok := publicWindowEvents[window.Name]
		if !ok {
			continue
		}
		events = append(events, publicstats.ScheduledEvent{Kind: names.kind, Title: names.title, Start: window.Start,
			End: window.End, Running: window.Running})
	}
	return events
}

/*
================
setPrivacyFlag

A privacy write (the profile's hidden flag, or its gear's). The owner
check and the write share one store door, so a character deleted or
transferred meanwhile is refused, not written.
================
*/
func setPrivacyFlag(
	authority *store.Store,
	shard, account, name, reason string,
	hidden bool,
	flag func(*domain.Character) *bool,
) error {
	var target *domain.Character
	authority.ReadCharacters(shard, func(characters []*domain.Character) {
		for _, c := range characters {
			if strings.EqualFold(c.Name, name) {
				target = c
				return
			}
		}
	})
	if target == nil {
		return publicstats.ErrNotOwned
	}
	owned := false
	authority.UpdateCharacter(target, reason, func() bool {
		if target.AccountID != account || target.DeletePending {
			return false
		}
		owned = true
		if *flag(target) == hidden {
			return false
		}
		*flag(target) = hidden
		return true
	})
	if !owned {
		return publicstats.ErrNotOwned
	}
	if authority.Health().LastError != "" {
		return errors.New("privacy write not persisted")
	}
	return nil
}

/*
================
readPublicWriteToken

The write token file, or "" (write disabled) when unset, unreadable or
shorter than publicstats.MinWriteTokenBytes.
================
*/
func readPublicWriteToken() string {
	path := strings.TrimSpace(os.Getenv(publicstats.EnvTokenPath))
	if path == "" {
		return ""
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxPublicWriteTokenBytes {
		log.WithField("path", path).Warn("public api: write token unreadable; privacy write disabled")
		return ""
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		log.WithError(err).Warn("public api: write token unreadable; privacy write disabled")
		return ""
	}
	token := strings.TrimSpace(string(payload))
	if len(token) < publicstats.MinWriteTokenBytes {
		// Also catches a template that rendered "<no value>" for a missing item.
		if token != "" {
			log.WithField("path", path).Warn("public api: write token too short; privacy write disabled")
		}
		return ""
	}
	return token
}

/*
================
enqueue

Never blocks the tick: a full queue or a closed recorder drops the kill
with a warning.
================
*/
func (api *publicAPI) enqueue(kill domain.UniqueKill) {
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.closed {
		log.WithField("unique", kill.RefObjID).Warn("public api: recorder closed; kill not recorded")
		return
	}
	select {
	case api.kills <- kill:
	default:
		log.WithField("unique", kill.RefObjID).Warn("public api: unique-kill queue full; kill not recorded")
	}
}

/*
================
recordKills

Commits each queued kill until Close drains the queue.
================
*/
func (api *publicAPI) recordKills(authority *store.Store, shard string) {
	defer close(api.done)
	for kill := range api.kills {
		if _, err := authority.RecordUniqueKill(shard, kill); err != nil {
			log.WithError(err).WithField("unique", kill.RefObjID).Warn("public api: unique kill not recorded")
		}
	}
}

/*
================
Close

Stops the listener, then lets the recorder commit what is queued.
================
*/
func (api *publicAPI) Close() error {
	var err error
	api.once.Do(func() {
		if api.service != nil {
			err = api.service.Close()
		}
		api.mu.Lock()
		api.closed = true
		close(api.kills)
		api.mu.Unlock()
		<-api.done
	})
	return err
}

/*
================
publicGuildEntries

Every guild of the division for the public guild list: the guild ids the
characters carry, each guild read through its door (as GuildOf does), the
other guilds of its union from the union door and the fortress it holds.
A failed union or fortress read leaves those fields empty, not the list.
================
*/
func publicGuildEntries(authority *store.Store, shard string) []publicstats.GuildEntry {
	ids := map[int64]bool{}
	authority.ReadCharacters(shard, func(characters []*domain.Character) {
		for _, c := range characters {
			if c.GuildID != nil {
				ids[*c.GuildID] = true
			}
		}
	})
	unions := map[int64][]int64{}
	if alliances, err := authority.Alliances().Alliances(shard); err == nil {
		for _, alliance := range alliances {
			for _, guild := range alliance.Guilds {
				for _, other := range alliance.Guilds {
					if guild != 0 && other != 0 && other != guild {
						unions[guild] = append(unions[guild], other)
					}
				}
			}
		}
	}
	fortresses := map[int64]string{}
	if records, _, err := authority.Fortresses().FortressState(shard); err == nil {
		towns := map[uint32]string{}
		for _, row := range enterworld.DefaultSiegeFortressDataRows() {
			towns[row.FortressID] = strings.ToLower(strings.TrimPrefix(row.CodeName, "FORTRESS_"))
		}
		for _, record := range records {
			if record.GuildID != 0 {
				fortresses[record.GuildID] = towns[record.FortressID]
			}
		}
	}
	out := make([]publicstats.GuildEntry, 0, len(ids))
	for id := range ids {
		guild, members, ok := authority.Guilds().Guild(shard, id)
		if !ok {
			continue
		}
		entry := publicstats.GuildEntry{ID: id, Name: guild.Name, Level: guild.Level, Union: unions[id],
			Fortress: fortresses[id], FoundedAtMs: guild.FoundedAtMs}
		for _, member := range members {
			entry.Members = append(entry.Members, publicstats.GuildEntryMember{CharID: member.CharID, Grade: member.Grade})
		}
		out = append(out, entry)
	}
	return out
}

/*
================
publicSources

Everything the public API reads, wired to this GameWorld's authorities.
A builder of its own so a test can hold each wiring (the unique names
went unwired once, and every name printed as its codename).
================
*/
func publicSources(gameplay *gameplayPlane, hub *transport.Hub, authority *store.Store, shard string, sealBoard *sealboard.Service) publicstats.Sources {
	bridge := worldsession.New(hub)
	monsters := gameplay.deps.MonsterState
	startedAt := time.Now()
	sources := publicstats.Sources{
		Characters: func() []*domain.Character {
			var out []*domain.Character
			authority.ReadCharacters(shard, func(characters []*domain.Character) {
				out = make([]*domain.Character, 0, len(characters))
				for _, c := range characters {
					out = append(out, c.Snapshot())
				}
			})
			return out
		},
		Kills:    func(afterSeq int64) ([]domain.UniqueKill, error) { return authority.UniqueKillsAfter(shard, afterSeq) },
		KillPage: store.UniqueKillPage,
		Uniques: func() []simulation.UniqueState {
			if monsters == nil {
				return nil
			}
			return monsters.UniqueStates(shard)
		},
		UniqueName: uniqueNameSource(monsters),
		Guilds:     func() []publicstats.GuildEntry { return publicGuildEntries(authority, shard) },
		GuildOf: func(c *domain.Character) (string, string) {
			if c.GuildID == nil {
				return "", ""
			}
			guild, members, ok := authority.Guilds().Guild(shard, *c.GuildID)
			if !ok {
				return "", ""
			}
			rank := "member"
			for _, member := range members {
				if member.CharID == c.ID && member.Grade == 0 {
					rank = "master"
				}
			}
			return guild.Name, rank
		},
		HasDurability: func(refObjID uint32) bool {
			// Without item references nothing can be told apart, so every
			// item keeps its durability as before; with them, a reference the
			// itemdata does not know has no Dur_U and so no durability.
			items := gameplay.deps.ItemReferences()
			if items == nil {
				return true
			}
			ref, ok := items.ItemRefByID(refObjID)
			return ok && ref != nil && ref.MaxDurability > 0
		},
		ItemGrade: func(refObjID uint32) (int, int, bool) {
			items := gameplay.deps.ItemReferences()
			if items == nil {
				return 0, 0, false
			}
			ref, ok := items.ItemRefByID(refObjID)
			if !ok || ref == nil {
				return 0, 0, false
			}
			tier, sealed := enterworld.EquipmentSeal(ref)
			return enterworld.EquipmentDegree(ref), tier, sealed
		},
		ModelRef: gameplay.deps.CharacterModelRef,
		Online: func(characterID int64) bool {
			for _, view := range bridge.SnapshotSessionViews() {
				if view.DivisionID == shard && view.CharacterID == characterID {
					return true
				}
			}
			return false
		},
		Rules: publicRules(),
		Shard: "beta",
		SetHidden: func(account, name string, hidden bool) error {
			return setPrivacyFlag(authority, shard, account, name, "public-hidden", hidden,
				func(c *domain.Character) *bool { return &c.PublicHidden })
		},
		SetGearHidden: func(account, name string, hidden bool) error {
			return setPrivacyFlag(authority, shard, account, name, "public-gear-hidden", hidden,
				func(c *domain.Character) *bool { return &c.PublicGearHidden })
		},
		SetBreakNotice: func(account, name string, on bool) error {
			return setPrivacyFlag(authority, shard, account, name, "break-notice", on,
				func(c *domain.Character) *bool { return &c.BreakNotice })
		},
		WriteToken: readPublicWriteToken(),
		Sample: func() (int, transport.Metrics) {
			online := 0
			for _, view := range bridge.SnapshotSessionViews() {
				if view.DivisionID == shard && view.CharacterID != 0 {
					online++
				}
			}
			return online, hub.Metrics()
		},
		StartedAt:            startedAt,
		MasteryTotalOverride: int(gameplay.deps.MasteryTotalOverride),
		ExpRequired: func(level int64) (int64, bool) {
			levels := gameplay.deps.LevelData()
			if levels == nil {
				return 0, false
			}
			return levels.ExpRequired(level)
		},
		Schedule: func(now time.Time) []publicstats.ScheduledEvent { return publicSchedule(gameplay.siege, now) },
		// A muted author's lines are left out while the mute holds, on or
		// off line: the by-character restriction record is read directly
		// (Hub.CharacterRestricted). restriction.Report would also send
		// the muted player its notice on every poll.
		Chat: func() []chatfeed.Line {
			return gameplay.chatFeed.Lines(shard, func(author string) bool {
				return hub.CharacterRestricted(shard, author, transport.CommandRestrictionChat)
			})
		},
		Events: func() []chatfeed.Line { return gameplay.chatFeed.Events(shard) },
	}
	// The weekly seal board (#1641): a nil counter leaves the route
	// unserved (404), which is SRO_SEAL_BOARD off.
	if sealBoard != nil {
		sources.SealCounts = sealBoard.Counts
		sources.SealCountingSince = sealBoard.CountingSince
	}
	return sources
}

/*
================
uniqueNameSource

Every unique name the API prints (the board, the kill feed, world-firsts)
goes through this: the monster's retail display name. Unwired, each fell
back to its codename (MOB_OA_URUCHI) on the site.
================
*/
func uniqueNameSource(monsters *simulation.MonsterState) func(uint32) string {
	return func(refObjID uint32) string {
		if monsters == nil {
			return ""
		}
		return monsters.MonsterName(refObjID)
	}
}
