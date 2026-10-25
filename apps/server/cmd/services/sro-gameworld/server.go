// Command sro-gameworld runs one shard-owned GameWorld process.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/config"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/platform/logging"
	"opensro.online/server/internal/platform/processguard"
	"opensro.online/server/internal/transport"
)

func main() {
	// Formatter first: config.Initialize logs, and its lines would
	// otherwise carry logrus' default shape instead of this server's.
	// Init runs after, once the configured level is readable.
	logging.InstallFormat()
	config.Initialize()
	logging.Init()

	// Nomad sends CTRL_BREAK_EVENT on Windows, which Go exposes as
	// os.Interrupt. SIGTERM covers Unix containers and service hosts.
	signalContext, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	ownedShard, err := loadOwnedShard()
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	// CyberCapture and similar launch scanners may transiently execute a
	// detached copy with the allocation's real environment. Serialize all local
	// copies before any shard lease, store, or listener side effect. Nomad's
	// tracked process waits and continues as soon as the scan copy exits.
	guardPath := filepath.Join(
		store.DirForShardFromEnv(ownedShard.ID),
		".gameworld-process.lock",
	)
	guard, err := processguard.Acquire(signalContext, guardPath)
	if err != nil {
		log.Fatalf("startup: shard %q process guard: %v", ownedShard.ID, err)
	}
	defer func() {
		if err := guard.Close(); err != nil {
			log.Warnf("shutdown: shard %q process guard: %v", ownedShard.ID, err)
		}
	}()
	log.Infof("startup: shard %q local process guard acquired", ownedShard.ID)

	// A GameWorld process owns exactly one shard. Its catalog capacity is the
	// hard live-session ceiling; an environment override cannot quietly let
	// this worker advertise one capacity while admitting another.
	transport.RegisterViperDefaults()
	transportConfig := transport.ConfigFromViper()
	if err := applyOwnedShardTransportDefaults(
		ownedShard,
		&transportConfig,
		os.Getenv("TRANSPORT_WT_ADDR") != "" ||
			viper.InConfig(transport.KeyWTAddr),
		os.Getenv("TRANSPORT_WS_ADDR") != "" ||
			viper.InConfig(transport.KeyWSAddr),
	); err != nil {
		log.Fatalf("transport: %v", err)
	}
	transportConfig.PrivateNetwork =
		os.Getenv("SRO_GAMEWORLD_PRIVATE_NETWORK") == "1"
	transportConfig.WebTransportPublic = transport.WebTransportPublicEnabled()
	transportConfig.MaxSessions = ownedShard.Capacity
	warnTransportCatalogMismatch(ownedShard, transportConfig)
	ts, err := transport.NewServer(transportConfig)
	if err != nil {
		log.Fatalf("transport: %v", err)
	}
	application, err := newGameWorldApplication(
		signalContext,
		ts,
		ownedShard,
	)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	if err := application.Run(signalContext); err != nil {
		log.Errorf("shutdown: %v", err)
		os.Exit(1)
	}
}

func applyOwnedShardTransportDefaults(
	ownedShard shard.Definition,
	config *transport.Config,
	wtExplicit bool,
	wsExplicit bool,
) error {
	endpoint, err := url.Parse(ownedShard.TransportURL)
	if err != nil || endpoint.Host == "" {
		return fmt.Errorf(
			"shard %q has invalid transport URL %q",
			ownedShard.ID,
			ownedShard.TransportURL,
		)
	}
	if !wtExplicit {
		config.WTAddr = endpoint.Host
	}
	if !wsExplicit {
		config.WSAddr = endpoint.Host
	}
	return nil
}

/*
================
warnTransportCatalogMismatch

Dev-only guard: the catalog transportUrl is the public origin clients
connect to; TRANSPORT_* is the bind address. They usually match on
loopback but can diverge behind TLS terminators. Log when they disagree
so unsupervised local runs do not silently advertise the wrong port.
================
*/
func warnTransportCatalogMismatch(ownedShard shard.Definition, cfg transport.Config) {
	endpoint, err := url.Parse(ownedShard.TransportURL)
	if err != nil || endpoint.Host == "" {
		return
	}

	catalogHost := endpoint.Host
	if cfg.WSAddr != catalogHost || cfg.WTAddr != catalogHost {
		log.Warnf(
			"shard %q transport bind %s (wt) / %s (ws) differs from catalog transportUrl host %q",
			ownedShard.ID,
			cfg.WTAddr,
			cfg.WSAddr,
			catalogHost,
		)
	}
}
