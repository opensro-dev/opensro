package shard

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
)

const (
	DefaultHeartbeatInterval = 3 * time.Second
	DefaultAcquireTimeout    = DefaultLeaseTTL + 5*time.Second
	ControlShardHeader       = "X-SRO-Shard-ID"
	maxHeartbeatResponse     = 64 << 10
	maxConsecutiveFailures   = 3
)

// ErrLeaseContended means another short-lived process identity still owns the
// shard. Startup may wait through one lease TTL; all other reporter failures
// remain immediately fatal.
var ErrLeaseContended = errors.New("shard lease is temporarily contended")

// Reporter publishes one GameWorld process's renewable ownership lease.
type Reporter struct {
	AgentURL     string
	IdentityFile string
	ShardID      string
	Population   func() int
	HTTPClient   *http.Client
	Interval     time.Duration

	instanceID string
	mu         sync.Mutex
	sequence   uint64
	admitting  bool
	// kick asks Run for one heartbeat now (MarkAdmitting), so the title
	// learns of admission without a second, racing publisher.
	kick chan struct{}
}

func NewReporter(
	agentURL string,
	identityFile string,
	shardID string,
	population func() int,
) (*Reporter, error) {
	if strings.TrimSpace(agentURL) == "" {
		return nil, fmt.Errorf("shard reporter requires Agent URL")
	}
	if strings.TrimSpace(identityFile) == "" {
		return nil, fmt.Errorf("shard reporter requires workload identity file")
	}
	if strings.TrimSpace(shardID) == "" {
		return nil, fmt.Errorf("shard reporter requires shard id")
	}
	if population == nil {
		return nil, fmt.Errorf("shard reporter requires population source")
	}
	instanceBytes := make([]byte, 16)
	if _, err := rand.Read(instanceBytes); err != nil {
		return nil, fmt.Errorf("shard reporter instance id: %w", err)
	}
	return &Reporter{
		AgentURL:     strings.TrimSuffix(agentURL, "/"),
		IdentityFile: identityFile,
		ShardID:      shardID,
		Population:   population,
		HTTPClient:   &http.Client{Timeout: 5 * time.Second},
		Interval:     DefaultHeartbeatInterval,
		instanceID:   hex.EncodeToString(instanceBytes),
		kick:         make(chan struct{}, 1),
	}, nil
}

/*
================
MarkAdmitting

Called once GameWorld readiness opens: every later heartbeat says
PhaseAdmitting for the rest of this process, and Run publishes one now.
================
*/
func (reporter *Reporter) MarkAdmitting() {
	reporter.mu.Lock()
	reporter.admitting = true
	reporter.mu.Unlock()
	select {
	case reporter.kick <- struct{}{}:
	default:
	}
}

// Publish sends one heartbeat. It is also the startup lease acquisition.
func (reporter *Reporter) Publish(ctx context.Context) error {
	reporter.mu.Lock()
	reporter.sequence++
	phase := PhaseStarting
	if reporter.admitting {
		phase = PhaseAdmitting
	}
	heartbeat := Heartbeat{
		ShardID:       reporter.ShardID,
		InstanceID:    reporter.instanceID,
		Sequence:      reporter.sequence,
		OnlinePlayers: reporter.Population(),
		Phase:         phase,
	}
	reporter.mu.Unlock()

	payload, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		reporter.AgentURL+"/internal/cluster/shards/heartbeat",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	if err := reporter.authorize(request); err != nil {
		return err
	}
	request.Header.Set(ControlShardHeader, reporter.ShardID)
	request.Header.Set("Content-Type", "application/json")
	response, err := reporter.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxHeartbeatResponse))
	if response.StatusCode != http.StatusOK {
		responseErr := fmt.Errorf(
			"agent heartbeat refused with HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
		if response.StatusCode == http.StatusConflict {
			return fmt.Errorf("%w: %v", ErrLeaseContended, responseErr)
		}
		return responseErr
	}
	return nil
}

