/*
===========================================================================

observatory.go - local operator snapshot wiring

Copies player, population, transport and storage diagnostics into one cached
operator view. Online player regions define the population's complete focus;
the capture never advances gameplay or changes authority state.

===========================================================================
*/

package main

import (
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
	"opensro.online/server/internal/transport/worldsession"
	"runtime"
	"runtime/metrics"
	"sort"
	"time"
)

/*
================
observatoryPlayer
================
*/
type observatoryPlayer struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Level  int64   `json:"level"`
	HP     int64   `json:"hp"`
	MP     int64   `json:"mp"`
	Region uint16  `json:"region"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Z      float64 `json:"z"`
	Alive  bool    `json:"alive"`
}

/*
================
captureObservatoryPlayer

Absent stored gauges mean full for diagnostic readers. Use the shared
record interpretation without inventing a gameplay stat-graph maximum.
================
*/
func captureObservatoryPlayer(c *domain.Character, session simulation.SessionSnapshot, now int64) observatoryPlayer {
	p := session.World.LiveSpawnAt(now)
	row := observatoryPlayer{
		ID: c.ID, Name: c.Name, Level: 1,
		HP: charactervitals.CurrentHP(c), MP: charactervitals.CurrentMP(c),
		Region: p.RegionID, X: p.X, Y: p.Y, Z: p.Z, Alive: session.CombatEligible,
	}
	if c.Level != nil {
		row.Level = *c.Level
	}
	return row
}

/*
================
installObservatory

Installs the full cached capture and the smaller process summary before the
local operator API begins serving requests.
================
*/
func installObservatory(api *agentapi.API, state *simulation.MonsterState, hub *transport.Hub, authority *store.Store, shard string) {
	started := time.Now()
	bridge := worldsession.New(hub)
	api.InstallObservatory(func() any {
		now := time.Now()
		sessions := bridge.SnapshotSessions()
		online := make(map[int64]simulation.SessionSnapshot, len(sessions))
		for _, session := range sessions {
			if session.DivisionID == shard {
				online[session.CharacterID] = session
			}
		}
		players := []observatoryPlayer{}
		registered := 0
		authority.ReadCharacters(shard, func(chars []*domain.Character) {
			registered = len(chars)
			for _, c := range chars {
				session, ok := online[c.ID]
				if !ok {
					continue
				}
				players = append(players, captureObservatoryPlayer(c, session, now.UnixMilli()))
			}
		})
		sort.Slice(players, func(i, j int) bool { return players[i].ID < players[j].ID })
		samples := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}, {Name: "/gc/heap/allocs:bytes"}, {Name: "/gc/cycles/total:gc-cycles"}, {Name: "/cpu/classes/gc/total:cpu-seconds"}}
		metrics.Read(samples)
		values := map[string]any{}
		for _, s := range samples {
			switch s.Value.Kind() {
			case metrics.KindUint64:
				values[s.Name] = s.Value.Uint64()
			case metrics.KindFloat64:
				values[s.Name] = s.Value.Float64()
			}
		}
		population := simulation.ObservatoryPopulation{Monsters: []simulation.ObservatoryMonster{}}
		if state != nil {
			// Monsters around the online players are never the rows the cap drops.
			focus := make([]uint16, 0, len(players))
			for _, player := range players {
				focus = append(focus, player.Region)
			}
			population = state.Observatory(shard, focus)
		}
		health := authority.Health()
		return map[string]any{"version": 1, "shard": shard, "capturedAt": now.UTC().Format(time.RFC3339Nano), "uptimeSeconds": time.Since(started).Seconds(), "players": players, "registeredCharacters": registered, "population": population, "transport": hub.Metrics(), "runtime": map[string]any{"goVersion": runtime.Version(), "goroutines": runtime.NumGoroutine(), "parallelism": runtime.GOMAXPROCS(0), "metrics": values}, "storage": health, "captureMs": time.Since(now).Seconds() * 1000}
	})
	api.InstallObservatorySummary(func() any {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		samples := []metrics.Sample{{Name: "/gc/heap/live:bytes"}, {Name: "/gc/gomemlimit:bytes"}}
		metrics.Read(samples)
		population := map[string]int{}
		if state != nil {
			population = state.DormancyStats(shard)
		}
		return map[string]any{"population": population, "memory": map[string]uint64{
			"heapLive": samples[0].Value.Uint64(), "memoryLimit": samples[1].Value.Uint64(),
			"heapAlloc": m.HeapAlloc, "heapSys": m.HeapSys, "heapIdle": m.HeapIdle,
			"heapReleased": m.HeapReleased, "stackSys": m.StackSys, "sys": m.Sys,
			"totalAlloc": m.TotalAlloc, "gcCycles": uint64(m.NumGC),
		}}
	})
}
