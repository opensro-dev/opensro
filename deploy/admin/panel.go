/*
===========================================================================

panel.go - the panel's pages and the calls behind them

Characters go to the chosen shard's player operations
(/internal/operations/player): a GET reads one character, a POST runs one
audited operation. The panel's own shard answers on loopback; another
shard answers through its operator relay (relay.go), each with its own
token. The GameWorld validates every field and writes the
audit; the panel only turns a form into the operation's JSON, gives it a
fresh request id and names the operator. Accounts go through the relay
to the Agent's provisioning API.

The panel has no login of its own: it is reachable only from the host's
loopback, through an SSH tunnel. guard keeps a web page in the staff's
browser from driving it (a cross-site POST or a rebound DNS name).

===========================================================================
*/
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed templates/*.html
var templateFiles embed.FS

// responseLimit bounds what the panel reads back from a private API.
const responseLimit = 4 << 20

/*
================
panelConfig
================
*/
type panelConfig struct {
	Shards                []shardConfig
	AccountsURL           string
	ProvisioningTokenFile string
	OperatorName          string
}

/*
================
shardConfig

One shard the panel operates: its id, where its player operations answer
and the file holding its operator token.
================
*/
type shardConfig struct {
	ID        string
	URL       string
	TokenFile string
}

/*
================
parseShards

ADMIN_SHARDS is "id=url,id=url"; the first is the default. Each shard's
token is <tokenDir>/operator-token-<id>, the copy its provision job writes.
================
*/
func parseShards(spec, tokenDir string) ([]shardConfig, error) {
	var shards []shardConfig
	for _, entry := range strings.Split(spec, ",") {
		id, address, found := strings.Cut(strings.TrimSpace(entry), "=")
		if !found || id == "" || strings.ContainsAny(id, "/.") {
			return nil, fmt.Errorf("ADMIN_SHARDS entry %q must be id=url", entry)
		}
		if parsed, err := url.Parse(address); err != nil || parsed.Scheme != "http" || parsed.Host == "" {
			return nil, fmt.Errorf("ADMIN_SHARDS %s: %q must be an http URL", id, address)
		}
		shards = append(shards, shardConfig{ID: id, URL: address, TokenFile: filepath.Join(tokenDir, "operator-token-"+id)})
	}
	return shards, nil
}

/*
================
shard

The configured shard named id, or the first one for an empty id.
================
*/
func (p *panel) shard(id string) (shardConfig, error) {
	if id == "" {
		return p.config.Shards[0], nil
	}
	for _, shard := range p.config.Shards {
		if shard.ID == id {
			return shard, nil
		}
	}
	return shardConfig{}, fmt.Errorf("unknown shard %q", id)
}

/*
================
panel
================
*/
type panel struct {
	config panelConfig
	pages  *template.Template
	client *http.Client
}

/*
================
pageData

What every page renders: the page name, the outcome of the last action,
and the page's own data.
================
*/
type pageData struct {
	Page      string
	Shards    []shardConfig
	Shard     string
	OK        string
	Error     string
	Name      string
	Character map[string]any
	Towns     []any
	Raw       string
	Account   string
}

/*
================
newPanel
================
*/
func newPanel(config panelConfig) (*panel, error) {
	if len(config.Shards) == 0 {
		return nil, fmt.Errorf("the panel needs at least one shard")
	}
	pages, err := template.New("").Funcs(template.FuncMap{"field": field}).ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &panel{config: config, pages: pages, client: &http.Client{Timeout: upstreamTimeout}}, nil
}

/*
================
Handler
================
*/
func (p *panel) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/character", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /character", p.showCharacter)
	mux.HandleFunc("POST /character", p.runOperation)
	mux.HandleFunc("GET /accounts", p.showAccounts)
	mux.HandleFunc("POST /accounts", p.runAccount)
	return guard(mux)
}

