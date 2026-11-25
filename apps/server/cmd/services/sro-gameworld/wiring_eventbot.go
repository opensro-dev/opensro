/*
===========================================================================

wiring_eventbot.go - the EventBot host's composition (port-only, #3533)

SRO_EVENT_BOT seats the chat-event host (internal/game/social/eventbot):
its whisper intake on the chat runtime, its rounds on the mission
tick, its schedule on the public feed and its prizes on the normal
item door. Off (the default, and the native posture) none of this
exists: no hook is installed, no tick runs, and the name EventBot is
nobody's.

===========================================================================
*/
package main

import (
	"math"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	livepresence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/eventbot"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// eventSeekRange is how close a finder must stand to the hider for
// the find to count (the composition root's world check, host.go's
// ReportFound judges the claim itself).
const eventSeekRange = 30.0

/*
================
buildEventBot

The host and its doors; nil while the switch is off. A schedule file
that refuses to parse leaves the host running empty (the error is
logged once): the operator fixes the file and restarts.
================
*/
func buildEventBot(division string, deps *enterworld.Deps, presence *livepresence.Directory, authority *store.Store, items *action.Runtime) *eventbot.Host {
	if !eventbot.EnabledFromEnv() {
		return nil
	}
	knownItem := func(codename string) bool {
		_, ok := deps.Items.ItemRefByCodename(codename)
		return ok
	}
	var schedule eventbot.Schedule
	if path := strings.TrimSpace(os.Getenv(eventbot.EnvEventBotFile)); path != "" {
		loaded, err := eventbot.LoadScheduleFile(path, knownItem)
		if err != nil {
			log.Errorf("eventbot: schedule file %s refused: %v (host runs empty)", path, err)
		} else {
			schedule = loaded
			for _, event := range schedule.Events {
				if event.Hider != "" {
					if _, known := items.Monsters.ReferenceByCodename(event.Hider); !known {
						log.Errorf("eventbot: %q hider %q is not a monster codename; its rounds will run hiderless", event.Title, event.Hider)
					}
				}
				for _, codename := range event.Uniques {
					ref, known := items.Monsters.ReferenceByCodename(codename)
					if !known {
						log.Errorf("eventbot: %q unique %q is not a monster codename; its rounds will run without it", event.Title, codename)
						continue
					}
					// Only a unique's death records a kill owner
					// (RecordUniqueKiller), so any other monster's
					// round always ends with no winner.
					if !monster.RarityIsUnique(ref.MonsterType) {
						log.Errorf("eventbot: %q unique %q is not a unique; its kills name no owner and its rounds can never be won", event.Title, codename)
					}
				}
			}
		}
	} else {
		log.Infof("eventbot: %s is unset; the host runs with an empty schedule", eventbot.EnvEventBotFile)
	}
	host := eventbot.New(true, schedule, eventbot.Dependencies{
		Whisper: func(division, name, text string) {
			if session, online := presence.SessionByName(division, name); online {
				_ = session.Send(chat.OpChatBroadcast,
					chat.EncodeChatBroadcastNamed(chat.ChatTypeWhisper, eventbot.BotName, text))
			}
		},
		Announce: func(text string) {
			payload := chat.EncodeChatBroadcastNamed(chat.ChatTypeWhisper, eventbot.BotName, text)
			for _, division := range authority.DivisionIDs() {
				var names []string
				deps.Read(division, func() {
					for _, character := range deps.CharactersForDivision(division) {
						if character != nil && !character.DeletePending {
							names = append(names, character.Name)
						}
					}
				})
				for _, name := range names {
					if session, online := presence.SessionByName(division, name); online {
						_ = session.Send(chat.OpChatBroadcast, payload)
					}
				}
			}
		},
		Grant: func(division string, character *enterworld.Character, codename string, count int) bool {
			// The atomic quest-inventory owner plans the prize (stacks,
			// slots, capacity) on a detached bag copy; the rows commit
			// inside the labelled door and its frames go to the winner's
			// session, so an online winner sees the prize at once. A
			// refusal (a full bag) commits nothing, so the win never
			// reaches the audit and never counts against the cap.
			paid := false
			var frames []wire.Frame
			deps.Mutate(character, domain.GrantEventBotReward, func() {
				rows, planned, err := items.PlanQuestInventory(character, nil,
					[]inventory.ItemAmount{{Codename: codename, Count: uint32(count)}})
				if err != nil {
					log.Infof("eventbot: prize %dx %s for %s refused: %v", count, codename, character.Name, err)
					return
				}
				character.MissionInventory = rows
				frames = planned
				paid = true
			})
			if paid {
				if session, online := presence.SessionByName(division, character.Name); online {
					for _, frame := range frames {
						_ = session.Send(frame.Opcode, frame.Payload)
					}
				}
			}
			return paid
		},
		WinsToday: func(accountID string, day time.Time) int {
			n, err := authority.FreeGrantDayCount(accountID, domain.GrantEventBotReward, day.UnixMilli())
			if err != nil {
				log.Warnf("eventbot: win count for %s: %v (counts as at the cap)", accountID, err)
			}
			return eventbot.WinsAfterRead(n, err)
		},
		// Hide & Seek's hider (#3536): a tracked GM spawn in the open
		// world's population at the file's spot, so visibility,
		// selection and entry all take the ordinary monster lanes;
		// the despawn is the spawn's silent counterpart. An unknown
		// codename or a closed world runs the round hiderless.
		SpawnHider: func(event eventbot.Event, spot eventbot.Spot, at time.Time) (eventbot.Hider, bool) {
			ref, ok := items.Monsters.ReferenceByCodename(event.Hider)
			if !ok {
				log.Warnf("eventbot: hider %q is not a monster codename; the round runs hiderless", event.Hider)
				return eventbot.Hider{}, false
			}
			lease, open := items.Monsters.PopulationLease(division, instance.ID(domain.DefaultWorldInstance))
			if !open {
				return eventbot.Hider{}, false
			}
			gids := items.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{
				Division:   division,
				Population: lease,
				RefObjID:   ref.RefObjID,
				Count:      1,
				Position:   simulation.Spawn{RegionID: uint16(spot.RegionID), X: spot.X, Z: spot.Z},
				NowMs:      at.UnixMilli(),
			})
			if len(gids) != 1 {
				return eventbot.Hider{}, false
			}
			gid := gids[0]
			return eventbot.Hider{
				ObjectID: gid,
				RegionID: spot.RegionID,
				X:        spot.X,
				Z:        spot.Z,
				Despawn:  func() { items.Monsters.DespawnGMMonster(division, gid) },
			}, true
		},
		KnownItem: knownItem,
	})
	log.Infof("eventbot: ON, %d scheduled event(s)", len(schedule.Events))
	return host
}

