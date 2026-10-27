/*
===========================================================================

teleportprotection.go - the untouchable grace after a teleport arrival

Port-only, not native, behind SRO_TELEPORT_GRACE (off is native; owner
decision 2026-10-25). With the switch on, a successful return or reverse
return scroll, gate or server relocation arrives untouchable: body mode
2, the revival's state, for the revival's grace (6.0 s, 11.0 in a siege
world; spawnprotection.go). Monsters skip the player as a target and the
player cannot attack, exactly as after a revival.

Native evidence (the #1518 re-verify at assembly level): the original
grants this body mode after an arrival only on the revival family. The
town-teleport routine schedules it when its caller asks for the grant,
which revival and the timed-event return do and the GM move does not.
Scrolls and gates set the separate teleport-mode state instead, and no
other arrival path grants body mode 2. The v1.150 client never grants the
state itself; it shows the server's refusal code 32 notice (client 68A0A0). The
timed-event return's native grace is #1582, not this switch.

The grace starts at the arrival's game-ready, not at the travel commit, a
port adaptation: a loading session is outside the tick, so nothing can
reach it there, and a browser client's load can outlast the whole grace.
The arrival only marks the character; ReconcileScene grants the body mode
once the scene is live.

===========================================================================
*/

package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvTeleportGrace turns the post-teleport grace on ("on", "1", "true").
const EnvTeleportGrace = "SRO_TELEPORT_GRACE"

/*
================
TeleportGraceFromEnv
================
*/
func TeleportGraceFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTeleportGrace))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
noteTeleportArrival

The caller holds the division action door, after the travel's last rollback
point: a failed teleport never marks. A later arrival before game-ready
leaves one mark, so one grace follows the scene that finally goes live.
================
*/
func (rt *Runtime) noteTeleportArrival(division string, c *enterworld.Character) {
	if !rt.TeleportGrace {
		return
	}
	rt.teleportArrivals.Store(simulation.WorldKey(division, c.Name), struct{}{})
}

/*
================
startTeleportGrace

The game-ready half: a marked arrival becomes body mode 2 from now, through
the revival's grant and restore (grantReviveUntouchable), and is published
to the player and its peers. Any other scene going live, a login included,
finds no mark.
================
*/
func (rt *Runtime) startTeleportGrace(division, name string) {
	if _, marked := rt.teleportArrivals.LoadAndDelete(simulation.WorldKey(division, name)); !marked {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	character := rt.findCharacter(division, name)
	if character == nil {
		return
	}
	var frames []wire.Frame
	rt.deps.UpdateDeferred(character, "teleport-untouchable", func() bool {
		if character.DeletePending || !enterworld.CharacterAlive(character) {
			return false
		}
		frames = rt.grantReviveUntouchable(division, character, rt.Now().UnixMilli())
		return len(frames) > 0
	})
	rt.publishBodyStatus(division, name, frames)
}
