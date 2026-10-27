/*
===========================================================================

wiring_certwatch_test.go - the staff poster behind the certificate alert

The poster reads the webhook file at every post, refuses anything that is
not a Discord webhook, disables mentions, and keeps the token out of its
errors. A fake round-tripper stands in for Discord.

===========================================================================
*/
package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bug-report package's own webhook shape: a snowflake id and a token.
const testStaffWebhook = "https://discord.com/api/webhooks/1234567890123456789/staffsecrettoken_0123456789-ABC"

/*
================
recordingTransport
================
*/
type recordingTransport struct {
	requests []*http.Request
	bodies   []string
	status   int
	fail     bool
}

func (r *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if r.fail {
		return nil, errors.New("dial " + request.URL.String() + ": refused")
	}
	body, _ := io.ReadAll(request.Body)
	r.requests = append(r.requests, request)
	r.bodies = append(r.bodies, string(body))
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

/*
================
writeWebhook
================
*/
func writeWebhook(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "staff-webhook")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

/*
================
TestStaffWebhookPosterPostsPlainTextWithoutMentions
================
*/
func TestStaffWebhookPosterPostsPlainTextWithoutMentions(t *testing.T) {
	discord := &recordingTransport{status: http.StatusNoContent}
	post := staffWebhookPoster(writeWebhook(t, testStaffWebhook+"\n"), &http.Client{Transport: discord})
	if err := post("OpenSRO global-official: @everyone the pair expires"); err != nil {
		t.Fatal(err)
	}
	if len(discord.requests) != 1 || discord.requests[0].URL.String() != testStaffWebhook {
		t.Fatalf("requests = %v, want one POST to the trimmed webhook", discord.requests)
	}
	var body struct {
		Content         string              `json:"content"`
		AllowedMentions map[string][]string `json:"allowed_mentions"`
	}
	if err := json.Unmarshal([]byte(discord.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Content, "expires") || body.AllowedMentions["parse"] == nil ||
		len(body.AllowedMentions["parse"]) != 0 {
		t.Fatalf("body = %s, want the text with mentions disabled", discord.bodies[0])
	}
}

/*
================
TestStaffWebhookPosterReadsTheFileAtEveryPost

A rotated webhook applies to the next post without a restart.
================
*/
func TestStaffWebhookPosterReadsTheFileAtEveryPost(t *testing.T) {
	discord := &recordingTransport{status: http.StatusOK}
	path := writeWebhook(t, "")
	post := staffWebhookPoster(path, &http.Client{Transport: discord})
	if err := post("first"); !errors.Is(err, errNoStaffWebhook) {
		t.Fatalf("post with an empty file = %v, want errNoStaffWebhook", err)
	}
	if err := os.WriteFile(path, []byte(testStaffWebhook), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := post("second"); err != nil || len(discord.requests) != 1 {
		t.Fatalf("post after the rotation = %v with %d requests, want one delivery", err, len(discord.requests))
	}
}

/*
================
TestStaffWebhookPosterRefusesAndRedacts
================
*/
func TestStaffWebhookPosterRefusesAndRedacts(t *testing.T) {
	if err := staffWebhookPoster("", http.DefaultClient)("x"); !errors.Is(err, errNoStaffWebhook) {
		t.Fatalf("no path = %v, want errNoStaffWebhook", err)
	}
	discord := &recordingTransport{status: http.StatusOK}
	if err := staffWebhookPoster(writeWebhook(t, "https://example.test/hook"), &http.Client{Transport: discord})("x"); err == nil ||
		len(discord.requests) != 0 {
		t.Fatalf("a non-Discord URL = %v with %d requests, want a refusal and no request", err, len(discord.requests))
	}
	discord.status = http.StatusTooManyRequests
	if err := staffWebhookPoster(writeWebhook(t, testStaffWebhook), &http.Client{Transport: discord})("x"); err == nil ||
		!strings.Contains(err.Error(), "429") {
		t.Fatalf("a 429 = %v, want an HTTP error", err)
	}
	discord.fail = true
	err := staffWebhookPoster(writeWebhook(t, testStaffWebhook), &http.Client{Transport: discord})("x")
	if err == nil || strings.Contains(err.Error(), "staffsecrettoken") {
		t.Fatalf("a failed request = %v, want an error without the webhook token", err)
	}
}
