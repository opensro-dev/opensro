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
leg reads. A session without appearance (never spawned) is left out, and
so is a GM in /INVISIBLE (body status 4).

The roster is port-only (wire.OpBetaPlayerMap): the v1.150 world map draws
only the local player and the party, so no native rule covers it. A
GM-invisible character spawns for its party only (peer_visibility.go,
gmInvisibleBodyStatus), so a roster that drew it for everyone would give
away what the spawn leg hides. Party members still see the GM through the
map's native party signs, so the roster leaves the GM out for every
recipient instead of building a frame per viewer. The frame is rebuilt
every playerMapIntervalMs from the live status, so the marker returns on
the first frame after /INVISIBLE is turned off. The GM's own position
stays on their map: the client draws the local player from its own pose
and skips its gid in this roster.
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
		if session.Appearance == nil || session.NativeBodyStatus == gmInvisibleBodyStatus {
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
