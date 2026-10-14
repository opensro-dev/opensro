package wire

import "unicode/utf16"

// JobKillNoticeChannel is the 0x3667 channel of the job-kill announcement.
// Port-only, not native: v1.150 (753760) sends any channel outside its
// table, 0x80 included, to its named-sender arm (753D8A), so this channel
// is safe only because the browser client is the only client. A channel of
// its own lets that client hide these notices (the Experimental "Job kill
// notices" row) while showing them exactly like channel 7; a browser
// client without the row drops it (decodeChatBroadcast returns null).
const JobKillNoticeChannel = 0x80

// noticeChannel is v1.150's server notification channel (753A6F).
const noticeChannel = 7

// v1.150 753A6F: chat type 7 contains only a sized UTF-16 string.
// 752ABD sends it to the notification banner and the chat log. This is a
// server-authored message, not an invented B245 skill-error number.
func NotificationFrame(message string) Frame {
	return notificationFrameOn(noticeChannel, message)
}

// JobKillNotificationFrame is NotificationFrame on JobKillNoticeChannel.
func JobKillNotificationFrame(message string) Frame {
	return notificationFrameOn(JobKillNoticeChannel, message)
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
