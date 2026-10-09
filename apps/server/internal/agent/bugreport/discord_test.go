/*
===========================================================================

discord_test.go - delivery through a fake Discord webhook

===========================================================================
*/
package bugreport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "secret-webhook-token-0123456789"

/*
================
fakeDiscord

Records each webhook execution and answers with the next queued status.
================
*/
type fakeDiscord struct {
	mu       sync.Mutex
	payloads []discordPayload
	files    []string
	queries  []string
	answers  []int
}

/*
================
fakeDiscord.ServeHTTP
================
*/
func (fake *fakeDiscord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.queries = append(fake.queries, r.URL.RawQuery)
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(part)
		if part.FormName() == "payload_json" {
			var payload discordPayload
			if err := json.Unmarshal(data, &payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fake.payloads = append(fake.payloads, payload)
			continue
		}
		fake.files = append(fake.files, part.FormName()+"="+part.FileName()+":"+part.Header.Get("Content-Type"))
	}
	status := http.StatusOK
	if len(fake.answers) > 0 {
		status, fake.answers = fake.answers[0], fake.answers[1:]
	}
	switch status {
	case http.StatusOK:
		_, _ = io.WriteString(w, `{"id":"1420000000000000001"}`)
	case http.StatusBadRequest:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"code":220001,"message":"Webhooks posted to forum channels must have a thread_name or thread_id"}`)
	default:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"code":0}`)
	}
}

