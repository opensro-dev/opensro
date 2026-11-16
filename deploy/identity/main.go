/*
===========================================================================

main.go - the identity service: key, JWKS listener and token writer

Two modes:

	identity -key <file> -out-dir <dir> -shards <id,...> [-listen addr]
	         [-issuer url] [-namespace ns]
	    Serve the JWKS and keep one token per shard fresh in out-dir.

	identity -derive-public <private ring> -out <file>
	    Write the GameWorld's public session key ring once and exit.

The service runs in the Agent's network namespace (docker compose
network_mode: service:agent): the Agent's verifier accepts plain HTTP
only on loopback, so the JWKS lives on 127.0.0.1 and no other container
can reach it.

===========================================================================
*/
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

/*
================
storedKey

The key file's shape: the Ed25519 seed and the key id the JWKS names.
================
*/
type storedKey struct {
	Kid  string `json:"kid"`
	Seed string `json:"seed"`
}

/*
================
tokenConfig
================
*/
type tokenConfig struct {
	key       ed25519.PrivateKey
	kid       string
	issuer    string
	namespace string
	outDir    string
	shards    []string
}

/*
================
loadOrCreateKey

Loads the key at path, creating it (mode 0600) only when the file does
not exist. An existing key is never replaced: the Agent caches the JWKS,
and a new key would refuse every token minted before the swap.
================
*/
func loadOrCreateKey(path string) (ed25519.PrivateKey, string, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, "", err
		}
		kid := fmt.Sprintf("silkroad-%d", time.Now().Unix())
		payload, err = json.Marshal(storedKey{Kid: kid, Seed: base64.RawURLEncoding.EncodeToString(seed)})
		if err != nil {
			return nil, "", err
		}
		if err := writeAtomic(path, payload, 0o600); err != nil {
			return nil, "", err
		}
	} else if err != nil {
		return nil, "", err
	}
	var stored storedKey
	if err := json.Unmarshal(payload, &stored); err != nil {
		return nil, "", fmt.Errorf("key file %s: %w", path, err)
	}
	seed, err := base64.RawURLEncoding.DecodeString(stored.Seed)
	if err != nil || len(seed) != ed25519.SeedSize || stored.Kid == "" {
		return nil, "", fmt.Errorf("key file %s is malformed", path)
	}
	return ed25519.NewKeyFromSeed(seed), stored.Kid, nil
}

/*
================
writeTokens

Rewrites every shard's token file, each atomically.
================
*/
func writeTokens(config tokenConfig) error {
	now := time.Now()
	for _, shard := range config.shards {
		token := mintToken(config.key, config.kid, config.issuer, config.namespace, shard, now)
		path := filepath.Join(config.outDir, "gameworld-"+shard+".jwt")
		if err := writeAtomic(path, []byte(token), 0o644); err != nil {
			return err
		}
	}
	return nil
}

/*
================
splitShards
================
*/
func splitShards(list string) []string {
	var shards []string
	for _, shard := range strings.Split(list, ",") {
		if shard = strings.TrimSpace(shard); shard != "" {
			shards = append(shards, shard)
		}
	}
	return shards
}

/*
================
serve

Runs the JWKS listener and the refresh loop until a signal arrives.
================
*/
func serve(ctx context.Context, listen string, config tokenConfig) error {
	if err := writeTokens(config); err != nil {
		return err
	}
	server := &http.Server{
		Addr:              listen,
		Handler:           jwksHandler(config.key.Public().(ed25519.PublicKey), config.kid),
		ReadHeaderTimeout: 5 * time.Second,
	}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	ticker := time.NewTicker(tokenRefresh)
	defer ticker.Stop()
	for {
		select {
		case err := <-failed:
			return err
		case <-ticker.C:
			if err := writeTokens(config); err != nil {
				return err
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		}
	}
}

/*
================
main
================
*/
func main() {
	keyPath := flag.String("key", "/identity/key.json", "Ed25519 key file, created when missing")
	listen := flag.String("listen", "127.0.0.1:8080", "JWKS listen address")
	issuer := flag.String("issuer", "http://127.0.0.1:8080", "token issuer; the Agent's SRO_NOMAD_IDENTITY_ISSUER")
	namespace := flag.String("namespace", "silkroad", "workload namespace; the Agent's SRO_NOMAD_NAMESPACE")
	outDir := flag.String("out-dir", "/out", "directory the per-shard tokens are written to")
	shards := flag.String("shards", "", "comma-separated shard ids")
	derive := flag.String("derive-public", "", "private Agent session key ring to project")
	out := flag.String("out", "", "public key ring output (with -derive-public)")
	flag.Parse()

	if *derive != "" {
		if *out == "" {
			log.Fatal("identity: -derive-public needs -out")
		}
		changed, err := derivePublic(*derive, *out)
		if err != nil {
			log.Fatalf("identity: %v", err)
		}
		log.Printf("identity: public key ring %s (changed=%v)", *out, changed)
		return
	}

	config := tokenConfig{issuer: *issuer, namespace: *namespace, outDir: *outDir, shards: splitShards(*shards)}
	if len(config.shards) == 0 {
		log.Fatal("identity: -shards names no shard")
	}
	var err error
	config.key, config.kid, err = loadOrCreateKey(*keyPath)
	if err != nil {
		log.Fatalf("identity: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("identity: serving %s/jwks.json for %s", *issuer, strings.Join(config.shards, ","))
	if err := serve(ctx, *listen, config); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("identity: %v", err)
	}
}
