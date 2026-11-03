/*
===========================================================================

search_test.go - the console's read of the moderation log

===========================================================================
*/
package chatlog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef"

/*
================
writeDay

One daily file holding the given raw rows.
================
*/
func writeDay(t *testing.T, dir, date string, rows ...string) {
	t.Helper()
	body := strings.Join(rows, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, FilePrefix+date+FileSuffix), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

/*
================
row
================
*/
func row(t *testing.T, ts, account, character, text string) string {
	t.Helper()
	out, err := json.Marshal(line{TS: ts, Account: account, Character: character, Channel: "all", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

/*
================
texts
================
*/
func texts(result Result) string {
	out := make([]string, len(result.Lines))
	for i, entry := range result.Lines {
		out[i] = entry.Text
	}
	return strings.Join(out, ",")
}

/*
================
searchFixture

Three lines over two days, with the other files a shard directory holds.
================
*/
func searchFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeDay(t, dir, "2026-10-30",
		row(t, "2026-10-30T10:00:00Z", "acct1", "Alpha", "first"),
		row(t, "2026-10-30T11:00:00Z", "acct2", "Beta", "second"))
	writeDay(t, dir, "2026-10-31", row(t, "2026-10-31T09:00:00Z", "acct1", "Alpha", "third"))
	if err := os.WriteFile(filepath.Join(dir, "chat-notes.log"), []byte("not a day\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSearchFiltersNewestFirstAcrossDays(t *testing.T) {
	dir := searchFixture(t)
	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"everything", Query{}, "third,second,first"},
		{"account", Query{Account: "ACCT1"}, "third,first"},
		{"character", Query{Character: "beta"}, "second"},
		{"from is inclusive", Query{From: time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC)}, "third,second"},
		{"to is exclusive", Query{To: time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC)}, "first"},
		{"nobody", Query{Character: "Gamma"}, ""},
	}
	for _, c := range cases {
		result, err := Search(dir, c.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := texts(result); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSearchLimitKeepsTheNewestAndSaysSo(t *testing.T) {
	result, err := Search(searchFixture(t), Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(result); got != "third,second" || !result.Truncated {
		t.Fatalf("got %q truncated=%v", got, result.Truncated)
	}
	exact, err := Search(searchFixture(t), Query{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if exact.Truncated {
		t.Fatal("a limit that holds every match reported older lines")
	}
}

func TestSearchCountsDamageInsteadOfHidingIt(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-10-31", row(t, "2026-10-31T09:00:00Z", "a", "A", "kept"), "{torn", row(t, "soon", "a", "A", "x"))
	result, err := Search(dir, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if texts(result) != "kept" || result.Skipped != 2 {
		t.Fatalf("got %q skipped=%d", texts(result), result.Skipped)
	}
}

func TestSearchOfAnEmptyShardIsEmptyNotAnError(t *testing.T) {
	result, err := Search(filepath.Join(t.TempDir(), "absent"), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Lines == nil || len(result.Lines) != 0 {
		t.Fatalf("lines = %#v, want an empty list for the console", result.Lines)
	}
}

/*
================
operatorRequest
================
*/
func operatorRequest(method, query string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1"+OperatorPath+query, nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("X-SRO-Local-Diagnostics", "1")
	return r
}

func TestOperatorHandlerAnswersOnlyTheLocalOperator(t *testing.T) {
	dir := searchFixture(t)
	tokenPath := filepath.Join(t.TempDir(), "operator-token")
	if err := os.WriteFile(tokenPath, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := OperatorHandler(dir, tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Origin", "X-Forwarded-For", "Authorization"} {
		r := operatorRequest("GET", "")
		r.Header.Set(bad, "untrusted")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "first") {
			t.Fatalf("%s: code %d body %q", bad, w.Code, w.Body.String())
		}
	}
	statuses := []struct {
		method, query string
		want          int
	}{
		{"GET", "?character=alpha&limit=1", http.StatusOK},
		{"POST", "", http.StatusMethodNotAllowed},
		{"GET", "?from=yesterday", http.StatusBadRequest},
		{"GET", "?limit=0", http.StatusBadRequest},
		{"GET", "?account=" + strings.Repeat("a", maxFilterLength+1), http.StatusBadRequest},
	}
	for _, c := range statuses {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, operatorRequest(c.method, c.query))
		if w.Code != c.want {
			t.Fatalf("%s %s: code %d, want %d", c.method, c.query, w.Code, c.want)
		}
		if c.want == http.StatusOK {
			var result Result
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if texts(result) != "third" || !result.Truncated {
				t.Fatalf("got %q truncated=%v", texts(result), result.Truncated)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("chat text must not be cached")
			}
		}
	}
}

func TestOperatorHandlerWithoutATokenAnswersNobody(t *testing.T) {
	handler, err := OperatorHandler(searchFixture(t), filepath.Join(t.TempDir(), "operator-token"))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, operatorRequest("GET", ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("code %d, want 404", w.Code)
	}
}
