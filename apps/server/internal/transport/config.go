package transport

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Viper keys for the transport, with env aliases in the same style as
// config/gateway.go.
const (
	KeyEnabled         = "transport.enabled"
	KeyWTAddr          = "transport.wt_addr"
	KeyWSAddr          = "transport.ws_addr"
	KeyCertDir         = "transport.cert_dir"
	KeyCertFile        = "transport.cert_file"
	KeyKeyFile         = "transport.key_file"
	KeyAllowedOrigins  = "transport.allowed_origins"
	KeyOutboundQueue   = "transport.outbound_queue"
	KeyOutboundBytes   = "transport.outbound_queue_bytes"
	KeyRateLimitPerSec = "transport.rate_limit_per_sec"
	KeyRateLimitBurst  = "transport.rate_limit_burst"
	KeyMaxSessions     = "transport.max_sessions"
	KeyMaxAccountSess  = "transport.max_sessions_per_account"
	KeyMaxHandshakes   = "transport.max_pending_handshakes"
	KeyPprof           = "transport.pprof"
)

// Config tunes the transport server. Zero fields take the defaults below.
type Config struct {
	// WTAddr is the UDP host:port for WebTransport (HTTP/3). Never :8787 —
	// the global Agent owns that.
	WTAddr string
	// WSAddr is the TCP host:port for the WebSocket alternate and the
	// cert-hash/health endpoints.
	WSAddr string
	// CertDir holds the self-managed development certificate. It is used
	// only when CertFile and KeyFile are both empty.
	CertDir string
	// CertFile and KeyFile are the explicit production TLS pair. They must
	// be configured together; the server loads them read-only and never
	// replaces or renews them.
	CertFile string
	KeyFile  string
	// AllowedOrigins whitelists the Origin header on WS upgrades and WT
	// CONNECTs. Empty does NOT mean allow-any: newOriginChecker then
	// accepts only LOOPBACK Origins (localhost, 127.0.0.1, ::1) — a
	// zero-config dev posture that fails closed. Any real deployment must
	// set the list; requests with no Origin header pass either way.
	AllowedOrigins []string
	// PrivateNetwork acknowledges that a non-loopback plaintext WS/HTTP
	// listener is reachable only by a private TLS edge. It never permits
	// pprof on that listener and does not relax Origin or identity checks.
	PrivateNetwork bool

	// HelloTimeout is how long a fresh connection may sit without HELLO.
	HelloTimeout time.Duration
	// GracePeriod is how long a detached session waits for a resume.
	GracePeriod time.Duration
	// KeepaliveInterval is how often a quiet attached client is pinged.
	KeepaliveInterval time.Duration
	// IdleTimeout detaches a client that answered nothing at all.
	IdleTimeout time.Duration
	// OutboundQueue and OutboundQueueBytes bound pending reliable and
	// coalesced frames per session. Keeping both limits prevents tiny-frame
	// floods and a few catalogue-sized frames from consuming unbounded memory.
	// The frame limit must also accommodate one normal enter-world burst;
	// admission enqueues that burst atomically so its success cannot depend on
	// whether the socket writer happened to run between individual Sends.
	OutboundQueue      int
	OutboundQueueBytes int

	// RateLimitPerSec is the per-session inbound dispatch budget in
	// frames/second (token-bucket refill), sized from the legit-traffic
	// inventory in ratelimit.go. 0 takes the default; negative is invalid.
	RateLimitPerSec int
	// RateLimitBurst is the bucket capacity: how many frames may arrive
	// back-to-back before the sustained rate applies. 0 takes the default;
	// negative is invalid.
	RateLimitBurst int

	// MaxSessions caps attached plus resume-grace sessions. A detached
	// session still owns state and goroutines, so it counts until final
	// teardown. 0 takes the default.
	MaxSessions int
	// MaxSessionsPerAccount caps the live sessions one account holds. Every
	// session costs an admission ticket the account can mint at will, so
	// without it one account could fill MaxSessions and lock every other
	// player out. A new session evicts the account's oldest instead of being
	// refused, so a player reloading faster than resume grace expires is
	// never locked out. 0 takes the default.
	MaxSessionsPerAccount int
	// MaxPendingHandshakes caps connections concurrently waiting for HELLO.
	// Excess connections are rejected immediately instead of consuming a
	// goroutine for the full HelloTimeout. 0 takes the default.
	MaxPendingHandshakes int

	// EnablePprof registers net/http/pprof on the transport's TCP mux
	// (/debug/pprof/). OFF by default and opt-in only (transport.pprof /
	// TRANSPORT_PPROF=1). The TCP listener is loopback-only, so profiling
	// data never rides a public socket.
	EnablePprof bool
}

