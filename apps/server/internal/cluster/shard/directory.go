package shard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const DefaultLeaseTTL = 10 * time.Second
const maxDirectoryStateBytes int64 = 64 << 10

var (
	ErrUnknownShard      = errors.New("unknown shard")
	ErrShardDisabled     = errors.New("shard is disabled")
	ErrShardAlreadyOwned = errors.New("shard already has a live owner")
	ErrStaleHeartbeat    = errors.New("stale shard heartbeat")
	ErrLeaseNotOwned     = errors.New("shard lease is not owned by instance")
)

// Heartbeat is one GameWorld process's health and population publication.
// InstanceID must be freshly generated for every process boot. Phase says
// whether the process admits players yet; empty is a GameWorld from before
// the field, judged by its lease alone.
type Heartbeat struct {
	ShardID       string `json:"shardId"`
	InstanceID    string `json:"instanceId"`
	Sequence      uint64 `json:"sequence"`
	OnlinePlayers int    `json:"onlinePlayers"`
	Phase         string `json:"phase,omitempty"`
}

const (
	// PhaseStarting: the process holds its lease but is still loading its
	// worlds; the title shows the shard offline and login is refused.
	PhaseStarting = "starting"
	// PhaseAdmitting: GameWorld readiness has opened since this boot. It
	// latches for the process, so a long tick never flaps the title.
	PhaseAdmitting = "admitting"
)

type lease struct {
	instanceID    string
	sequence      uint64
	onlinePlayers int
	starting      bool
	expiresAt     time.Time
}

// Status is one Agent title-list snapshot. Starting is a live owner that
// holds its lease but has not opened admission yet: not operating, but
// worth waiting for.
type Status struct {
	Definition
	OnlinePlayers int
	Operating     bool
	Starting      bool
}

// Directory owns live GameWorld leases. At most one non-expired process may
// publish a given shard, preventing two authorities from serving one world.
type Directory struct {
	catalog  *Catalog
	leaseTTL time.Duration

	mu     sync.RWMutex
	leases map[string]lease
	path   string
}

func NewDirectory(catalog *Catalog, leaseTTL time.Duration) (*Directory, error) {
	if catalog == nil {
		return nil, fmt.Errorf("shard directory requires a catalog")
	}
	if leaseTTL <= 0 {
		leaseTTL = DefaultLeaseTTL
	}
	return &Directory{
		catalog:  catalog,
		leaseTTL: leaseTTL,
		leases:   make(map[string]lease),
	}, nil
}

// NewPersistentDirectory restores the lease generation across Agent restarts.
// Expiry remains wall-clock based; expired rows are harmless and replaceable.
func NewPersistentDirectory(
	catalog *Catalog,
	leaseTTL time.Duration,
	path string,
) (*Directory, error) {
	directory, err := NewDirectory(catalog, leaseTTL)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("shard directory state path is required")
	}
	directory.path = path
	if err := directory.load(); err != nil {
		return nil, err
	}
	return directory, nil
}

// Publish renews a worker lease and updates its active player count.
func (directory *Directory) Publish(heartbeat Heartbeat, now time.Time) error {
	definition, ok := directory.catalog.Resolve(heartbeat.ShardID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownShard, heartbeat.ShardID)
	}
	if !definition.Enabled {
		return fmt.Errorf("%w: %q", ErrShardDisabled, heartbeat.ShardID)
	}
	if heartbeat.InstanceID == "" {
		return fmt.Errorf("shard heartbeat requires an instance id")
	}
	if heartbeat.Sequence == 0 {
		return fmt.Errorf("shard heartbeat sequence must start at one")
	}
	if heartbeat.Phase != "" && heartbeat.Phase != PhaseStarting && heartbeat.Phase != PhaseAdmitting {
		return fmt.Errorf("shard heartbeat phase %q is unknown", heartbeat.Phase)
	}
	if heartbeat.OnlinePlayers < 0 || heartbeat.OnlinePlayers > definition.Capacity {
		return fmt.Errorf(
			"shard %q population %d is outside 0..%d",
			heartbeat.ShardID,
			heartbeat.OnlinePlayers,
			definition.Capacity,
		)
	}

	directory.mu.Lock()
	defer directory.mu.Unlock()

	current, exists := directory.leases[heartbeat.ShardID]
	if exists && now.Before(current.expiresAt) {
		if current.instanceID != heartbeat.InstanceID {
			return fmt.Errorf(
				"%w: shard %q is owned by instance %q until %s",
				ErrShardAlreadyOwned,
				heartbeat.ShardID,
				current.instanceID,
				current.expiresAt.UTC().Format(time.RFC3339Nano),
			)
		}
		if heartbeat.Sequence <= current.sequence {
			return fmt.Errorf(
				"%w: shard %q sequence %d is not newer than %d",
				ErrStaleHeartbeat,
				heartbeat.ShardID,
				heartbeat.Sequence,
				current.sequence,
			)
		}
	}

	next := lease{
		instanceID:    heartbeat.InstanceID,
		sequence:      heartbeat.Sequence,
		onlinePlayers: heartbeat.OnlinePlayers,
		starting:      heartbeat.Phase == PhaseStarting,
		expiresAt:     now.Add(directory.leaseTTL),
	}
	if directory.path != "" {
		candidate := make(map[string]lease, len(directory.leases)+1)
		for shardID, current := range directory.leases {
			candidate[shardID] = current
		}
		candidate[heartbeat.ShardID] = next
		if err := persistLeaseState(directory.path, candidate); err != nil {
			return fmt.Errorf("persist shard directory: %w", err)
		}
	}
	directory.leases[heartbeat.ShardID] = next
	return nil
}

