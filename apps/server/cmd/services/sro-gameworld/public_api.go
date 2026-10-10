/*
===========================================================================

public_api.go - wiring the community site's public read API (port-only)

Starts publicstats on its own loopback listener (SRO_PUBLIC_API_ADDR,
"off" disables it), hands it read-only sources, and records unique kills:
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

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
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
}

/*
================
installPublicAPI

Wires the recorder and starts the listener. A listener that cannot start is
logged and skipped: the site API never stops the game from booting.
================
*/
func installPublicAPI(gameplay *gameplayPlane, hub *transport.Hub, authority *store.Store, shard string) *publicAPI {
	api := &publicAPI{kills: make(chan domain.UniqueKill, uniqueKillQueue), done: make(chan struct{})}
	go api.recordKills(authority, shard)
	gameplay.items.RecordUniqueKill = func(division string, kill domain.UniqueKill) {
		if division != shard {
			return
		}
		select {
		case api.kills <- kill:
		default:
			log.WithField("unique", kill.RefObjID).Warn("public api: unique-kill queue full; kill not recorded")
		}
	}

	addr := strings.TrimSpace(os.Getenv(publicstats.EnvAddr))
	if strings.EqualFold(addr, "off") {
		log.Infof("public api: disabled (%s=off)", publicstats.EnvAddr)
		return api
	}
	bridge := worldsession.New(hub)
	monsters := gameplay.deps.MonsterState
	service := publicstats.New(publicstats.Sources{
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
		Kills: func(sinceMs int64) ([]domain.UniqueKill, error) { return authority.UniqueKills(shard, sinceMs) },
		Uniques: func() []simulation.UniqueState {
			if monsters == nil {
				return nil
			}
			return monsters.UniqueStates(shard)
		},
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
			return setPublicHidden(authority, shard, account, name, hidden)
		},
		WriteToken: readPublicWriteToken(),
	})
	if err := service.Start(addr); err != nil {
		log.WithError(err).Warn("public api: listener not started; the community site reads its cache")
		return api
	}
	api.service = service
	log.Infof("public api: serving on %s", service.Addr())
	return api
}

/*
================
setPublicHidden

The privacy write. The owner check and the write share one store door, so
a character deleted or transferred meanwhile is refused, not written.
================
*/
func setPublicHidden(authority *store.Store, shard, account, name string, hidden bool) error {
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
	authority.UpdateCharacter(target, "public-hidden", func() bool {
		if target.AccountID != account || target.DeletePending {
			return false
		}
		owned = true
		if target.PublicHidden == hidden {
			return false
		}
		target.PublicHidden = hidden
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

The write token file, or "" (write disabled) when unset or unreadable.
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
	return strings.TrimSpace(string(payload))
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
		close(api.kills)
		<-api.done
	})
	return err
}
