/*
===========================================================================

agent.go - the bot's HTTP flow against the Agent

The bot logs in and mints its two one-use tickets exactly like the browser
client: /title/login, then /auth/transport-token and /auth/enterworld-token
with the session bearer. Every request declares the release protocol.

Phase 1 of issue #74 (issue #75).

===========================================================================
*/
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"opensro.online/server/internal/releaseprotocol"
)

const (
	// PathLogin is the browser login route.
	PathLogin = "/title/login"
	// PathTransportToken mints the HELLO admission ticket.
	PathTransportToken = "/auth/transport-token"
	// PathEnterWorldToken mints the enter-world bind ticket.
	PathEnterWorldToken = "/auth/enterworld-token"

	maxAgentResponseBytes = 64 << 10
)

// AgentClient speaks the Agent's browser-facing HTTP surface. The bot uses
// the returned session token as a bearer; it does not keep a cookie jar.
type AgentClient struct {
	// BaseURL is the Agent origin, for example http://127.0.0.1:8787.
	BaseURL string
	// HTTP is the client. Nil uses a bounded default.
	HTTP *http.Client
	// Protocol is the release protocol to declare. Zero uses
	// releaseprotocol.Current.
	Protocol int
}

// LoginResult is the accepted login.
type LoginResult struct {
	SessionToken string
	DivisionID   string
	TransportURL string
}

func (a AgentClient) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (a AgentClient) protocol() int {
	if a.Protocol != 0 {
		return a.Protocol
	}
	return releaseprotocol.Current
}

// post sends one bounded JSON request and decodes the JSON answer.
func (a AgentClient) post(ctx context.Context, path, bearer string, body any) (map[string]any, error) {
	base := strings.TrimSuffix(a.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("bot: agent base URL is empty")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(releaseprotocol.Header, fmt.Sprintf("%d", a.protocol()))
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := a.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("bot: %s: %w", path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAgentResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("bot: %s: read: %w", path, err)
	}
	if len(data) > maxAgentResponseBytes {
		return nil, fmt.Errorf("bot: %s: response over %d bytes", path, maxAgentResponseBytes)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bot: %s: status %d: %s", path, response.StatusCode, strings.TrimSpace(string(data)))
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("bot: %s: decode: %w", path, err)
	}
	return decoded, nil
}

func stringField(decoded map[string]any, key string) string {
	value, _ := decoded[key].(string)
	return value
}

func okField(decoded map[string]any) bool {
	ok, _ := decoded["ok"].(bool)
	return ok
}

// Login authenticates the bot account and returns the session.
func (a AgentClient) Login(ctx context.Context, accountID, password, serverID, divisionID string) (LoginResult, error) {
	decoded, err := a.post(ctx, PathLogin, "", map[string]any{
		"id":         accountID,
		"password":   password,
		"serverId":   serverID,
		"divisionId": divisionID,
	})
	if err != nil {
		return LoginResult{}, err
	}
	if !okField(decoded) {
		return LoginResult{}, fmt.Errorf("bot: login refused: %s", refusal(decoded))
	}
	result := LoginResult{
		SessionToken: stringField(decoded, "sessionToken"),
		DivisionID:   stringField(decoded, "divisionId"),
		TransportURL: stringField(decoded, "transportUrl"),
	}
	if result.SessionToken == "" {
		return LoginResult{}, fmt.Errorf("bot: login answer has no session token")
	}
	return result, nil
}

// TransportToken mints the one-use HELLO admission ticket.
func (a AgentClient) TransportToken(ctx context.Context, sessionToken string) (string, error) {
	decoded, err := a.post(ctx, PathTransportToken, sessionToken, map[string]any{})
	if err != nil {
		return "", err
	}
	if !okField(decoded) {
		return "", fmt.Errorf("bot: transport token refused: %s", refusal(decoded))
	}
	token := stringField(decoded, "token")
	if token == "" {
		return "", fmt.Errorf("bot: transport token answer is empty")
	}
	return token, nil
}

// EnterWorldToken mints the one-use enter-world bind ticket.
func (a AgentClient) EnterWorldToken(ctx context.Context, sessionToken, divisionID, characterName string) (string, error) {
	decoded, err := a.post(ctx, PathEnterWorldToken, sessionToken, map[string]any{
		"characterName": characterName,
		"divisionId":    divisionID,
	})
	if err != nil {
		return "", err
	}
	if !okField(decoded) {
		return "", fmt.Errorf("bot: enter-world token refused: %s", refusal(decoded))
	}
	token := stringField(decoded, "token")
	if token == "" {
		return "", fmt.Errorf("bot: enter-world token answer is empty")
	}
	return token, nil
}

func refusal(decoded map[string]any) string {
	if code := stringField(decoded, "code"); code != "" {
		return code
	}
	return "unknown"
}
