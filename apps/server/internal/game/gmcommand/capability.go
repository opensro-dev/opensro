/*
===========================================================================

capability.go - which staff capability each GM subcommand needs

Port-only, not native; the default by owner decision 2026-11-23. The
original opens every subcommand with the one GM bit. Staff grants split
it (package staff): HandleGmCommand looks the subcommand up here after
the privilege gate and refuses one whose capability the sender's grant
does not carry, with the command's own failure ack. The check reads the
sender's live record, so an unticked box or a revoke applies to the next
command.

Every subcommand of wire.go has a row; capability_test.go fails when one
is added without it. A subcommand with no row is refused.

===========================================================================
*/
package gmcommand

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/staff"
)

// subcommandCapability maps each 0x75B6 subcommand to its capability.
var subcommandCapability = map[uint8]staff.Capability{
	SubFindUser: staff.FindUser,
	SubLieName:  staff.FindUser,
	SubRealName: staff.FindUser,

	SubWarp:       staff.TeleportSelf,
	SubGoTown:     staff.TeleportSelf,
	SubMoveToUser: staff.TeleportSelf,
	SubMoveToNPC:  staff.TeleportSelf,
	SubSetSpeed:   staff.Speed,

	SubInvisible:  staff.Invisible,
	SubInvincible: staff.Invincible,

	SubToTown:      staff.MovePlayers,
	SubRecallUser:  staff.MovePlayers,
	SubRecallGuild: staff.MovePlayers,
	SubBan:         staff.Ban,

	SubWorldStatus: staff.WorldControl,
	SubSetTime:     staff.WorldControl,
	SubFlagWorld:   staff.WorldControl,
	SubOpenWorld:   staff.WorldControl,
	SubWeather:     staff.WorldControl,

	SubMakeItem:    staff.MakeItem,
	SubLoadMonster: staff.SpawnMonster,
	SubMobKill:     staff.KillMonster,
	SubZoe:         staff.KillMonster,
	SubGrantSilk:   staff.GrantSilk,
	SubForge:       staff.Forge,
	SubSetBerserk:  staff.Berserk,
	SubSetGauge:    staff.Berserk,

	SubInitQ:   staff.Quests,
	SubResetQ:  staff.Quests,
	SubCompQ:   staff.Quests,
	SubRemoveQ: staff.Quests,

	SubSiegeStructure: staff.Siege,
	SubSiegePeriod:    staff.Siege,

	SubChatMute:     staff.Mute,
	SubChatUnmute:   staff.Mute,
	SubShadowMute:   staff.Mute,
	SubShadowUnmute: staff.Mute,
	SubBlockLogout:  staff.HoldLogout,
	SubAllowLogout:  staff.HoldLogout,

	SubNotice:    staff.Notice,
	SubNameplate: staff.Nameplate,
}

/*
================
capabilityAllows

Whether the sender's grant carries the subcommand's capability. An
unmapped subcommand needs a capability nobody holds.
================
*/
func capabilityAllows(sender *enterworld.Character, subcmd uint8) bool {
	capability, ok := subcommandCapability[subcmd]
	return ok && staff.Allows(sender, capability)
}
