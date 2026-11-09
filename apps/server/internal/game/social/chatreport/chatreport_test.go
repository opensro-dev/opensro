/*
===========================================================================

chatreport_test.go - /report intake: answer, triage payload, rate limit

The intake's done-when, exercised against a hand-written staff chat log:
the one-line confirmation, the report the bug-report flow receives
(reporter, resolved target, reason, the pair's context with the target's
lines marked), and the per-account rate limit.

===========================================================================
*/
package chatreport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/agent/bugreport"
)

// fakeSink captures every delivery on a channel the test drains.
type fakeSink struct {
	ch chan bugreport.Report
}

/*
================
fakeSink.Deliver
================
*/
func (fake *fakeSink) Deliver(_ context.Context, report bugreport.Report) (string, error) {
	fake.ch <- report
	return "fake-delivery", nil
}

/*
================
receiveReport
================
*/
func receiveReport(t *testing.T, sink *fakeSink) bugreport.Report {
	t.Helper()
	select {
	case report := <-sink.ch:
		return report
	case <-time.After(5 * time.Second):
		t.Fatal("the report never reached the sink")
		return bugreport.Report{}
	}
}

/*
================
expectNoReport
================
*/
func expectNoReport(t *testing.T, sink *fakeSink) {
	t.Helper()
	select {
	case report := <-sink.ch:
		t.Fatalf("a report was delivered anyway: %+v", report)
	case <-time.After(200 * time.Millisecond):
	}
}

// chatlogLine hand-writes one line of the staff chat log's JSONL.
func chatlogLine(ts time.Time, account, character, channel, target, text string) string {
	return fmt.Sprintf(`{"ts":%q,"account":%q,"character":%q,"channel":%q,"target":%q,"muted":false,"text":%q}`,
		ts.Format(time.RFC3339), account, character, channel, target, text)
}

/*
================
writeLog
================
*/
func writeLog(t *testing.T, dir string, lines ...string) {
	t.Helper()
	// The service's pinned clock sits on 2026-11-08, so that day's file
	// is the one the context scan reads.
	name := filepath.Join(dir, "chat-2026-11-08.log")
	if err := os.WriteFile(name, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

/*
================
newTestService
================
*/
func newTestService(sink *fakeSink, dir string) *Service {
	service := New(sink, dir, func(divisionID, name string) (string, string, bool) {
		if strings.EqualFold(name, "troublekid") {
			return "acc-target", "TroubleKid", true
		}
		return "", "", false
	})
	service.now = func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) }
	return service
}

/*
================
TestFileFilesAndDeliversWithContext

The confirmation names the typed target; the delivered report carries
the resolved target, the reason and the pair's lines oldest-first with
the target's marked - while a whisper to a third party and a bystander's
public line stay out of the context.
================
*/
func TestFileFilesAndDeliversWithContext(t *testing.T) {
	dir := t.TempDir()
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 11, 8, hour, minute, 0, 0, time.UTC)
	}
	writeLog(t, dir,
		chatlogLine(at(10, 0), "acc-target", "TroubleKid", "all", "", "buy gold cheap"),
		chatlogLine(at(10, 1), "acc-reporter", "ReporterOne", "all", "", "stop spamming"),
		chatlogLine(at(10, 2), "acc-target", "TroubleKid", "whisper", "ReporterOne", "nice try"),
		chatlogLine(at(10, 3), "acc-target", "TroubleKid", "whisper", "Bystander", "psst, gold?"),
		chatlogLine(at(10, 4), "acc-bystander", "Bystander", "all", "", "leave me out"),
		chatlogLine(at(10, 5), "acc-reporter", "ReporterOne", "whisper", "TroubleKid", "blocked"),
	)
	sink := &fakeSink{ch: make(chan bugreport.Report, 4)}
	service := newTestService(sink, dir)
	reporter := Reporter{AccountID: "acc-reporter", Character: "ReporterOne", Division: "global-official"}

	answer, handled := service.File(reporter, "/report troublekid spamming and swearing")
	if !handled {
		t.Fatal("the /report line was not handled")
	}
	if want := "Report filed against troublekid. Staff will review it."; answer != want {
		t.Fatalf("answer = %q, want %q", answer, want)
	}
	report := receiveReport(t, sink)
	if report.Account != "acc-reporter" || report.Character != "ReporterOne" || report.Division != "global-official" {
		t.Fatalf("report identity = %q/%q/%q", report.Account, report.Character, report.Division)
	}
	if want := "Chat abuse report: TroubleKid\nReason: spamming and swearing"; report.Submission.Description != want {
		t.Fatalf("description = %q, want %q", report.Submission.Description, want)
	}
	fields := report.Submission.Context
	if len(fields) != 3 {
		t.Fatalf("context fields = %d, want 3: %+v", len(fields), fields)
	}
	if fields[0] != (bugreport.Field{Name: "Reported player", Value: "TroubleKid"}) {
		t.Fatalf("fields[0] = %+v", fields[0])
	}
	if fields[1] != (bugreport.Field{Name: "Reason", Value: "spamming and swearing"}) {
		t.Fatalf("fields[1] = %+v", fields[1])
	}
	wantContext := "» [10:00] TroubleKid: buy gold cheap\n" +
		"  [10:01] ReporterOne: stop spamming\n" +
		"» [10:02] TroubleKid (whisper): nice try\n" +
		"  [10:05] ReporterOne (whisper): blocked"
	if fields[2] != (bugreport.Field{Name: "Chat context", Value: wantContext}) {
		t.Fatalf("fields[2] = %+v, want the marked pair context %q", fields[2], wantContext)
	}
}