// Release removes a lease only when the graceful caller still owns it.
func (directory *Directory) Release(shardID, instanceID string) error {
	if _, ok := directory.catalog.Resolve(shardID); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownShard, shardID)
	}
	if instanceID == "" {
		return fmt.Errorf("shard lease release requires an instance id")
	}
	directory.mu.Lock()
	defer directory.mu.Unlock()
	current, exists := directory.leases[shardID]
	if !exists || current.instanceID != instanceID {
		return fmt.Errorf("%w: %q", ErrLeaseNotOwned, shardID)
	}
	if directory.path != "" {
		candidate := make(map[string]lease, len(directory.leases)-1)
		for currentShardID, lease := range directory.leases {
			if currentShardID != shardID {
				candidate[currentShardID] = lease
			}
		}
		if err := persistLeaseState(directory.path, candidate); err != nil {
			return fmt.Errorf("persist shard directory release: %w", err)
		}
	}
	delete(directory.leases, shardID)
	return nil
}

// Snapshot returns stable native-order status. A configured shard is
// operating only while its sole GameWorld owner holds a fresh lease and
// has opened admission (a starting owner is not yet operating).
func (directory *Directory) Snapshot(now time.Time) []Status {
	definitions := directory.catalog.Definitions()

	directory.mu.RLock()
	defer directory.mu.RUnlock()

	statuses := make([]Status, 0, len(definitions))
	for _, definition := range definitions {
		current, exists := directory.leases[definition.ID]
		live := definition.Enabled && exists && now.Before(current.expiresAt)
		operating := live && !current.starting
		online := 0
		if operating {
			online = current.onlinePlayers
		}
		statuses = append(statuses, Status{
			Definition:    definition,
			OnlinePlayers: online,
			Operating:     operating,
			Starting:      live && current.starting,
		})
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		return statuses[i].NativeServerID < statuses[j].NativeServerID
	})
	return statuses
}

type persistedDirectory struct {
	Version int              `json:"version"`
	Leases  []persistedLease `json:"leases"`
}

type persistedLease struct {
	ShardID       string    `json:"shardId"`
	InstanceID    string    `json:"instanceId"`
	Sequence      uint64    `json:"sequence"`
	OnlinePlayers int       `json:"onlinePlayers"`
	Starting      bool      `json:"starting,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func (directory *Directory) load() error {
	info, err := os.Lstat(directory.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect shard directory state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("shard directory state is not a regular file")
	}
	if info.Size() > maxDirectoryStateBytes {
		return fmt.Errorf(
			"shard directory state is %d bytes, limit is %d",
			info.Size(),
			maxDirectoryStateBytes,
		)
	}
	payload, err := os.ReadFile(directory.path)
	if err != nil {
		return fmt.Errorf("read shard directory state: %w", err)
	}
	var document persistedDirectory
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode shard directory state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("decode shard directory trailing data")
	}
	if document.Version != 1 {
		return fmt.Errorf(
			"shard directory state version %d, want 1",
			document.Version,
		)
	}
	for _, row := range document.Leases {
		definition, ok := directory.catalog.Resolve(row.ShardID)
		if !ok {
			return fmt.Errorf(
				"shard directory state references unknown shard %q",
				row.ShardID,
			)
		}
		if row.InstanceID == "" || row.Sequence == 0 ||
			row.OnlinePlayers < 0 ||
			row.OnlinePlayers > definition.Capacity ||
			row.ExpiresAt.IsZero() {
			return fmt.Errorf(
				"shard directory state has invalid lease for %q",
				row.ShardID,
			)
		}
		if _, duplicate := directory.leases[row.ShardID]; duplicate {
			return fmt.Errorf(
				"shard directory state duplicates shard %q",
				row.ShardID,
			)
		}
		directory.leases[row.ShardID] = lease{
			instanceID:    row.InstanceID,
			sequence:      row.Sequence,
			onlinePlayers: row.OnlinePlayers,
			starting:      row.Starting,
			expiresAt:     row.ExpiresAt,
		}
	}
	return nil
}

func persistLeaseState(path string, leases map[string]lease) error {
	rows := make([]persistedLease, 0, len(leases))
	for shardID, current := range leases {
		rows = append(rows, persistedLease{
			ShardID:       shardID,
			InstanceID:    current.instanceID,
			Sequence:      current.sequence,
			OnlinePlayers: current.onlinePlayers,
			Starting:      current.starting,
			ExpiresAt:     current.expiresAt,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ShardID < rows[j].ShardID
	})
	payload, err := json.MarshalIndent(persistedDirectory{
		Version: 1,
		Leases:  rows,
	}, "", "  ")
	if err != nil {
		return err
	}
	if int64(len(payload)) > maxDirectoryStateBytes {
		return fmt.Errorf(
			"encoded state exceeds %d bytes",
			maxDirectoryStateBytes,
		)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
