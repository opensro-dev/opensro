/*
===========================================================================

playermap.go - the beta world map roster leg (operator switch, not native)

With SRO_BETA_PLAYER_MAP=on (the Nomad job's beta_player_map variable),
every playerMapIntervalMs each session in a division receives the ground
position of every player in that division (wire.OpBetaPlayerMap), so the
world map (M) can show them all. Off, nothing is sent and the map shows
what retail shows.

===========================================================================
*/
package simulation

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/item/wire"
)

// EnvBetaPlayerMap enables the beta world map roster.
const EnvBetaPlayerMap = "SRO_BETA_PLAYER_MAP"

// playerMapIntervalMs paces the roster push: the world map is a slow view.
const playerMapIntervalMs = 2000

/*
================
BetaPlayerMapEnabled
================
*/
func BetaPlayerMapEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaPlayerMap))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
runPlayerMap

One roster frame per session, from the same snapshots the peer visibility
leg reads. A session without appearance (never spawned) is left out.
================
*/
func (t *Ticker) runPlayerMap(state *divisionTickState, nowMs int64, sessions []SessionSnapshot) {
	if !t.PlayerMap || nowMs < state.playerMapDueMs {
		return
	}
	state.playerMapDueMs = nowMs + playerMapIntervalMs
	players := make([]wire.BetaMapPlayer, 0, len(sessions))
	for i := range sessions {
		session := &sessions[i]
		if session.Appearance == nil {
			continue
		}
		pose := session.World.LiveSpawnAt(nowMs)
		players = append(players, wire.BetaMapPlayer{
			Gid:      PlayerObjectID(session.CharacterID),
			RegionID: pose.RegionID,
			X:        float32(pose.X),
			Z:        float32(pose.Z),
			Name:     session.Appearance.Name,
		})
	}
	payload := wire.EncodeBetaPlayerMap(players)
	for i := range sessions {
		t.Push.PushToSession(sessions[i].SessionID, []Frame{{Opcode: wire.OpBetaPlayerMap, Payload: payload}})
	}
}
