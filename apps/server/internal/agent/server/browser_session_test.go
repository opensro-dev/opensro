package agentserver

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserCharacterRequiresMatchingCookieAndSuccessfulAdmission(t *testing.T) {
	accepted := true
	worker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !accepted {
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": "one-use-ticket"})
	})
	f := newAgentFixture(t, worker, worker)
	publishFixtureLease(t, f, "alpha", "worker-alpha", 1, 0)
	login := performJSON(t, f.handler, http.MethodPost, "/title/login", `{"id":"tester","password":"123123","serverId":"alpha"}`, "")
	cookie := login.Result().Cookies()[0]
	*f.now = f.now.Add(time.Hour)
	publishFixtureLease(t, f, "alpha", "worker-alpha", 2, 0)
	var hint *http.Cookie
	for _, row := range []struct{ cookie, accepted, remember bool }{{false, true, false}, {true, false, false}, {true, true, true}} {
		accepted = row.accepted
		r := httptest.NewRequest(http.MethodPost, "/auth/enterworld-token", strings.NewReader(`{"characterName":"Test2","divisionId":"alpha"}`))
		declareBrowser(r)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+cookie.Value)
		if row.cookie {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		cookies := w.Result().Cookies()
		if !row.remember {
			if len(cookies) != 0 {
				t.Fatal("unauthenticated or rejected selection remembered")
			}
			continue
		}
		if len(cookies) != 1 {
			t.Fatalf("missing selection cookie, status %d", w.Code)
		}
		hint = cookies[0]
		if hint.Name != cookie.Name+"Character" || !hint.HttpOnly || !hint.Secure || hint.SameSite != http.SameSiteStrictMode || hint.MaxAge != 11*3600 {
			t.Fatal("selection must retain secure attributes and original expiry")
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/title/session", strings.NewReader(`{}`))
	declareBrowser(r)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	r.AddCookie(hint)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	var restored struct {
		ResumeCharacter  string `json:"resumeCharacter"`
		NativeServerName string `json:"nativeServerName"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &restored); err != nil || restored.ResumeCharacter != "Test2" || restored.NativeServerName != "Alpha" {
		t.Fatal("character hint not restored")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(hint.Value)
	if err != nil || string(decoded) != "Test2" {
		t.Fatal("hint encoding changed")
	}
}

func TestBrowserSessionRefreshExpiryAndLogout(t *testing.T) {
	f := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	publishFixtureLease(t, f, "alpha", "worker-alpha", 1, 0)
	login := performJSON(t, f.handler, http.MethodPost, "/title/login", `{"id":"tester","password":"123123","serverId":"alpha"}`, "")
	cookies := login.Result().Cookies()
	if len(cookies) != 2 || cookies[1].MaxAge != -1 {
		t.Fatalf("missing cookie: status %d", login.Code)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 43200 || cookie.Name != browserSessionCookie {
		t.Fatal("unsafe session cookie attributes")
	}
	call := func(path string, c *http.Cookie, origin string, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		declareBrowser(r)
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("Origin", origin)
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	restored := call("/title/session", cookie, "", "application/json")
	var body map[string]any
	if err := json.Unmarshal(restored.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if restored.Code != 200 || body["ok"] != true || body["divisionId"] != "alpha" || body["sessionToken"] != cookie.Value || restored.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("restoration did not preserve authenticated route")
	}
	if len(restored.Result().Cookies()) != 0 {
		t.Fatal("reload must not extend expiry")
	}
	if call("/title/session", nil, "", "application/json").Code != 401 {
		t.Fatal("anonymous resume accepted")
	}
	forged := *cookie
	forged.Value += "x"
	if call("/title/session", &forged, "", "application/json").Code != 401 {
		t.Fatal("forged cookie accepted")
	}
	if call("/title/session", cookie, "https://attacker.invalid", "application/json").Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	if call("/title/logout", cookie, "", "text/plain").Code != 415 {
		t.Fatal("simple CSRF accepted")
	}
	dock := call("/title/character-select", cookie, "", "application/json")
	dockCookies := dock.Result().Cookies()
	if dock.Code != 200 || len(dockCookies) != 1 || dockCookies[0].Name != cookie.Name+"Character" || dockCookies[0].MaxAge != -1 {
		t.Fatal("restart must clear only the remembered character")
	}
	if call("/title/session", cookie, "", "application/json").Code != 200 {
		t.Fatal("restart invalidated account authentication")
	}
	logout := call("/title/logout", cookie, "", "application/json")
	cleared := logout.Result().Cookies()
	if logout.Code != 200 || len(cleared) != 2 || cleared[0].MaxAge != -1 || cleared[0].Value != "" || cleared[1].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	*f.now = f.now.Add(12*time.Hour + time.Second)
	if call("/title/session", cookie, "", "application/json").Code != 401 {
		t.Fatal("expired cookie accepted")
	}
}

func TestBrowserCookieSecureExceptExplicitLocalHTTP(t *testing.T) {
	for _, row := range []struct {
		url, forwarded, name string
		secure               bool
	}{
		{"http://127.0.0.1:8787/title/login", "", loopbackSessionCookie, false},
		{"http://localhost:8787/title/login", "", loopbackSessionCookie, false},
		{"http://[::1]:8787/title/login", "", loopbackSessionCookie, false},
		{"http://game.example/title/login", "", browserSessionCookie, true},
		{"https://127.0.0.1/title/login", "", browserSessionCookie, true},
		{"http://127.0.0.1/title/login", "https", browserSessionCookie, true},
	} {
		r := httptest.NewRequest("POST", row.url, nil)
		r.Header.Set("X-Forwarded-Proto", row.forwarded)
		name, secure := browserCookieName(r)
		if name != row.name || secure != row.secure {
			t.Fatalf("cookie policy for %s", row.url)
		}
	}
}

func TestBrowserSessionCredentialsRequireExactAllowedOrigin(t *testing.T) {
	server := &Server{allowedOrigins: map[string]bool{"https://game.example": true}}
	handler := server.cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	for _, origin := range []string{"https://game.example", "https://game.example.attacker.invalid", "null"} {
		r := httptest.NewRequest(http.MethodOptions, "/title/session", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if origin == "https://game.example" {
			if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != origin || w.Header().Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatal("missing exact credentialed CORS")
			}
		} else if w.Code != 403 || w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("foreign credentialed CORS accepted")
		}
	}
}
