/*
===========================================================================

churn_test.go - the accounts that open the most sessions

A loop account's entries rank first with their minute and
character-hour peaks; an older span stays out of the window; a session
without a User-Agent is counted on its account; the route refuses an
unknown window.

===========================================================================
*/
package census

import (
	"net/http"
	"testing"
)

/*
================
TestChurnRanksTheLoopAccountWithItsPeaks
================
*/
func TestChurnRanksTheLoopAccountWithItsPeaks(t *testing.T) {
	now := nowMs()
	minuteStart := now - 30*churnMinuteMs
	var spans []Span
	// The loop: five entries inside one minute on Looper, one more on
	// Looper2 a minute later, and one of them with no browser.
	for i := int64(0); i < 5; i++ {
		spans = append(spans, span("loop-"+string(rune('a'+i)), "acct-loop", "Looper",
			minuteStart+i*1000, minuteStart+i*1000+500, "ip-l", "net-l", "client-l"))
	}
	spans[2].NoUserAgent = true
	spans = append(spans, span("loop-f", "acct-loop", "Looper2", minuteStart+churnMinuteMs, 0, "ip-l", "net-l", "client-l"))
	// An ordinary player: one session in the window, one before it.
	spans = append(spans,
		span("human-old", "acct-human", "Talker", now-3*churnHourMs, now-2*churnHourMs, "ip-h", "net-h", "client-h"),
		span("human-now", "acct-human", "Talker", now-10*churnMinuteMs, 0, "ip-h", "net-h", "client-h"))

	answer, err := testService(spans, nil).Churn(churnHourMs)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Sessions != 7 || answer.Accounts != 2 || answer.Truncated || len(answer.Top) != 2 {
		t.Fatalf("totals %+v", answer)
	}
	loop, human := answer.Top[0], answer.Top[1]
	if loop != (AccountChurn{Account: "acct-loop", Sessions: 6, Characters: 2, PeakMinute: 5, PeakCharacterHour: 5, NoUserAgent: 1}) {
		t.Fatalf("loop row %+v", loop)
	}
	if human != (AccountChurn{Account: "acct-human", Sessions: 1, Characters: 1, PeakMinute: 1, PeakCharacterHour: 1}) {
		t.Fatalf("human row %+v", human)
	}

	wide, err := testService(spans, nil).Churn(windowDayMs)
	if err != nil {
		t.Fatal(err)
	}
	if wide.Sessions != 8 || wide.Top[1].Sessions != 2 {
		t.Fatalf("24h window %+v", wide)
	}
}

/*
================
TestChurnRouteChecksItsWindow
================
*/
func TestChurnRouteChecksItsWindow(t *testing.T) {
	handler := handlerFor(t, testService(nil, nil))
	if code, _ := call(handler, "/churn?window=7d", testToken); code != http.StatusBadRequest {
		t.Fatalf("7d window answered %d", code)
	}
	code, body := call(handler, "/churn", testToken)
	if code != http.StatusOK || body["windowMs"] != float64(churnHourMs) {
		t.Fatalf("default window %d %v", code, body)
	}
}
