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
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	livepresence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/eventbot"
)

/*
================
buildEventBot

The host and its doors; nil while the switch is off. A schedule file
that refuses to parse leaves the host running empty (the error is
logged once): the operator fixes the file and restarts.
================
*/
func buildEventBot(deps *enterworld.Deps, presence *livepresence.Directory, authority *store.Store) *eventbot.Host {
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
		Grant: func(character *enterworld.Character, codename string, count int) bool {
			ref, ok := deps.Items.ItemRefByCodename(codename)
			if !ok {
				return false
			}
			paid := false
			deps.Mutate(character, domain.GrantEventBotReward, func() {
				paid = eventbot.AddReward(character, ref, count)
			})
			return paid
		},
		WinsToday: func(accountID string, day time.Time) int {
			n, err := authority.FreeGrantDayCount(accountID, domain.GrantEventBotReward, day.UnixMilli())
			if err != nil {
				log.Warnf("eventbot: win count for %s: %v", accountID, err)
				return 0
			}
			return int(n)
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
