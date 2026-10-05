package guild

import (
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// AppendSeedFrame appends the 0x32C4 guild block when character has a valid
// persisted guild membership. No guild is represented by frame absence.
func AppendSeedFrame(
	frames []enterworld.Packet,
	guilds domain.GuildStore,
	presence Presence,
	divisionID string,
	character *domain.Character,
) []enterworld.Packet {
	if guilds == nil || character == nil || character.GuildID == nil {
		return frames
	}

	guildID := *character.GuildID
	record, members, ok := guilds.Guild(divisionID, guildID)
	if !ok {
		log.Errorf(
			"guild: character %s references missing guild %d in division %s; omitting 0x32C4",
			character.Name,
			guildID,
			divisionID,
		)
		return frames
	}

	online := func(name string) bool {
		return presence != nil && presence.OnlineByName(divisionID, name)
	}
	return append(
		frames,
		enterworld.NewPacket(OpGuildInfo, EncodeGuildInfo32C4(record, members, online, time.Now().UnixMilli())),
	)
}
