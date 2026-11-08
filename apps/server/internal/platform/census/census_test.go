/*
===========================================================================

census_test.go - the census answers over synthetic spans

A fixed clock and fixture spans pin the online counts (people count
clusters, not accounts), the unique windows, the person view, and
the privacy rule: no hash or fingerprint value appears in any
answer, ever.

===========================================================================
*/
package census

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const hourMs = 60 * 60 * 1000

var testNowTime = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func nowMs() int64 { return testNowTime.UnixMilli() }

// span builds one fixture span; ended 0 leaves it open.
func span(session, account, character string, started, ended int64, ip, net, client string) Span {
	return Span{Session: session, Account: account, Character: character, Transport: "webtransport",
		Started: started, Ended: ended, IPKey: ip, NetKey: net, Client: client}
}

// testService is a Service over fixture spans with the fixed clock.
func testService(spans []Span, chat ChatCounts) *Service {
	return NewService(func(int64) ([]Span, error) { return spans, nil }, chat,
		func() time.Time { return testNowTime })
}

/*
================
TestOnlineCountsPeopleNotAccounts

Two accounts of one device and one stranger are online: three
sessions, three accounts, two people. The series samples the same
truth backwards.
================
*/
func TestOnlineCountsPeopleNotAccounts(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-2*hourMs, 0, "ip-h", "net-h", "client-1"),
		span("s2", "acct-b", "Bert", now-hourMs, 0, "ip-h", "net-h", "client-1"),
		span("s3", "acct-c", "Cora", now-30*60*1000, 0, "ip-c", "net-c", "client-2"),
		span("s4", "acct-d", "Dana", now-3*hourMs, now-2*hourMs, "ip-d", "net-d", "client-3"),
	}
	online, err := testService(spans, nil).OnlineNow(24*hourMs, 15*60*1000)
	if err != nil {
		t.Fatal(err)
	}
	if online.Sessions != 3 || online.Accounts != 3 || online.Characters != 3 || online.People != 2 {
		t.Fatalf("online now: got sessions=%d accounts=%d characters=%d people=%d, want 3/3/3/2",
			online.Sessions, online.Accounts, online.Characters, online.People)
	}
	if len(online.Series) != 97 {
		t.Fatalf("series points: got %d, want 97 (a day of quarters plus now)", len(online.Series))
	}
	last := online.Series[len(online.Series)-1]
	if last.T != now || last.People != 2 || last.Sessions != 3 {
		t.Fatalf("series at now: got %+v, want people=2 sessions=3", last)
	}
	for _, point := range online.Series {
		if point.T == now-2*hourMs {
			if point.Sessions != 1 || point.People != 1 {
				t.Fatalf("series two hours back: got %+v, want one session of one person", point)
			}
		}
		if point.T == now-3*hourMs {
			if point.Sessions != 1 {
				t.Fatalf("series three hours back: got %+v, want Dana's session only", point)
			}
		}
	}
}

/*
================
TestUniquesWindows

One account per window edge: the 24h, 7d and 30d uniques step up,
and people follow accounts when nothing links them.
================
*/
func TestUniquesWindows(t *testing.T) {
	now := nowMs()
	var day int64 = 24 * hourMs
	spans := []Span{
		span("s1", "acct-a", "Anna", now-2*hourMs, now-hourMs, "ip-a", "net-a", "client-a"),
		span("s2", "acct-b", "Bert", now-3*day, now-3*day+hourMs, "ip-b", "net-b", "client-b"),
		span("s3", "acct-c", "Cora", now-10*day, now-10*day+hourMs, "ip-c", "net-c", "client-c"),
	}
	uniques, err := testService(spans, nil).Uniques()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"24h": 1, "7d": 2, "30d": 3}
	for name, people := range want {
		got := uniques.Windows[name]
		if got.People != people || got.Accounts != people || got.Sessions != people {
			t.Fatalf("uniques %s: got %+v, want %d of everything", name, got, people)
		}
	}
}

