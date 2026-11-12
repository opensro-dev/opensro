package wire

import "unicode/utf16"

// JobKillNoticeChannel is the 0x3667 channel of the job-kill announcement.
// Port-only, not native: v1.150 (753760) sends any channel outside its
// table, 0x80 included, to its named-sender arm (client 753D8A), so
// this channel
// is safe only because the browser client is the only client. A channel of
// its own lets that client hide these notices (the Experimental "Job kill
// notices" row) while showing them exactly like channel 7; a browser
// client without the row drops it (decodeChatBroadcast returns null).
const JobKillNoticeChannel = 0x80

// DropPlusNoticeChannel is the 0x3667 channel of the seal-drop and +8..+11
// announcements. Port-only, not native, like JobKillNoticeChannel: the
// browser client shows it as channel 7 unless the Experimental "Drop & plus
// notices" row is off. Its payload extends channel 7's with the player and
// the item the notice names, so the client can link them
// (DropPlusNotificationFrame); a protocol-9 client drops the channel.
const DropPlusNoticeChannel = 0x81

// BreakNoticeChannel is the 0x3667 channel of the item-break announcement
// (#1738, port-only, not native, like JobKillNoticeChannel): the browser
// client shows it as channel 7 unless the Experimental "Item break notices"
// row is off, and a client without the row drops it. A break notice links
// nothing at all. A failed-reinforcement notice (#3260) may carry the
// drop/plus link section holding only its item's body - never a player
// name - so the alias it names can still never lead to a character.
const BreakNoticeChannel = 0x82

// RenameNoticeChannel is the 0x3667 channel of the name-change notice
// (#1689, port-only, not native, like JobKillNoticeChannel): "X is now
// known as Y" goes to the renamed character's online friends and guild
// members only - never a shard announcement - so the frame only ever
// rides a targeted send. The browser client shows it as channel 7
// unconditionally: it is the feature's own feedback, not a broadcast a
// player opts out of. The frame is the plain channel-7 layout.
const RenameNoticeChannel = 0x83

// NoticeLink is what a drop or plus notice links: the player it names
// (empty for none) and the item's CSOItem body as the inventory sends it,
// ItemBody.Encode, which opens with the refObjID (empty for none).
type NoticeLink struct {
	Player string
	Body   []byte
}

// noticeChannel is v1.150's server notification channel (753A6F).
const noticeChannel = 7

// v1.150 753A6F: chat type 7 contains only a sized UTF-16 string.
// Client 752ABD (inside ClientChat_PresentChannelMessage) sends it to
// the notification banner and the chat log. This is a
// server-authored message, not an invented B245 skill-error number.
func NotificationFrame(message string) Frame {
	return notificationFrameOn(noticeChannel, message)
}

// JobKillNotificationFrame is NotificationFrame on JobKillNoticeChannel.
func JobKillNotificationFrame(message string) Frame {
	return notificationFrameOn(JobKillNoticeChannel, message)
}

// BreakNotificationFrame is a notice on BreakNoticeChannel. An empty
// body writes the plain channel-7 layout, byte for byte what the frame
// has always been; a body appends the drop/plus link section after the
// text. The frame takes the item body alone, so its name is always
// empty: no caller can make this channel name a player (#1738's rule).
func BreakNotificationFrame(message string, body []byte) Frame {
	if len(body) == 0 {
		return notificationFrameOn(BreakNoticeChannel, message)
	}
	return linkedNotificationFrame(BreakNoticeChannel, message, NoticeLink{Body: body})
}

// RenameNotificationFrame is NotificationFrame on RenameNoticeChannel.
func RenameNotificationFrame(message string) Frame {
	return notificationFrameOn(RenameNoticeChannel, message)
}

// DropPlusNotificationFrame is a notice on DropPlusNoticeChannel, always in
// its extended layout:
//
//	[u8 0x81][u16 n][n UTF-16 units][u16 nameLen][name][u16 bodyLen][body]
//
// nameLen 0 names no player; bodyLen 0 links no item.
func DropPlusNotificationFrame(message string, link NoticeLink) Frame {
	return linkedNotificationFrame(DropPlusNoticeChannel, message, link)
}

// linkedNotificationFrame writes one notification on a channel followed
// by the drop/plus link section, [u16 nameLen][name][u16 bodyLen][body].
func linkedNotificationFrame(channel uint8, message string, link NoticeLink) Frame {
	if len(link.Player) > 0xffff || len(link.Body) > 0xffff {
		panic("notice link exceeds its length fields")
	}
	text := notificationFrameOn(channel, message).Payload
	w := NewWriter(len(text) + 4 + len(link.Player) + len(link.Body)).Bytes(text).
		Str(link.Player).U16(uint16(len(link.Body))).Bytes(link.Body)
	return Frame{Opcode: 0x3667, Payload: w.Payload()}
}

// notificationFrameOn writes one notification on a channel.
func notificationFrameOn(channel uint8, message string) Frame {
	text := utf16.Encode([]rune(message))
	if len(text) > 100 {
		panic("notification exceeds text budget")
	}
	w := NewWriter(3 + 2*len(text)).U8(channel).U16(uint16(len(text)))
	for _, ch := range text {
		w.U16(ch)
	}
	return Frame{Opcode: 0x3667, Payload: w.Payload()}
}
