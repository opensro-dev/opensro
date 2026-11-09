/*
===========================================================================

chatreport_wire_test.go - /report over the wire: consumed, private, one line back

Port-only, not native (#2959). Over the real transport: A's /report line
earns A the ordinary success ack plus exactly one notice carrying the
confirmation, B hears nothing, and the report reaches the bug-report
sink. With no intake wired the same text is ordinary chat again.

===========================================================================
*/
package chat_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/agent/bugreport"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/chatreport"
)

// reportSink captures the reports the intake delivers.
type reportSink struct {
	ch chan bugreport.Report
}

/*
================
reportSink.Deliver
================
*/
func (sink *reportSink) Deliver(_ context.Context, report bugreport.Report) (string, error) {
	sink.ch <- report
	return "wire-delivery", nil
}

/*
================
TestChatReportLineIsConsumedAndPrivate
================
*/
func TestChatReportLineIsConsumedAndPrivate(t *testing.T) {
	const division = "global-official"
	server := startChatServer(t, filepath.Join(t.TempDir(), "authority"), division)
	sink := &reportSink{ch: make(chan bugreport.Report, 4)}
	server.chat.Reports = chatreport.New(sink, t.TempDir(), nil)

	a := dialWS(t, server.srv)
	helloWS(t, a)
	enterChatWorld(t, a, division, e2eChatNameA)
	b := dialWS(t, server.srv)
	helloWS(t, b)
	enterChatWorld(t, b, division, e2eChatNameB)

	// The reporter gets the ordinary ack and one notice, nothing more.
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "/report "+e2eChatNameB+" being awful"))
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "report receipt")
	expectExact(t, a, chat.OpChatBroadcast, chat.EncodeChatNotice("Report filed against "+e2eChatNameB+". Staff will review it."), "report confirmation")
	// The reported player hears nothing.
	gameReadyBarrier(t, b, "the reported player hears nothing")

	select {
	case report := <-sink.ch:
		if report.Character != e2eChatNameA {
			t.Fatalf("report character = %q, want %q", report.Character, e2eChatNameA)
		}
		if fields := report.Submission.Context; len(fields) < 2 ||
			fields[0] != (bugreport.Field{Name: "Reported player", Value: e2eChatNameB}) ||
			fields[1] != (bugreport.Field{Name: "Reason", Value: "being awful"}) {
			t.Fatalf("report context = %+v", fields)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the report never reached the sink")
	}

	// With no intake wired the same text is ordinary chat text: the
	// room hears it like any other line.
	server.chat.Reports = nil
	sendFrame(t, a, chat.OpChatRequest, chatRequestFrame(chat.ChatTypeAll, "", "/report "+e2eChatNameB+" hello?"))
	expectExact(t, a, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "/report "+e2eChatNameB+" hello?"), "unwired /report is chat")
	expectExact(t, a, chat.OpChatAck, []byte{1, 1, 255}, "ordinary receipt")
	expectExact(t, b, chat.OpChatBroadcast, namedBroadcast(chat.ChatTypeGlobal, e2eChatNameA, "/report "+e2eChatNameB+" hello?"), "the room hears the unwired line")
}