// DefaultMaxSessionsPerAccount allows a few browser tabs plus the sessions
// a reload leaves in resume grace; a player needs one.
const DefaultMaxSessionsPerAccount = 4

// DefaultConfig is the library/test baseline. The GameWorld composition
// replaces unconfigured listener addresses with its owned shard's catalog
// transport URL before constructing the server.
func DefaultConfig() Config {
	return Config{
		WTAddr:                "127.0.0.1:8788",
		WSAddr:                "127.0.0.1:8788",
		CertDir:               filepath.Join(".state", "cluster", "dev-certs"),
		HelloTimeout:          10 * time.Second,
		GracePeriod:           30 * time.Second,
		KeepaliveInterval:     20 * time.Second,
		IdleTimeout:           60 * time.Second,
		OutboundQueue:         8192,
		OutboundQueueBytes:    32 << 20,
		RateLimitPerSec:       25,
		RateLimitBurst:        75,
		MaxSessions:           5000,
		MaxSessionsPerAccount: DefaultMaxSessionsPerAccount,
		MaxPendingHandshakes:  256,
	}
}

func (c *Config) applyDefaults() {
	d := DefaultConfig()
	if c.WTAddr == "" {
		c.WTAddr = d.WTAddr
	}
	if c.WSAddr == "" {
		c.WSAddr = d.WSAddr
	}
	if c.CertDir == "" {
		c.CertDir = d.CertDir
	}
	if c.HelloTimeout == 0 {
		c.HelloTimeout = d.HelloTimeout
	}
	if c.GracePeriod == 0 {
		c.GracePeriod = d.GracePeriod
	}
	if c.KeepaliveInterval == 0 {
		c.KeepaliveInterval = d.KeepaliveInterval
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = d.IdleTimeout
	}
	if c.OutboundQueue == 0 {
		c.OutboundQueue = d.OutboundQueue
	}
	if c.OutboundQueueBytes == 0 {
		c.OutboundQueueBytes = d.OutboundQueueBytes
	}
	if c.RateLimitPerSec == 0 {
		c.RateLimitPerSec = d.RateLimitPerSec
	}
	if c.RateLimitBurst == 0 {
		c.RateLimitBurst = d.RateLimitBurst
	}
	if c.MaxSessions == 0 {
		c.MaxSessions = d.MaxSessions
	}
	if c.MaxSessionsPerAccount == 0 {
		c.MaxSessionsPerAccount = d.MaxSessionsPerAccount
	}
	if c.MaxPendingHandshakes == 0 {
		c.MaxPendingHandshakes = d.MaxPendingHandshakes
	}
}

