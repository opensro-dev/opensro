/*
===========================================================================

player_operations.go - console composition for the running shard authority

An optional private credential enables this surface. Recovery first evicts
the selected character and acquires the existing binding-control lease;
other players and the GameWorld process continue running.

===========================================================================
*/
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/transport"
)

/*
================
installPlayerOperations
================
*/
func installPlayerOperations(api *agentapi.API, game *gameplayPlane, hub *transport.Hub, authority *store.Store, shard string) error {
	state := store.DirForShardFromEnv(shard)
	bytes, err := os.ReadFile(filepath.Join(state, "operator-token"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	read := func(name string) (any, error) {
		if name == "" {
			return map[string]any{"towns": game.items.OperatorTowns()}, nil
		}
		player, err := game.items.OperatorCharacter(shard, name)
		if err != nil {
			return nil, err
		}
		session, bound := hub.BoundSession(shard + ":" + strings.ToLower(name))
		player["bound"] = bound
		if bound {
			player["session"] = map[string]any{"id": session.ID, "ready": session.WorldReady(), "evicted": session.Evicted()}
		}
		return map[string]any{"shard": shard, "capturedAt": time.Now().UTC().Format(time.RFC3339Nano), "player": player,
			"towns": game.items.OperatorTowns(), "storage": authority.Health()}, nil
	}
	return api.InstallPlayerOperations(agentapi.PlayerOperations{
		Token: strings.TrimSpace(string(bytes)), AuditPath: filepath.Join(state, "operator-audit.jsonl"), Read: read,
		GrantItems: func(request agentapi.PlayerOperation) (any, error) {
			if authority.Health().LastError != "" {
				return nil, fmt.Errorf("storage is unhealthy; item grant refused")
			}
			grants := make([]inventory.ItemAmount, len(request.Items))
			for i, item := range request.Items {
				grants[i] = inventory.ItemAmount{Codename: item.Codename, Count: item.Count}
			}
			if err := game.items.OperatorGrantItems(shard, request.Character, grants); err != nil {
				return nil, err
			}
			if authority.Health().LastError != "" {
				return nil, fmt.Errorf("item grant persistence failed; inspect storage before retrying")
			}
			return read(request.Character)
		},
		Rescue: func(request agentapi.PlayerOperation) (any, error) {
			valid := false
			for _, town := range game.items.OperatorTowns() {
				if town.ID == request.Town {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("unknown rescue town")
			}
			if authority.Health().LastError != "" {
				return nil, fmt.Errorf("storage is unhealthy; rescue refused")
			}
			key := shard + ":" + strings.ToLower(request.Character)
			deadline := time.Now().Add(5 * time.Second)
			var lease *transport.BindingControlLease
			for {
				var acquired bool
				lease, acquired = hub.AcquireBindingControl(key)
				if acquired {
					break
				}
				if time.Now().After(deadline) {
					return nil, fmt.Errorf("character session is still closing; inspect before retrying")
				}
				time.Sleep(50 * time.Millisecond)
			}
			defer lease.Release()
			if err := game.items.OperatorRescue(shard, request.Character, request.Town); err != nil {
				return nil, err
			}
			if authority.Health().LastError != "" {
				return nil, fmt.Errorf("rescue persistence failed; inspect storage health")
			}
			return read(request.Character)
		},
	})
}
