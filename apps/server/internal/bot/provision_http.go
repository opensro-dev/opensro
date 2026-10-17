/*
===========================================================================

provision_http.go - create a bot account and character over HTTP

The provisioning path creates the account through the Agent provisioning API
and the character through the Agent browser API, so it never writes state.db
while the GameWorld runs. It needs the provisioning bearer token from the
cluster state.

Phase 1 of issue #74 (issue #75).

===========================================================================
*/
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// PathAccounts is the Agent provisioning API route.
	PathAccounts = "/v1/accounts"
	// PathCharacterCreate is the Agent browser character-create route.
	PathCharacterCreate = "/character/create"
	// PathCharacterList is the Agent browser character-list route.
	PathCharacterList = "/character/list"

	maxProvisionResponseBytes = 16 << 10
)

// AgentProvisioner creates a bot account over HTTP.
type AgentProvisioner struct {
	// ProvisioningURL is the Agent provisioning API origin, for example
	// http://127.0.0.1:8789.
	ProvisioningURL string
	// ProvisioningToken is the bearer token from the cluster state.
	ProvisioningToken string
	// HTTP is the client for the provisioning API. Nil uses a bounded default.
	HTTP *http.Client
}

func (p AgentProvisioner) httpClient() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// CreateAccount creates the account with the given password. The Agent
// provisioning API answers 201.
func (p AgentProvisioner) CreateAccount(ctx context.Context, accountID, password string) error {
	base := strings.TrimSuffix(p.ProvisioningURL, "/")
	if base == "" || p.ProvisioningToken == "" {
		return errors.New("bot: provisioning URL and token are required")
	}
	payload, err := json.Marshal(map[string]string{"id": accountID, "password": password})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+PathAccounts, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.ProvisioningToken)
	response, err := p.httpClient().Do(request)
	if err != nil {
		return fmt.Errorf("bot: create account %s: %w", accountID, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProvisionResponseBytes))
	if response.StatusCode == http.StatusConflict {
		// The account exists from a partial run. Reset the password, so the
		// provision is idempotent and a retry finishes the bot.
		return p.SetPassword(ctx, accountID, password)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("bot: create account %s: status %d: %s", accountID, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// SetPassword replaces the password of an existing account through the
// provisioning API (PUT /v1/accounts/{id}/password).
func (p AgentProvisioner) SetPassword(ctx context.Context, accountID, password string) error {
	base := strings.TrimSuffix(p.ProvisioningURL, "/")
	if base == "" || p.ProvisioningToken == "" {
		return errors.New("bot: provisioning URL and token are required")
	}
	payload, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		return err
	}
	endpoint := base + PathAccounts + "/" + url.PathEscape(accountID) + "/password"
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.ProvisioningToken)
	response, err := p.httpClient().Do(request)
	if err != nil {
		return fmt.Errorf("bot: set password %s: %w", accountID, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProvisionResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("bot: set password %s: status %d: %s", accountID, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// CreateCharacter creates the character through the Agent browser API with
// the account session bearer. The defaults are a valid China appearance.
func (a AgentClient) CreateCharacter(ctx context.Context, sessionToken, divisionID, characterName, modelCodename string) error {
	if modelCodename == "" {
		modelCodename = DefaultModelCodename
	}
	decoded, err := a.post(ctx, PathCharacterCreate, sessionToken, map[string]any{
		"divisionId":     divisionID,
		"characterName":  characterName,
		"modelCodename":  modelCodename,
		"heightIndex":    0,
		"volumeIndex":    0,
		"weaponIndex":    1,
		"protectorIndex": 0,
		"armorSelected":  false,
		"weaponSelected": true,
	})
	if err != nil {
		return err
	}
	if native, ok := decoded["nativeResult"].(float64); ok && native == 1 {
		return nil
	}
	return fmt.Errorf("bot: create character %s refused: nativeErrorCode=%v", characterName, decoded["nativeErrorCode"])
}