// Acquire obtains the initial lease, waiting through at most one expired
// predecessor. This absorbs antivirus-capture copies and crash residue inside
// one scheduler-tracked process instead of causing a capture/restart loop.
func (reporter *Reporter) Acquire(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultAcquireTimeout
	}
	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		err := reporter.Publish(waitContext)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrLeaseContended) {
			return err
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-waitContext.Done():
			timer.Stop()
			return fmt.Errorf("waiting for prior shard lease: %w", errors.Join(err, waitContext.Err()))
		case <-timer.C:
		}
	}
}

// Release gives up a healthy worker's lease during ordered shutdown. A crash
// cannot call it, so crash takeover still waits for normal expiry.
func (reporter *Reporter) Release(ctx context.Context) error {
	payload, err := json.Marshal(struct {
		ShardID    string `json:"shardId"`
		InstanceID string `json:"instanceId"`
	}{
		ShardID:    reporter.ShardID,
		InstanceID: reporter.instanceID,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		reporter.AgentURL+"/internal/cluster/shards/release",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	if err := reporter.authorize(request); err != nil {
		return err
	}
	request.Header.Set(ControlShardHeader, reporter.ShardID)
	request.Header.Set("Content-Type", "application/json")
	response, err := reporter.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxHeartbeatResponse))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"agent lease release refused with HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	return nil
}

// AccountIDs reads the Agent-owned public account identity set. Password
// hashes never cross this boundary. GameWorld uses the snapshot once during
// startup to prove that every durable character owner can authenticate.
func (reporter *Reporter) AccountIDs(ctx context.Context) ([]string, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		reporter.AgentURL+"/internal/accounts",
		nil,
	)
	if err != nil {
		return nil, err
	}
	if err := reporter.authorize(request); err != nil {
		return nil, err
	}
	request.Header.Set(ControlShardHeader, reporter.ShardID)
	response, err := reporter.HTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(
		response.Body,
		maxHeartbeatResponse+1,
	))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHeartbeatResponse {
		return nil, fmt.Errorf("agent account directory exceeds response limit")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"agent account directory refused with HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	var document struct {
		AccountIDs []string `json:"accountIds"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode Agent account directory: %w", err)
	}
	if len(document.AccountIDs) == 0 {
		return nil, fmt.Errorf("agent account directory is empty")
	}
	seen := make(map[string]struct{}, len(document.AccountIDs))
	for _, accountID := range document.AccountIDs {
		if !domain.AccountIDValid(accountID) {
			return nil, fmt.Errorf(
				"agent account directory contains invalid id %q",
				accountID,
			)
		}
		if _, duplicate := seen[accountID]; duplicate {
			return nil, fmt.Errorf(
				"agent account directory contains duplicate id %q",
				accountID,
			)
		}
		seen[accountID] = struct{}{}
	}
	return append([]string(nil), document.AccountIDs...), nil
}

func (reporter *Reporter) authorize(request *http.Request) error {
	token, err := os.ReadFile(reporter.IdentityFile)
	if err != nil {
		return fmt.Errorf("read Nomad workload identity: %w", err)
	}
	identity := strings.TrimSpace(string(token))
	if identity == "" {
		return fmt.Errorf("nomad workload identity file is empty")
	}
	request.Header.Set("Authorization", "Bearer "+identity)
	return nil
}

// Run renews until cancellation. Three consecutive failures terminate the
// worker because it can no longer prove exclusive ownership of the shard.
func (reporter *Reporter) Run(ctx context.Context) error {
	interval := reporter.Interval
	if interval <= 0 {
		interval = DefaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-reporter.kick:
			// A missed admission kick only waits for the next tick.
			if err := reporter.Publish(ctx); err == nil {
				failures = 0
			}
		case <-ticker.C:
			if err := reporter.Publish(ctx); err != nil {
				failures++
				if failures < maxConsecutiveFailures {
					continue
				}
				return err
			}
			failures = 0
		}
	}
}
