/*
===========================================================================

moderation_wiring.go - the chat mute book and its console actions

The book opens at boot (before admission, so a muted character's first
bind already finds the record), the reaper ends mutes on time, and the
console's conduct actions reach the book and the presence directory. One
owner per piece: the book owns mutes, the hub owns live records, the
presence directory ends sessions (#2671). The /report intake (#2959)
seats here too: it is the other half of the same staff lane.

===========================================================================
*/
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/moderation"
	"opensro.online/server/internal/game/social/chatreport"
	"opensro.online/server/internal/transport"
)

/*
================
openMuteBook

The book lives beside the shard's other state files. A corrupt book stops
the boot: forgetting every mute silently is worse than a loud refusal.
================
*/
func openMuteBook(hub *transport.Hub, shard string) (*moderation.Book, error) {
	path := filepath.Join(store.DirForShardFromEnv(shard), moderation.MuteFileName)
	book, err := moderation.OpenBook(path, hub, time.Now, store.WriteFileAtomic)
	if err != nil {
		return nil, fmt.Errorf("chat mutes: %w", err)
	}
	return book, nil
}

/*
================
runMuteReaper

Ends mutes whose time is up, every moderation.ReapInterval, until ctx
ends. A failed save leaves the mutes enforced and retries next pass.
================
*/
func runMuteReaper(ctx context.Context, book *moderation.Book) {
	ticker := time.NewTicker(moderation.ReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ended, err := book.Reap()
			if err != nil {
				log.WithError(err).Warn("chat mutes: reap failed; retrying next pass")
				continue
			}
			for _, mute := range ended {
				log.WithFields(log.Fields{"character": mute.Character, "clause": mute.Clause}).Info("chat mute ended")
			}
		}
	}
}

/*
================
conductOperations

The console's mute-chat, unmute-chat and kick hooks. read reports the
character after the change, as every other player operation does.
================
*/
func conductOperations(game *gameplayPlane, book *moderation.Book, shard string, read func(string) (any, error)) (mute, unmute, kick func(agentapi.PlayerOperation) (any, error)) {
	mute = func(request agentapi.PlayerOperation) (any, error) {
		record, err := book.Mute(moderation.Mute{Division: shard, Character: request.Character, Operator: request.Operator,
			Clause: request.Clause, Reason: request.Reason}, time.Duration(request.Minutes)*time.Minute)
		if err != nil {
			return nil, err
		}
		return withMute(read, request.Character, record)
	}
	unmute = func(request agentapi.PlayerOperation) (any, error) {
		if _, err := book.Unmute(shard, request.Character); err != nil {
			return nil, err
		}
		return read(request.Character)
	}
	kick = func(request agentapi.PlayerOperation) (any, error) {
		if game.presence == nil || !game.presence.EndSessionByName(shard, request.Character) {
			return nil, fmt.Errorf("%s is not in the world", request.Character)
		}
		return read(request.Character)
	}
	return mute, unmute, kick
}

/*
================
withMute

The character view plus the mute just applied, so the console shows the
end time the server will keep.
================
*/
func withMute(read func(string) (any, error), name string, record moderation.Mute) (any, error) {
	view, err := read(name)
	if err != nil {
		return nil, err
	}
	if fields, ok := view.(map[string]any); ok {
		fields["chatMute"] = record
	}
	return view, nil
}

/*
================
wireChatReports

Port-only, not native (#2959): seats the chat handler's /report intake
(internal/game/social/chatreport) on this shard. The pair's context is
read from the same moderation chat log the console searches, and the
report is delivered through the bug-report flow, so it lands in the
staff triage the Discord bot already feeds. The intake exists only
where a bug-report sink reaches this process (SRO_BUG_REPORT_*);
without one "/report ..." stays ordinary chat text. The target
resolver answers inside the deps door because it walks the live
character indexes.
================
*/
func wireChatReports(game *gameplayPlane) {
	reports := chatreport.FromEnv(os.Getenv, store.DirForShardFromEnv(game.divisionID), func(divisionID, name string) (string, string, bool) {
		account, canonical, found := "", "", false
		game.deps.Read(divisionID, func() {
			for _, candidate := range game.deps.CharactersForDivision(divisionID) {
				if candidate != nil && strings.EqualFold(candidate.Name, name) {
					snapshot := candidate.Snapshot()
					account, canonical, found = snapshot.AccountID, snapshot.Name, true
					return
				}
			}
		})
		return account, canonical, found
	})
	if reports == nil {
		return
	}
	game.chat.Reports = reports
	log.Info("chat: /report intake ON (bug-report sink configured)")
}
