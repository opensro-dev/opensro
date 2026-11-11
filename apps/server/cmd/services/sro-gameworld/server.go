// Command sro-gameworld runs one shard-owned GameWorld process.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	// Canary standby handover (opensro-ops#281, port-only, not native):
	// armed, construction probes the guard itself, because this process
	// may be the live one's standby and nothing may serialize ahead of
	// that probe. Unarmed, the guard is taken here first, exactly as
	// the scanner comment above requires.
	if standbyHandoverEnabled() {
		ts, err := gameWorldTransportServer(ownedShard)
		if err != nil {
			log.Fatalf("transport: %v", err)
		}
		runGameWorld(signalContext, ts, ownedShard, standbyBoot{armed: true, guardPath: guardPath})
		return
	}
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

	ts, err := gameWorldTransportServer(ownedShard)
	if err != nil {
		log.Fatalf("transport: %v", err)
	}
	runGameWorld(signalContext, ts, ownedShard, standbyBoot{})
}

/*
================
runGameWorld

Construct the application and run it to shutdown. An interrupt during
boot is an ordinary stop, not a failure (#3186): the cancelled open
released its claim, and the process exits 0 without waiting out the
kill timeout; the same holds for a standby stopped while it waited.
================
*/
func runGameWorld(signalContext context.Context, ts *transport.Server, ownedShard shard.Definition, boot standbyBoot) {
	application, err := newGameWorldApplication(signalContext, ts, ownedShard, boot)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Info("startup: interrupted during boot; exiting")
			return
		}
		log.Fatalf("startup: %v", err)
	}
	if err := application.Run(signalContext); err != nil {
		log.Errorf("shutdown: %v", err)
		os.Exit(1)
	}
}

// The canary standby handover's environment (opensro-ops#281).
const (
	// EnvStandbyHandover arms the standby mode ("on", "1", "true").
	// Unset is the native boot: the guard first, the listeners as today.
	EnvStandbyHandover = "SRO_STANDBY_HANDOVER"
	// EnvStandbyHealthAddr is the standby-health listener's bind
	// address, assigned by the deployment (its health port). Unset
	// binds a loopback dynamic port.
	EnvStandbyHealthAddr = "SRO_GAMEWORLD_HEALTH_ADDR"
)

/*
================
standbyHandoverEnabled

Port-only, not native (opensro-ops#281): whether this boot may stand
by for the live process instead of only ever replacing it. The job
names it; the code's own default is off.
================
*/
func standbyHandoverEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvStandbyHandover))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
standbyHealthAddr

Where the standby-health listener binds: the deployment's assigned
health port, or a loopback dynamic port where none was assigned.
================
*/
func standbyHealthAddr() string {
	if addr := strings.TrimSpace(os.Getenv(EnvStandbyHealthAddr)); addr != "" {
		return addr
	}
	return "127.0.0.1:0"
}

/*
================
gameWorldTransportServer

Build this process's transport server for the shard it owns: the viper
read, the catalog's transport defaults, the private-network and
public-WebTransport posture switches, and the capacity ceiling. One
assembly, called by main() and by the posture tests (wtpublic_test.go) -
a copy in a test would keep passing if this one lost a line.

A GameWorld process owns exactly one shard. Its catalog capacity is the
hard live-session ceiling; an environment override cannot quietly let
this worker advertise one capacity while admitting another.
================
*/
func gameWorldTransportServer(ownedShard shard.Definition) (*transport.Server, error) {
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
		return nil, err
	}
	transportConfig.PrivateNetwork =
		os.Getenv("SRO_GAMEWORLD_PRIVATE_NETWORK") == "1"
	transportConfig.WebTransportPublic = transport.WebTransportPublicEnabled()
	// Port-only, not native (#2671): the conduct cap on live sessions per
	// client address, off unless SRO_MAX_SESSIONS_PER_CLIENT arms it.
	transportConfig.MaxSessionsPerClient = transport.MaxSessionsPerClientLimit()
	if transportConfig.MaxSessionsPerClient > 0 {
		log.Infof("transport: at most %d live sessions per client address (%s, port-only, not native)",
			transportConfig.MaxSessionsPerClient, transport.EnvMaxSessionsPerClient)
	}
	// Port-only, not native: the closed beta's several characters of one
	// account in the world, one per tab; off is the native one seat.
	transportConfig.AccountMultiWorld = transport.AccountMultiWorldEnabled()
	if transportConfig.AccountMultiWorld {
		log.Infof("transport: several characters of one account may stand in the world (%s, port-only, not native)",
			transport.EnvAccountMultiWorld)
	}
	transportConfig.MaxSessions = ownedShard.Capacity
	warnTransportCatalogMismatch(ownedShard, transportConfig)
	return transport.NewServer(transportConfig)
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

Dev-only guard: the catalog transportUrl is the origin the web edge
proxies to; TRANSPORT_* is the bind address. The hosts diverge on
purpose - the catalog names the loopback origin while the process
binds a wildcard or a private address, and deployments may terminate
TLS in front - so only a port disagreement can silently advertise a
port nothing listens on (#2441). Log that, and only that, so
unsupervised local runs do not advertise the wrong port.
================
*/
func warnTransportCatalogMismatch(ownedShard shard.Definition, cfg transport.Config) {
	if !transportCatalogPortMismatch(ownedShard, cfg) {
		return
	}
	log.Warnf(
		"shard %q transport bind %s (wt) / %s (ws) differs in port from catalog transportUrl %q",
		ownedShard.ID,
		cfg.WTAddr,
		cfg.WSAddr,
		ownedShard.TransportURL,
	)
}

/*
================
transportCatalogPortMismatch

Whether either transport bind port disagrees with the catalog
transportUrl port (the scheme's default port when the URL names
none). Hosts are not compared: loopback, wildcard and private binds
behind the edge are the expected deployment shape.
================
*/
func transportCatalogPortMismatch(ownedShard shard.Definition, cfg transport.Config) bool {
	endpoint, err := url.Parse(ownedShard.TransportURL)
	if err != nil || endpoint.Host == "" {
		return false
	}
	catalogPort := endpoint.Port()
	if catalogPort == "" {
		catalogPort = "80"
		if endpoint.Scheme == "https" {
			catalogPort = "443"
		}
	}
	return transportBindPort(cfg.WTAddr) != catalogPort ||
		transportBindPort(cfg.WSAddr) != catalogPort
}

/*
================
transportBindPort

The port of a bind address, or "" when it carries none.
================
*/
func transportBindPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}