/*
================
guard

The Host must be a loopback name (a rebound DNS name is not), and a POST
must come from the panel's own pages, not a cross-site form.
================
*/
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if ip := net.ParseIP(strings.Trim(host, "[]")); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "open the panel through its SSH tunnel (http://localhost:9090)", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (origin != "" && origin != "http://"+r.Host) {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

//============================================================================

/*
================
showCharacter
================
*/
func (p *panel) showCharacter(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "character", Name: strings.TrimSpace(r.URL.Query().Get("name")), Shards: p.config.Shards,
		OK: r.URL.Query().Get("ok"), Error: r.URL.Query().Get("err")}
	shard, err := p.shard(r.URL.Query().Get("shard"))
	data.Shard = shard.ID
	if err != nil {
		data.Error = err.Error()
	} else if data.Name != "" {
		body, err := p.operator(shard, http.MethodGet, "?character="+url.QueryEscape(data.Name), nil)
		if err != nil {
			data.Error = err.Error()
		} else {
			var read struct {
				Player map[string]any `json:"player"`
				Towns  []any          `json:"towns"`
			}
			if err := json.Unmarshal(body, &read); err != nil {
				data.Error = "unreadable character: " + err.Error()
			}
			data.Character, data.Towns, data.Raw = read.Player, read.Towns, indent(body)
		}
	}
	p.render(w, data)
}

/*
================
runOperation

One form, one operation. Field names follow the GameWorld's
PlayerOperation; the GameWorld refuses a field an action does not take.
================
*/
func (p *panel) runOperation(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	shard, err := p.shard(r.FormValue("shard"))
	var operation map[string]any
	if err == nil {
		operation, err = buildOperation(r, name, p.config.OperatorName)
	}
	if err == nil {
		var body []byte
		body, err = json.Marshal(operation)
		if err == nil {
			_, err = p.operator(shard, http.MethodPost, "", body)
		}
	}
	back := url.Values{"name": {name}, "shard": {shard.ID}}
	if err != nil {
		back.Set("err", err.Error())
	} else {
		back.Set("ok", operation["action"].(string)+" applied")
	}
	http.Redirect(w, r, "/character?"+back.Encode(), http.StatusSeeOther)
}

/*
================
buildOperation
================
*/
func buildOperation(r *http.Request, name, operator string) (map[string]any, error) {
	action := r.FormValue("action")
	operation := map[string]any{"id": requestID(), "operator": operator, "character": name, "action": action,
		"reason": strings.TrimSpace(r.FormValue("reason"))}
	number := func(field string, bits int) (int64, error) {
		value, err := strconv.ParseInt(strings.TrimSpace(r.FormValue(field)), 10, bits)
		if err != nil {
			return 0, fmt.Errorf("%s: not a number", field)
		}
		return value, nil
	}
	var fields []string
	switch action {
	case "set-level":
		fields = []string{"level"}
	case "set-sp":
		fields = []string{"skillPoints"}
	case "grant-silk":
		fields = []string{"silk"}
	case "adjust-gold":
		fields = []string{"gold"}
	case "rescue":
		fields = []string{"town"}
	case "mute-chat":
		fields = []string{"minutes"}
	case "grant-items":
		count, err := number("count", 32)
		if err != nil {
			return nil, err
		}
		operation["items"] = []map[string]any{{"codename": strings.TrimSpace(r.FormValue("codename")), "count": count}}
	case "remove-crest":
		operation["crest"] = r.FormValue("crest")
	case "reset-stats", "clear-pk", "unmute-chat", "kick":
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
	for _, field := range fields {
		value, err := number(field, 64)
		if err != nil {
			return nil, err
		}
		operation[field] = value
	}
	if action == "mute-chat" || action == "unmute-chat" || action == "kick" {
		operation["clause"] = strings.TrimSpace(r.FormValue("clause"))
	}
	return operation, nil
}

/*
================
operator

One call to a shard's player operations, answered body or the
GameWorld's refusal as the error.
================
*/
func (p *panel) operator(shard shardConfig, method, query string, body []byte) ([]byte, error) {
	token, err := readToken(shard.TokenFile)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(context.Background(), method,
		strings.TrimSuffix(shard.URL, "/")+"/internal/operations/player"+query, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// The GameWorld's local-operator rule: loopback, no browser Origin, and
	// this marker (agentapi.localDiagnosticsRequest).
	request.Header.Set("X-SRO-Local-Diagnostics", "1")
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return p.do(request)
}

//============================================================================

/*
================
showAccounts
================
*/
func (p *panel) showAccounts(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "accounts", Account: strings.TrimSpace(r.URL.Query().Get("id")),
		OK: r.URL.Query().Get("ok"), Error: r.URL.Query().Get("err")}
	if data.Account != "" {
		body, err := p.accounts(http.MethodGet, "/"+url.PathEscape(data.Account), nil)
		if err != nil {
			data.Error = err.Error()
		} else {
			data.Raw = indent(body)
		}
	}
	p.render(w, data)
}

/*
================
runAccount
================
*/
func (p *panel) runAccount(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.FormValue("id"))
	password := r.FormValue("password")
	var err error
	var done string
	switch r.FormValue("action") {
	case "create":
		if password != r.FormValue("confirm") {
			err = fmt.Errorf("the passwords do not match")
			break
		}
		err = p.accountJSON(http.MethodPost, "", map[string]any{"id": id, "password": password})
		done = "account " + id + " created"
	case "password":
		if password != r.FormValue("confirm") {
			err = fmt.Errorf("the passwords do not match")
			break
		}
		err = p.accountJSON(http.MethodPut, "/"+url.PathEscape(id)+"/password", map[string]any{"password": password})
		done = "password of " + id + " changed"
	case "disable", "enable":
		disabled := r.FormValue("action") == "disable"
		err = p.accountJSON(http.MethodPut, "/"+url.PathEscape(id)+"/disabled", map[string]any{"disabled": disabled})
		done = "account " + id + " " + r.FormValue("action") + "d"
	default:
		err = fmt.Errorf("unknown account action")
	}
	back := url.Values{"id": {id}}
	if err != nil {
		back.Set("err", err.Error())
	} else {
		back.Set("ok", done)
	}
	http.Redirect(w, r, "/accounts?"+back.Encode(), http.StatusSeeOther)
}

