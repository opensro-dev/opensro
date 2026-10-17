/*
===========================================================================

life.go - the bot's life state and the self-rebirth request

The server flips an entity's life channel with a 0x3122 refresh: LIFE-dead
when the character dies and LIFE-alive after a rebirth. The brain must know
the dead state, because the server refuses every move and every action of a
corpse with reason "characterDead". The bot answers a death with the 0x32DC
self-rebirth request.

Phase 2 of issue #74 (issue #76).

===========================================================================
*/
package bot

import "opensro.online/server/internal/game/item/wire"

// OpcodeObjectStateRefresh is the 0x3122 discrete state channel refresh.
const OpcodeObjectStateRefresh = wire.OpObjectStateRefresh

// Life is the bot's own life state from a 0x3122 refresh.
type Life struct {
	GID  uint32
	Dead bool
}

// ParseLife decodes a 0x3122 life-channel refresh. It returns ok=false for
// another channel or a short body.
func ParseLife(payload []byte) (Life, bool) {
	refresh, err := wire.DecodeObjectStateRefresh(payload)
	if err != nil || refresh.StateType != wire.StateChannelLife {
		return Life{}, false
	}
	return Life{GID: refresh.Gid, Dead: refresh.Value == wire.LifeStateDead}, true
}

// OpcodeLocalRebirthRequest is the 0x32DC self-rebirth request.
const OpcodeLocalRebirthRequest = wire.OpLocalRebirthRequest

// EncodeLocalRebirth builds the 0x32DC body. Choice 2 is the present-point
// rebirth the retail client offers at level 10 and below.
func EncodeLocalRebirth() []byte {
	return []byte{wire.RebirthAtPresentPoint}
}