// RegisterViperDefaults wires the transport keys and env aliases into viper.
// Idempotent; NewServerFromViper calls it.
func RegisterViperDefaults() {
	d := DefaultConfig()
	viper.SetDefault(KeyEnabled, true)
	viper.SetDefault(KeyWTAddr, d.WTAddr)
	viper.SetDefault(KeyWSAddr, d.WSAddr)
	viper.SetDefault(KeyCertDir, d.CertDir)
	viper.SetDefault(KeyCertFile, "")
	viper.SetDefault(KeyKeyFile, "")
	viper.SetDefault(KeyAllowedOrigins, []string{})
	viper.SetDefault(KeyOutboundQueue, d.OutboundQueue)
	viper.SetDefault(KeyOutboundBytes, d.OutboundQueueBytes)
	viper.SetDefault(KeyRateLimitPerSec, d.RateLimitPerSec)
	viper.SetDefault(KeyRateLimitBurst, d.RateLimitBurst)
	viper.SetDefault(KeyMaxSessions, d.MaxSessions)
	viper.SetDefault(KeyMaxAccountSess, d.MaxSessionsPerAccount)
	viper.SetDefault(KeyMaxHandshakes, d.MaxPendingHandshakes)
	viper.SetDefault(KeyPprof, false)

	_ = viper.BindEnv(KeyEnabled, "TRANSPORT_ENABLED")
	_ = viper.BindEnv(KeyWTAddr, "TRANSPORT_WT_ADDR")
	_ = viper.BindEnv(KeyWSAddr, "TRANSPORT_WS_ADDR")
	_ = viper.BindEnv(KeyCertDir, "TRANSPORT_CERT_DIR")
	_ = viper.BindEnv(KeyCertFile, "TRANSPORT_CERT_FILE")
	_ = viper.BindEnv(KeyKeyFile, "TRANSPORT_KEY_FILE")
	_ = viper.BindEnv(KeyAllowedOrigins, "TRANSPORT_ALLOWED_ORIGINS")
	_ = viper.BindEnv(KeyOutboundQueue, "TRANSPORT_OUTBOUND_QUEUE")
	_ = viper.BindEnv(KeyOutboundBytes, "TRANSPORT_OUTBOUND_QUEUE_BYTES")
	_ = viper.BindEnv(KeyRateLimitPerSec, "TRANSPORT_RATE_LIMIT_PER_SEC")
	_ = viper.BindEnv(KeyRateLimitBurst, "TRANSPORT_RATE_LIMIT_BURST")
	_ = viper.BindEnv(KeyMaxSessions, "TRANSPORT_MAX_SESSIONS")
	_ = viper.BindEnv(KeyMaxAccountSess, "TRANSPORT_MAX_SESSIONS_PER_ACCOUNT")
	_ = viper.BindEnv(KeyMaxHandshakes, "TRANSPORT_MAX_PENDING_HANDSHAKES")
	_ = viper.BindEnv(KeyPprof, "TRANSPORT_PPROF")
}

// ConfigFromViper reads the transport config after RegisterViperDefaults.
func ConfigFromViper() Config {
	cfg := DefaultConfig()
	cfg.WTAddr = viper.GetString(KeyWTAddr)
	cfg.WSAddr = viper.GetString(KeyWSAddr)
	cfg.CertDir = viper.GetString(KeyCertDir)
	cfg.CertFile = viper.GetString(KeyCertFile)
	cfg.KeyFile = viper.GetString(KeyKeyFile)
	cfg.AllowedOrigins = configuredStringList(
		viper.Get(KeyAllowedOrigins),
	)
	cfg.OutboundQueue = viper.GetInt(KeyOutboundQueue)
	cfg.OutboundQueueBytes = viper.GetInt(KeyOutboundBytes)
	cfg.RateLimitPerSec = viper.GetInt(KeyRateLimitPerSec)
	cfg.RateLimitBurst = viper.GetInt(KeyRateLimitBurst)
	cfg.MaxSessions = viper.GetInt(KeyMaxSessions)
	cfg.MaxSessionsPerAccount = viper.GetInt(KeyMaxAccountSess)
	cfg.MaxPendingHandshakes = viper.GetInt(KeyMaxHandshakes)
	cfg.EnablePprof = viper.GetBool(KeyPprof)
	cfg.applyDefaults()
	return cfg
}

// configuredStringList preserves native string slices from config files and
// explicitly splits comma-separated environment values. Viper's
// GetStringSlice does not split a bound environment string on commas, which
// would turn several allowed origins into one impossible origin.
func configuredStringList(value any) []string {
	var raw []string
	switch typed := value.(type) {
	case string:
		raw = strings.Split(typed, ",")
	case []string:
		raw = typed
	case []any:
		raw = make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				raw = append(raw, text)
			}
		}
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