/*
================
accountJSON
================
*/
func (p *panel) accountJSON(method, path string, document map[string]any) error {
	if id, _ := document["id"].(string); method == http.MethodPost && id == "" {
		return fmt.Errorf("an account needs an id")
	}
	body, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = p.accounts(method, path, body)
	return err
}

/*
================
accounts

One call to the provisioning API through the relay.
================
*/
func (p *panel) accounts(method, path string, body []byte) ([]byte, error) {
	token, err := readToken(p.config.ProvisioningTokenFile)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(context.Background(), method,
		strings.TrimSuffix(p.config.AccountsURL, "/")+"/v1/accounts"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return p.do(request)
}

//============================================================================

/*
================
do

A 2xx answer's body, or the upstream's own message as the error.
================
*/
func (p *panel) do(request *http.Request) ([]byte, error) {
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit))
	if err != nil {
		return nil, err
	}
	if response.StatusCode/100 != 2 {
		message := strings.TrimSpace(string(body))
		var document struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &document) == nil && document.Message != "" {
			message = document.Message
		}
		return nil, fmt.Errorf("%s (%d)", message, response.StatusCode)
	}
	return body, nil
}

/*
================
render
================
*/
func (p *panel) render(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := p.pages.ExecuteTemplate(w, "page.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

/*
================
readToken

Read on every call, so a token provisioned after the panel started works
without a restart.
================
*/
func readToken(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("token %s: %w", path, err)
	}
	return strings.TrimSpace(string(bytes)), nil
}

/*
================
requestID

The GameWorld's request id ([a-zA-Z0-9_-]{16,80}), fresh per form: a
resubmitted form is a new, deliberate operation.
================
*/
func requestID() string {
	var bytes [16]byte
	rand.Read(bytes[:])
	return "panel-" + hex.EncodeToString(bytes[:])
}

/*
================
indent
================
*/
func indent(body []byte) string {
	var out bytes.Buffer
	if json.Indent(&out, body, "", "  ") != nil {
		return string(body)
	}
	return out.String()
}

/*
================
field

A character field for display: an absent value reads as a dash.
================
*/
func field(character map[string]any, name string) string {
	value, found := character[name]
	if !found || value == nil {
		return "-"
	}
	if number, ok := value.(float64); ok {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	if text, ok := value.(string); ok {
		return text
	}
	bytes, _ := json.Marshal(value)
	return string(bytes)
}
