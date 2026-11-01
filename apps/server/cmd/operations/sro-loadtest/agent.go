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
	"net"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"

	"opensro.online/server/internal/releaseprotocol"
)

// loadCharacterModel is the model every provisioned load character is
// created with: the plain adventurer the Agent's own chargen tests use.
// A load bot needs a valid body, not a wardrobe.
const loadCharacterModel = "CHAR_CH_MAN_ADVENTURER"

// agentClient is one bot's signed-in Agent session. The cookie jar holds
// the browser session the sign-in sets, and the session token the login
// answers is carried as the bearer on the shard requests, exactly as
// the browser carries it (handlers.go: handleShardRequest reads bearer
// claims, not the cookie).
type agentClient struct {
	base     string
	http     *http.Client
	serverID string
	division string
	token    string
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

One bot's client. It declares the release protocol this build speaks
(releaseprotocol.Current - the Agent refuses any other with 426) and
dials from the bot's own loopback source address (target.go). No proxy:
a load target is dialed as itself.
================
*/
func newAgentClient(base, serverID, division string, timeout time.Duration, sourceIP net.IP) (*agentClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: nil}
	if sourceIP != nil {
		dialer := &net.Dialer{Timeout: timeout, LocalAddr: &net.TCPAddr{IP: sourceIP}}
		transport.DialContext = dialer.DialContext
	}
	return &agentClient{
		base:     strings.TrimSuffix(base, "/"),
		http:     &http.Client{Jar: jar, Timeout: timeout, Transport: transport},
		serverID: serverID,
		division: division,
	}, nil
}

/*
================
declare

The headers every Agent request carries: the release protocol this
build speaks, and the session bearer once the login has minted it. The
login itself sends no bearer - there is none yet.
================
*/
func (c *agentClient) declare(req *http.Request) {
	req.Header.Set(releaseprotocol.Header, strconv.Itoa(releaseprotocol.Current))
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
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
	c.declare(req)
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

POST /title/login with the account credential; success is ok=true, the
session cookie the jar keeps, and the session token the shard requests
carry as bearer. A 426 means the stack speaks another release protocol
than this tool: the two ship together, so the run stops naming it
instead of failing inside a decoder downstream.
================
*/
func (c *agentClient) login(accountID, password string) error {
	var reply struct {
		OK           bool   `json:"ok"`
		Code         string `json:"code"`
		SessionToken string `json:"sessionToken"`
	}
	status, err := c.postJSON("/title/login", map[string]any{
		"id":         accountID,
		"password":   password,
		"serverId":   c.serverID,
		"divisionId": c.division,
	}, &reply)
	if err != nil {
		return err
	}
	if status == http.StatusUpgradeRequired {
		return fmt.Errorf("agent speaks another release protocol (this tool declares %d); build the tool and the stack from one tree", releaseprotocol.Current)
	}
	if !reply.OK {
		return fmt.Errorf("agent login refused: %s", reply.Code)
	}
	if reply.SessionToken == "" {
		return fmt.Errorf("agent login answered ok without a session token")
	}
	c.token = reply.SessionToken
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
getJSON

One JSON GET against the Agent; the reply decoded into out. The
character list is answered only on GET, under the session the login
established; anything but a 200 is a refusal (the session middleware
answers 401 to an unsigned caller).
================
*/
func (c *agentClient) getJSON(path string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, err
	}
	c.declare(req)
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
characterNames

The account's characters on this division, in the Agent's order. The bot
enters with the first one unless the run names one. The roster reply
carries no ok flag: success is the 200 itself, and the shard is the
login session's claim, so there is nothing to send.
================
*/
func (c *agentClient) characterNames() ([]string, error) {
	var reply struct {
		Characters []struct {
			Name string `json:"name"`
		} `json:"characters"`
	}
	status, err := c.getJSON("/character/list", &reply)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("agent character list refused: status %d", status)
	}
	names := make([]string, 0, len(reply.Characters))
	for _, ch := range reply.Characters {
		names = append(names, ch.Name)
	}
	return names, nil
}

/*
================
createCharacter

POST /character/create with the plain adventurer draft (the body the
Agent's own chargen tests prove valid). The answer is the native
result pair: nativeResult 1 created the character, anything else is a
refusal carrying its native error code.
================
*/
func (c *agentClient) createCharacter(name string) error {
	var reply struct {
		NativeResult    int `json:"nativeResult"`
		NativeErrorCode int `json:"nativeErrorCode"`
	}
	_, err := c.postJSON("/character/create", map[string]any{
		"characterName":  name,
		"modelCodename":  loadCharacterModel,
		"heightIndex":    0,
		"volumeIndex":    0,
		"weaponIndex":    1,
		"protectorIndex": 0,
		"armorSelected":  false,
		"weaponSelected": true,
		"divisionId":     c.division,
	}, &reply)
	if err != nil {
		return err
	}
	if reply.NativeResult != 1 {
		return fmt.Errorf("agent character create refused: native error code %d", reply.NativeErrorCode)
	}
	return nil
}
