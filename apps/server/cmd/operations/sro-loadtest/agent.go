/*
===========================================================================

agent.go - the load bot's Agent client: real sign-in and ticket mints

A bot reaches the world the way the browser does: it signs in to the
Agent, asks for the one-use transport admission ticket and the
per-character EnterWorld bind token, and only then opens a game
connection. Nothing here mints or forges a credential; a refusal at any
step is the bot's named failure in the run report.

===========================================================================
*/

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// agentClient is one bot's signed-in Agent session. The cookie jar holds
// the browser session the sign-in sets; the token endpoints sit behind
// that same session, exactly as they do for the browser.
type agentClient struct {
	base     string
	http     *http.Client
	serverID string
	division string
}

// tokenReply is the shared shape of the two ticket mints.
type tokenReply struct {
	OK    bool   `json:"ok"`
	Code  string `json:"code"`
	Token string `json:"token"`
}

/*
================
newAgentClient
================
*/
func newAgentClient(base, serverID, division string, timeout time.Duration) (*agentClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &agentClient{
		base:     strings.TrimSuffix(base, "/"),
		http:     &http.Client{Jar: jar, Timeout: timeout},
		serverID: serverID,
		division: division,
	}, nil
}

/*
================
postJSON

One JSON POST against the Agent; the reply decoded into out. Agent
endpoints answer 200 with an ok flag for refusals, so the caller reads
the flag instead of the status where that is the contract.
================
*/
func (c *agentClient) postJSON(path string, body any, out any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("agent %s: undecodable reply: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

/*
================
login

POST /title/login with the account credential; success is the session
cookie the jar keeps plus ok=true in the reply.
================
*/
func (c *agentClient) login(accountID, password string) error {
	var reply struct {
		OK   bool   `json:"ok"`
		Code string `json:"code"`
	}
	_, err := c.postJSON("/title/login", map[string]any{
		"id":         accountID,
		"password":   password,
		"serverId":   c.serverID,
		"divisionId": c.division,
	}, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return fmt.Errorf("agent login refused: %s", reply.Code)
	}
	return nil
}

/*
================
transportToken

The one-use HELLO admission ticket. Consumed by the GameWorld only once
WELCOME is written, so a bot that fails later may retry with a fresh one.
================
*/
func (c *agentClient) transportToken() (string, error) {
	return c.mint("/auth/transport-token", map[string]any{})
}

/*
================
enterWorldToken

The per-character EnterWorld bind token, minted immediately before the
bot sends its enter-world frame.
================
*/
func (c *agentClient) enterWorldToken(character string) (string, error) {
	return c.mint("/auth/enterworld-token", map[string]any{
		"characterName": character,
		"divisionId":    c.division,
	})
}

/*
================
mint
================
*/
func (c *agentClient) mint(path string, body map[string]any) (string, error) {
	var reply tokenReply
	if _, err := c.postJSON(path, body, &reply); err != nil {
		return "", err
	}
	if !reply.OK || reply.Token == "" {
		return "", fmt.Errorf("agent %s refused: %s", path, reply.Code)
	}
	return reply.Token, nil
}

/*
================
characterNames

The account's characters on this division, in the Agent's order. The bot
enters with the first one unless the run names one.
================
*/
func (c *agentClient) characterNames() ([]string, error) {
	var reply struct {
		OK         bool   `json:"ok"`
		Code       string `json:"code"`
		Characters []struct {
			Name string `json:"name"`
		} `json:"characters"`
	}
	if _, err := c.postJSON("/character/list", map[string]any{"divisionId": c.division}, &reply); err != nil {
		return nil, err
	}
	if !reply.OK {
		return nil, fmt.Errorf("agent character list refused: %s", reply.Code)
	}
	names := make([]string, 0, len(reply.Characters))
	for _, ch := range reply.Characters {
		names = append(names, ch.Name)
	}
	return names, nil
}
