/*
===========================================================================

provision.go - seeded load accounts, made real on the isolated stack

A staircase needs hundreds of accounts that can actually sign in. They
are provisioned through the Agent's own loopback provisioning API (the
website's channel, internal/agent/provisioning), never by writing the
account store, and each bot's character through the real
/character/create. Provisioning is idempotent: an account that exists
with the run's password is kept, one that exists with another password
stops the run - quietly reusing it would sign the bots into someone
else's fixture. Nothing here runs unless -provision is passed, and the
target guard (target.go) has already refused anything but an isolated
stack.

===========================================================================
*/

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// provisionWorkers bounds the provisioning fan-out: account creation is
// bcrypt work on the Agent, so a modest pool keeps it busy without
// queueing behind its own password slots.
const provisionWorkers = 8

// provisionTimeout bounds one provisioning request.
const provisionTimeout = 30 * time.Second

/*
================
ensureAccounts

The run's account list: the -accounts file when it exists, or a
generated one when it does not and -bots names the size. Generated
accounts share one random password - they are load fixtures on an
isolated stack, and the file that holds them is written 0600 beside
the operator's chosen path.
================
*/
func ensureAccounts(path string, bots int) ([]botAccount, error) {
	accounts, err := loadAccounts(path)
	if err == nil {
		return accounts, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if bots <= 0 {
		return nil, fmt.Errorf("accounts file %s does not exist and -bots does not name a size to generate", path)
	}
	password, err := randomPassword()
	if err != nil {
		return nil, err
	}
	accounts = make([]botAccount, 0, bots)
	for i := 0; i < bots; i++ {
		accounts = append(accounts, botAccount{
			ID:        fmt.Sprintf("load%04d", i+1),
			Password:  password,
			Character: fmt.Sprintf("Load%04d", i+1),
		})
	}
	raw, err := json.MarshalIndent(accounts, "", "\t")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return nil, err
	}
	fmt.Printf("generated %d load accounts in %s (one shared password; isolated stack only)\n", bots, path)
	return accounts, nil
}

/*
================
randomPassword
================
*/
func randomPassword() (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// provisionClient is the Agent's account-provisioning API with its
// bearer token (the cluster state's agent-provisioning-token file).
type provisionClient struct {
	base  string
	token string
	http  *http.Client
}

/*
================
newProvisionClient
================
*/
func newProvisionClient(base, tokenFile string) (*provisionClient, error) {
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("provisioning token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return nil, fmt.Errorf("provisioning token file %s is empty", tokenFile)
	}
	return &provisionClient{
		base:  strings.TrimSuffix(base, "/"),
		token: token,
		http:  &http.Client{Timeout: provisionTimeout, Transport: &http.Transport{Proxy: nil}},
	}, nil
}

// provisionReply is the provisioning API's error shape.
type provisionReply struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

/*
================
call

One provisioning request; the reply decoded into out. The caller reads
the status: this API answers refusals with a code, not silence.
================
*/
func (p *provisionClient) call(method, path string, body any, out any) (int, provisionReply, error) {
	var reply provisionReply
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, reply, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, p.base+path, reader)
	if err != nil {
		return 0, reply, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return 0, reply, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return resp.StatusCode, reply, err
	}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &reply)
		if out != nil {
			_ = json.Unmarshal(data, out)
		}
	}
	return resp.StatusCode, reply, nil
}

/*
================
ensureAccount

Create the account, or prove the existing one holds the run's password.
created reports which of the two happened.
================
*/
func (p *provisionClient) ensureAccount(acct botAccount) (created bool, err error) {
	status, reply, err := p.call(http.MethodPost, "/v1/accounts", map[string]any{
		"id": acct.ID, "password": acct.Password,
	}, nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusCreated:
		return true, nil
	case http.StatusConflict:
		var verdict struct {
			Valid bool `json:"valid"`
		}
		vstatus, _, err := p.call(http.MethodPost, "/v1/accounts/"+acct.ID+"/verify", map[string]any{
			"password": acct.Password,
		}, &verdict)
		if err != nil {
			return false, err
		}
		if vstatus != http.StatusOK {
			return false, fmt.Errorf("provisioning verify for %s answered status %d", acct.ID, vstatus)
		}
		if !verdict.Valid {
			return false, fmt.Errorf("account %s already exists with a different password; refusing to reuse it", acct.ID)
		}
		return false, nil
	default:
		return false, fmt.Errorf("provisioning create for %s answered %d (%s: %s)", acct.ID, status, reply.Code, reply.Message)
	}
}

// provisionOutcome is one account's provisioning result, tallied by
// provisionRun for the run's one summary line.
type provisionOutcome struct {
	accountCreated   bool
	characterCreated bool
	err              error
}

/*
================
provisionRun

Make every account real and give it the character the run will enter
with. accounts[i].Character is resolved in place (the named character,
created when absent; the first existing one when unnamed), so the
steps enter deterministically. Any refusal aborts the whole run before
a step starts: a staircase over a half-provisioned roster would report
load the server never carried.
================
*/
func provisionRun(cfg runConfig, prov *provisionClient, accounts []botAccount) error {
	outcomes := make(chan provisionOutcome, len(accounts))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < provisionWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				outcomes <- provisionOne(cfg, prov, accounts, i)
			}
		}()
	}
	for i := range accounts {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(outcomes)

	var created, kept, charsMade int
	var firstErr error
	for outcome := range outcomes {
		if outcome.err != nil {
			if firstErr == nil {
				firstErr = outcome.err
			}
			continue
		}
		if outcome.accountCreated {
			created++
		} else {
			kept++
		}
		if outcome.characterCreated {
			charsMade++
		}
	}
	fmt.Printf("provisioning: %d accounts created, %d already present, %d characters created\n", created, kept, charsMade)
	return firstErr
}

/*
================
provisionOne

One account end to end: provisioned, signed in, and holding its
character. The account's Character is set to the character the run
will enter with.
================
*/
func provisionOne(cfg runConfig, prov *provisionClient, accounts []botAccount, index int) provisionOutcome {
	acct := accounts[index]
	createdAccount, err := prov.ensureAccount(acct)
	if err != nil {
		return provisionOutcome{err: err}
	}
	agent, err := newAgentClient(cfg.agentURL, cfg.serverID, cfg.division, cfg.timeout, botSourceIP(index))
	if err != nil {
		return provisionOutcome{err: err}
	}
	if err := agent.login(acct.ID, acct.Password); err != nil {
		return provisionOutcome{err: fmt.Errorf("provisioning %s: %w", acct.ID, err)}
	}
	names, err := agent.characterNames()
	if err != nil {
		return provisionOutcome{err: fmt.Errorf("provisioning %s: %w", acct.ID, err)}
	}
	want := acct.Character
	if want == "" {
		want = fmt.Sprintf("Load%04d", index+1)
	}
	for _, name := range names {
		if name == want {
			accounts[index].Character = want
			return provisionOutcome{accountCreated: createdAccount}
		}
	}
	if acct.Character == "" && len(names) > 0 {
		accounts[index].Character = names[0]
		return provisionOutcome{accountCreated: createdAccount}
	}
	if err := agent.createCharacter(want); err != nil {
		return provisionOutcome{err: fmt.Errorf("provisioning %s: %w", acct.ID, err)}
	}
	accounts[index].Character = want
	return provisionOutcome{accountCreated: createdAccount, characterCreated: true}
}