/*
================
TestFileRateLimit

One account is paced for the cooldown; another account is not; once the
clock passes the cooldown the first account reports again.
================
*/
func TestFileRateLimit(t *testing.T) {
	sink := &fakeSink{ch: make(chan bugreport.Report, 4)}
	service := newTestService(sink, t.TempDir())
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	reporter := Reporter{AccountID: "acc-reporter", Character: "ReporterOne", Division: "global-official"}

	if _, handled := service.File(reporter, "/report TroubleKid"); !handled {
		t.Fatal("the first report was not handled")
	}
	receiveReport(t, sink)

	answer, handled := service.File(reporter, "/report TroubleKid again")
	if !handled || answer != reportLimited {
		t.Fatalf("second report = %q, handled=%v, want the limited answer", answer, handled)
	}
	expectNoReport(t, sink)

	other := Reporter{AccountID: "acc-other", Character: "SomeoneElse", Division: "global-official"}
	if _, handled := service.File(other, "/report TroubleKid"); !handled {
		t.Fatal("another account's report was not handled")
	}
	receiveReport(t, sink)

	now = now.Add(reportCooldown + time.Minute)
	if _, handled := service.File(reporter, "/report TroubleKid third"); !handled {
		t.Fatal("the post-cooldown report was not handled")
	}
	receiveReport(t, sink)
}

/*
================
TestFileUsageSelfAndOrdinaryText
================
*/
func TestFileUsageSelfAndOrdinaryText(t *testing.T) {
	sink := &fakeSink{ch: make(chan bugreport.Report, 4)}
	service := newTestService(sink, t.TempDir())
	reporter := Reporter{AccountID: "acc-reporter", Character: "ReporterOne", Division: "global-official"}

	if answer, handled := service.File(reporter, "/report"); !handled || answer != reportUsage {
		t.Fatalf("bare /report = %q, handled=%v, want the usage line", answer, handled)
	}
	if answer, handled := service.File(reporter, "/report ReporterOne honest"); !handled || answer != reportSelf {
		t.Fatalf("self report = %q, handled=%v, want the self line", answer, handled)
	}
	if answer, handled := service.File(reporter, "just talking"); handled || answer != "" {
		t.Fatalf("ordinary text = %q, handled=%v, want unhandled", answer, handled)
	}
	expectNoReport(t, sink)
}

/*
================
TestFileWithoutLogContext

An empty log says so in the report instead of inventing context.
================
*/
func TestFileWithoutLogContext(t *testing.T) {
	sink := &fakeSink{ch: make(chan bugreport.Report, 4)}
	service := newTestService(sink, t.TempDir())
	reporter := Reporter{AccountID: "acc-reporter", Character: "ReporterOne", Division: "global-official"}

	if _, handled := service.File(reporter, "/report Ghost"); !handled {
		t.Fatal("the report was not handled")
	}
	report := receiveReport(t, sink)
	fields := report.Submission.Context
	if len(fields) != 3 {
		t.Fatalf("context fields = %d, want 3: %+v", len(fields), fields)
	}
	if want := (bugreport.Field{Name: "Chat context", Value: "(no recent lines between them in the chat log)"}); fields[2] != want {
		t.Fatalf("fields[2] = %+v, want %+v", fields[2], want)
	}
	if fields[1] != (bugreport.Field{Name: "Reason", Value: "(no reason given)"}) {
		t.Fatalf("fields[1] = %+v, want the no-reason placeholder", fields[1])
	}
}