/*
================
TestPublicCounts

The public pair (opensro-web#78) is the online read's people number
and the 7-day window's, derived from the same fixtures the staff
readings use - never the census itself.
================
*/
func TestPublicCounts(t *testing.T) {
	now := nowMs()
	var day int64 = 24 * hourMs
	spans := []Span{
		span("s1", "acct-a", "Anna", now-2*hourMs, 0, "ip-a", "net-a", "client-a"),
		span("s2", "acct-b", "Bert", now-3*day, now-3*day+hourMs, "ip-b", "net-b", "client-b"),
	}
	peopleNow, uniquePeople7d, err := testService(spans, nil).PublicCounts()
	if err != nil {
		t.Fatal(err)
	}
	if peopleNow != 1 || uniquePeople7d != 2 {
		t.Fatalf("public counts: got peopleNow=%d uniquePeople7d=%d, want 1/2", peopleNow, uniquePeople7d)
	}
}

/*
================
TestPersonView

From one character: the whole cluster, both accounts with their
presence, the shared history newest first, and the household
neighbour riding along as a possible link - not a merge.
================
*/
func TestPersonView(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-5*hourMs, now-4*hourMs, "ip-h", "net-h", "client-1"),
		span("s2", "acct-a", "Anna", now-hourMs, 0, "ip-h", "net-h", "client-1"),
		span("s3", "acct-b", "Bert", now-2*hourMs, 0, "ip-h", "net-h", "client-1"),
		span("s4", "acct-c", "Cora", now-2*hourMs, now, "ip-h", "net-h", "client-9"),
	}
	person, found, err := testService(spans, nil).Person("anna")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Anna's person view not found")
	}
	if person.Account != "acct-a" || person.Character != "Anna" {
		t.Fatalf("person head: got account=%q character=%q", person.Account, person.Character)
	}
	if len(person.Cluster.Accounts) != 2 || len(person.Accounts) != 2 {
		t.Fatalf("person cluster: got accounts=%v entries=%d, want the device pair",
			person.Cluster.Accounts, len(person.Accounts))
	}
	for _, account := range person.Accounts {
		if !account.Online {
			t.Fatalf("account %s should read online", account.Account)
		}
	}
	if len(person.History) != 3 || person.History[0].Session != "s2" {
		t.Fatalf("history: got %+v, want three spans newest first", person.History)
	}
	link, linked := findLink(person.PossibleLinks, "acct-a", "acct-c")
	if !linked || link.Merged {
		t.Fatalf("the household neighbour should ride as an unmerged possible link: %+v", person.PossibleLinks)
	}
	if _, found, err := testService(spans, nil).Person("Nobody"); err != nil || found {
		t.Fatalf("unknown character: got found=%v err=%v, want neither", found, err)
	}
}

/*
================
TestAnswersNeverCarryHashes

Every answer marshals without the fixture's hash or fingerprint
values: correlation happens inside the package or nowhere.
================
*/
func TestAnswersNeverCarryHashes(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-9*hourMs, 0, "SECRET-IP-A", "SECRET-NET-A", "SECRET-CLIENT"),
		span("s2", "acct-b", "Bert", now-2*hourMs, now, "SECRET-IP-A", "SECRET-NET-A", "SECRET-CLIENT"),
	}
	service := testService(spans, func(int64, int64) (map[string]int, error) { return map[string]int{}, nil })
	answers := []any{}
	for _, read := range []func() (any, error){
		func() (any, error) { return service.OnlineNow(24*hourMs, 15*60*1000) },
		func() (any, error) { return service.Uniques() },
		func() (any, error) { return service.Clusters() },
		func() (any, error) { return service.Signals() },
	} {
		answer, err := read()
		if err != nil {
			t.Fatal(err)
		}
		answers = append(answers, answer)
	}
	person, found, err := service.Person("Anna")
	if err != nil || !found {
		t.Fatalf("person: found=%v err=%v", found, err)
	}
	answers = append(answers, person)
	for _, answer := range answers {
		body, err := json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"SECRET-IP-A", "SECRET-NET-A", "SECRET-CLIENT"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("answer leaks %s: %s", secret, body)
			}
		}
	}
}
