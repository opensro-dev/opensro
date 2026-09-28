/*
===========================================================================

notices.go - wire operator notices to the existing in-world fanout

The bridge selects active scenes in this process's shard. The native item-wire
notification encoder already owns the text/banner packet used by the client.

===========================================================================
*/
package main

import (
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
	"opensro.online/server/internal/transport/worldsession"
)

/*
================
installOperatorNotices
================
*/
func installOperatorNotices(api *agentapi.API, hub *transport.Hub, shardID string) {
	bridge := worldsession.New(hub)
	api.InstallNoticePublisher(func(message string) {
		frame := wire.NotificationFrame(message)
		bridge.PushToDivision(shardID, []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload}}, "")
	})
}