/*
================
newTestService
================
*/
func newTestService(t *testing.T, fake *fakeDiscord) *Service {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	service, err := New(Config{WebhookURL: server.URL + "/api/webhooks/1/" + testToken, ReplayDefault: true, MaxBytes: testMaxBytes}, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

/*
================
testReport
================
*/
func testReport() Report {
	return Report{
		Account:   "tester",
		Division:  "Global Official",
		Character: "Hero",
		Submission: Submission{
			Description: "@everyone the bridge eats my character\nsecond line",
			Context:     []Field{{Name: "Build", Value: "290e29b"}},
			Errors:      []string{"TypeError: x is undefined", "```closing fence"},
			Attachment:  &Attachment{FileName: "replay.mp4", ContentType: "video/mp4", Data: testMP4},
		},
	}
}

/*
================
TestDeliverPostsEmbedAndClipWithoutMentions
================
*/
func TestDeliverPostsEmbedAndClipWithoutMentions(t *testing.T) {
	fake := &fakeDiscord{}
	id, err := newTestService(t, fake).Deliver(context.Background(), testReport())
	if err != nil || id != "1420000000000000001" {
		t.Fatalf("id %q err %v", id, err)
	}
	if len(fake.payloads) != 1 || fake.queries[0] != "wait=true" {
		t.Fatalf("want one waited execution, got %d (%v)", len(fake.payloads), fake.queries)
	}
	payload := fake.payloads[0]
	parse, ok := payload.AllowedMentions["parse"].([]any)
	if !ok || len(parse) != 0 {
		t.Fatalf("allowed_mentions must disable every ping: %#v", payload.AllowedMentions)
	}
	if payload.ThreadName != "" {
		t.Fatal("text channels must not get a thread name")
	}
	embed := payload.Embeds[0]
	if embed.Title != "Bug report — Hero" || !strings.HasPrefix(embed.Description, "@everyone the bridge") {
		t.Fatalf("embed %+v", embed)
	}
	names := map[string]string{}
	for _, field := range embed.Fields {
		names[field.Name] = field.Value
	}
	if _, named := names["Account"]; named || strings.Contains(embed.Title+payload.ThreadName, "tester") {
		t.Fatalf("the public post names the account: %+v", embed)
	}
	if names["Server"] != "Global Official" || names["Build"] != "290e29b" {
		t.Fatalf("fields %+v", embed.Fields)
	}
	if errorsField := names[errorsFieldName]; strings.Count(errorsField, "```") != 2 {
		t.Fatalf("errors must stay inside one code block: %q", errorsField)
	}
	if len(fake.files) != 1 || fake.files[0] != "files[0]=replay.mp4:video/mp4" {
		t.Fatalf("files %v", fake.files)
	}
}

/*
================
TestDeliverLearnsForumChannels
================
*/
func TestDeliverLearnsForumChannels(t *testing.T) {
	fake := &fakeDiscord{answers: []int{http.StatusBadRequest}}
	service := newTestService(t, fake)
	if _, err := service.Deliver(context.Background(), testReport()); err != nil {
		t.Fatal(err)
	}
	if len(fake.payloads) != 2 || fake.payloads[1].ThreadName != "Hero: @everyone the bridge eats my character" {
		t.Fatalf("want a retry with a thread name, got %+v", fake.payloads)
	}
	if _, err := service.Deliver(context.Background(), testReport()); err != nil {
		t.Fatal(err)
	}
	if len(fake.payloads) != 3 || fake.payloads[2].ThreadName == "" {
		t.Fatal("the forum channel must be remembered")
	}
}

/*
================
TestDeliverMapsDiscordFailures
================
*/
func TestDeliverMapsDiscordFailures(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusTooManyRequests:     ErrBusy,
		http.StatusInternalServerError: ErrDelivery,
		http.StatusNotFound:            ErrDelivery,
	} {
		fake := &fakeDiscord{answers: []int{status}}
		_, err := newTestService(t, fake).Deliver(context.Background(), testReport())
		if !errors.Is(err, want) {
			t.Errorf("status %d: want %v, got %v", status, want, err)
		}
	}
}

/*
================
TestDeliveryErrorsNeverContainTheWebhook

net/http puts the request URL, token included, into its errors.
================
*/
func TestDeliveryErrorsNeverContainTheWebhook(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL + "/api/webhooks/1/" + testToken
	server.Close()
	service, err := New(Config{WebhookURL: url, MaxBytes: testMaxBytes}, &http.Client{Timeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Deliver(context.Background(), testReport())
	if !errors.Is(err, ErrDelivery) {
		t.Fatalf("want ErrDelivery, got %v", err)
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "/api/webhooks") {
		t.Fatalf("error leaks the webhook: %v", err)
	}
}

/*
================
TestEmbedStaysWithinDiscordLimits
================
*/
func TestEmbedStaysWithinDiscordLimits(t *testing.T) {
	report := testReport()
	report.Description = strings.Repeat("d", maxDescriptionRunes)
	report.Context = nil
	for range maxContextFields {
		report.Context = append(report.Context, Field{Name: strings.Repeat("n", maxFieldNameRunes), Value: strings.Repeat("v", maxFieldValueRunes)})
	}
	report.Errors = nil
	for range maxErrorLines {
		report.Errors = append(report.Errors, strings.Repeat("e", maxErrorRunes))
	}
	embed := buildEmbed(report)
	total := len([]rune(embed.Title)) + len([]rune(embed.Description))
	for _, field := range embed.Fields {
		if len([]rune(field.Value)) > maxFieldValue {
			t.Fatalf("field %q has %d characters", field.Name, len([]rune(field.Value)))
		}
		total += len([]rune(field.Name)) + len([]rune(field.Value))
	}
	if total > 6000 || len(embed.Fields) > 25 {
		t.Fatalf("embed has %d characters in %d fields", total, len(embed.Fields))
	}
}

/*
================
TestDeliverPostsDiagnosticsAsSecondFile

The journal archive follows the clip as files[1], and the payload's
attachment ids name both files in the same order.
================
*/
func TestDeliverPostsDiagnosticsAsSecondFile(t *testing.T) {
	fake := &fakeDiscord{}
	report := testReport()
	report.Diagnostics = &Attachment{FileName: "diagnostics.zip", ContentType: "application/zip", Data: []byte("PK\x03\x04")}
	if _, err := newTestService(t, fake).Deliver(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if len(fake.files) != 2 || fake.files[0] != "files[0]=replay.mp4:video/mp4" ||
		fake.files[1] != "files[1]=diagnostics.zip:application/zip" {
		t.Fatalf("files %v", fake.files)
	}
	got := fake.payloads[0].Attachments
	if len(got) != 2 || got[0] != (discordFileInfo{ID: 0, FileName: "replay.mp4"}) ||
		got[1] != (discordFileInfo{ID: 1, FileName: "diagnostics.zip"}) {
		t.Fatalf("attachments %+v", got)
	}
}

/*
================
TestDeliverPostsDiagnosticsAlone

A report without a clip or screenshot still carries its journals as files[0].
================
*/
func TestDeliverPostsDiagnosticsAlone(t *testing.T) {
	fake := &fakeDiscord{}
	report := testReport()
	report.Attachment = nil
	report.Diagnostics = &Attachment{FileName: "diagnostics.zip", ContentType: "application/zip", Data: []byte("PK\x03\x04")}
	if _, err := newTestService(t, fake).Deliver(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if len(fake.files) != 1 || fake.files[0] != "files[0]=diagnostics.zip:application/zip" {
		t.Fatalf("files %v", fake.files)
	}
}
