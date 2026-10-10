/*
===========================================================================

public_rules.go - the "Original vs OpenSRO" list the public API serves (P9)

Every player-facing port-only switch, read through the same function the
game reads it with, so the site shows the shard's live setting, plus the
deliberate deviations that have no switch. Operator-only switches (movement
clip telemetry, data paths) are not player-facing and are left out.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
publicRules

Built once at boot: the switches are read from the environment at startup,
so the list cannot change while the GameWorld runs.
================
*/
func publicRules() publicstats.RulesResponse {
	silkRate, _ := action.BetaSilkFromEnv()
	stackSizes, _ := enterworld.StackSizesFromEnv()
	return publicstats.RulesResponse{
		Rules: []publicstats.Rule{
			{Flag: progression.EnvBetaGrowth, On: progression.BetaGrowthFromEnv().Enabled,
				Native: "Retail experience, skill point and drop rates.",
				Now:    "Raised beta rates for experience, skill points and drops.",
				Why:    "A beta lasts weeks, not years; testers need to reach the content."},
			{Flag: action.EnvBetaSilk, On: silkRate > 0,
				Native: "Silk is bought for the account.",
				Now:    "Every full hour in the world earns silk, up to a cap.",
				Why:    "Lets testers try the Item Mall; beta silk never reaches the launch shard."},
			{Flag: enterworld.EnvBetaStarterKit, On: enterworld.BetaStarterKitEnabled(),
				Native: "Characters start with the retail creation items only.",
				Now:    "Every character also holds a return scroll and a speed scroll that are never used up.",
				Why:    "Testers spend their time on the systems under test, not on walking."},
			{Flag: quest.EnvInstantInventoryExpansion, On: quest.InstantInventoryExpansionFromEnv(),
				Native: "A bigger bag takes effect at the next world entry.",
				Now:    "A bigger bag takes effect at once.",
				Why:    "Relogging to see new slots read as a bug."},
			{Flag: companion.EnvPetPacing, On: companion.PoliciesFromEnv().Pacing,
				Native: "Grab and growth pets run at their full projected speed.",
				Now:    "Grab and growth pets run at four fifths of it.",
				Why:    "Owner-approved beta pacing for pets."},
			{Flag: party.EnvPartyMasteries, On: party.MasteriesFromEnv(),
				Native: "The party board shows the basic member row.",
				Now:    "The party board also shows each member's two main masteries.",
				Why:    "Lets a party see its builds at a glance."},
			{Flag: simulation.EnvBetaPlayerMap, On: simulation.BetaPlayerMapEnabled(),
				Native: "The world map shows only your own position.",
				Now:    "The world map also shows other players.",
				Why:    "Helps testers find each other on a small shard."},
			{Flag: enterworld.EnvStackSizes, On: len(stackSizes) > 0,
				Native: "Retail stack sizes.",
				Now:    "Larger stacks for some item kinds.",
				Why:    "Less bag management during testing."},
		},
		Deviations: []publicstats.Deviation{
			{Title: "Storage Ctrl+click",
				Native: "Items move between bag and storage by dragging.",
				Now:    "Ctrl+click also moves an item between bag and storage."},
			{Title: "Double-click attack",
				Native: "A second click on a monster that drifted can miss it.",
				Now:    "A second press on the same monster within half a second attacks it."},
			{Title: "Skill press while seated",
				Native: "A skill pressed while seated casts from the seat.",
				Now:    "The character stands up, then casts."},
		},
	}
}