/*
================
installEventBotChat

Seats the host's whisper intake on the chat runtime. Only the host's
own name is claimed; every other whisper keeps the native lane.
================
*/
func (game *gameplayPlane) installEventBotChat() {
	if game.eventBot == nil || game.chat == nil {
		return
	}
	game.chat.EventBotWhisper = func(divisionID string, sender *enterworld.Character, target, text string) ([]string, bool) {
		if !strings.EqualFold(target, eventbot.BotName) {
			return nil, false
		}
		return game.eventBot.HandleWhisper(eventbot.WhisperSender{Division: divisionID, Character: sender}, text), true
	}
}

/*
================
eventSeekTick

The world kinds' tick: while a hider is live, a seek round goes to
the first entrant standing close with the hider actually selected,
alive and out of a stall. A slain (or otherwise gone) spawn ends
the round here - for a kill round (Random Unique, #3537) the host
judges the recorded killer instead, and finds never count.
Per-tick over the division's characters; the host re-checks entry
and the cap before paying.
================
*/
func (game *gameplayPlane) eventSeekTick(nowMs int64) {
	host := game.eventBot
	if host == nil {
		return
	}
	hider, live := host.LiveHider()
	if !live {
		return
	}
	if spawn, ok := game.items.Monsters.Get(game.divisionID, hider.ObjectID); !ok || spawn.CurrentHP == 0 {
		// A kill round's monster fell to a recorded killer: the
		// host judges the kill. Anything else is simply lost.
		if killer, known := game.items.Monsters.UniqueKiller(game.divisionID, hider.ObjectID); known && host.ReportSlain(killer) {
			return
		}
		host.HiderLost()
		return
	}
	// A kill round is judged on the kill, never on a find.
	if host.LiveRoundWonByKill() {
		return
	}
	var hunters []*enterworld.Character
	game.deps.Read(game.divisionID, func() {
		for _, character := range game.deps.CharactersForDivision(game.divisionID) {
			if character != nil && !character.DeletePending {
				hunters = append(hunters, character)
			}
		}
	})
	for _, character := range hunters {
		selected, okSel := game.items.Selected.Get(game.divisionID, character.Name)
		if !okSel || selected != hider.ObjectID {
			continue
		}
		if !enterworld.CharacterAlive(character) {
			continue
		}
		if _, owns := game.items.Stalls.Get(game.divisionID, character.Name); owns {
			continue
		}
		if _, visiting := game.items.Stalls.Visiting(game.divisionID, character.Name); visiting {
			continue
		}
		pose := game.items.LiveSpawnFor(game.divisionID, character)
		if int(pose.RegionID) != hider.RegionID {
			continue
		}
		if math.Hypot(pose.X-hider.X, pose.Z-hider.Z) > eventSeekRange {
			continue
		}
		if host.ReportFound(character) {
			return
		}
	}
}

/*
================
shutdownEventBot

The drain's last seat: a live hider leaves the population with the
process instead of lingering as a phantom in a world that outlives
no one. The hook is optional exactly like the host itself.
================
*/
func (game *gameplayPlane) shutdownEventBot() {
	if game.eventBot != nil {
		game.eventBot.Shutdown()
	}
}
