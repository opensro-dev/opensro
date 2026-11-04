package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// LogLevel is the logrus level key; logging.Init resolves it after
// Initialize has defaulted and bound it.
const LogLevel = "log.level"

// acceptedConfigExts is the PINNED config-file contract: the gateway reads
// config.yaml, config.yml, config.json or config.toml, nothing else. The
// order is the tie-break priority when one directory holds several.
//
// This list is deliberately explicit rather than viper.SupportedExts:
// viper 1.20 removed HCL, INI and Java-properties support, and a contract
// defined as "whatever viper parses this month" turns such removals into
// silently-ignored config files. Grow this list only as a deliberate,
// documented decision (the operator's deployment guide, section 2).
var acceptedConfigExts = []string{"yaml", "yml", "json", "toml"}

// unsupportedConfigExts are recognizable config filenames outside the
// gateway contract. Their presence is a startup error so an operator cannot
// mistake an ignored file for active configuration.
var unsupportedConfigExts = []string{"hcl", "ini", "properties", "props", "prop", "env", "dotenv"}

// Initialize wires viper for the gateway: automatic env, pinned config-file
// discovery and the log-level default. Any config-file problem other than
// "there is no config file" is fatal: a present-but-unreadable file must
// stop the boot, never fall back to defaults an operator did not choose.
func Initialize() {
	if err := initializeIn(viper.GetViper(), searchPaths()); err != nil {
		logrus.Fatalf("config: %v", err)
	}
}

// searchPaths returns the config-file search directories in priority order.
// Same set the server has always used; $HOME resolves via os.UserHomeDir so
// it works on Windows (USERPROFILE) as well as Unix.
func searchPaths() []string {
	paths := []string{"/etc/go-sro-gateway-server"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, home)
	}
	return append(paths, ".")
}

// initializeIn is Initialize with the viper instance and search paths
// injected so tests can drive it against temp directories without touching
// process-global state or os.Exit.
func initializeIn(v *viper.Viper, paths []string) error {
	v.AutomaticEnv()
	v.SetDefault(LogLevel, "info")
	if err := v.BindEnv(LogLevel, "LOG_LEVEL"); err != nil {
		return fmt.Errorf("binding %s environment override: %w", LogLevel, err)
	}

	file, err := discoverConfigFile(paths)
	if err != nil {
		return err
	}

	// No config file at all stays benign: env vars plus defaults are the
	// containerized and Nomad deployment paths and carry the
	// whole documented contract. Only a file that exists but cannot be
	// used is a boot-stopper.
	if file == "" {
		logrus.Info("no config file found. using environment config or default values")
	} else {
		v.SetConfigFile(file)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("config file %s exists but cannot be parsed: %v - fix or delete it; the server refuses to run on defaults an operator did not choose", file, err)
		}
		logrus.Infof("loaded config file %s", file)
	}

	logrus.Info("gateway config initialized")
	return nil
}

// discoverConfigFile scans the search paths for config.<ext>. It returns
// the highest-priority accepted file ("" when none exists) and an error
// when ANY unsupported config file is present - even one shadowed by an
// accepted file, because a stray config.ini that nothing reads is exactly
// the invisible misconfiguration this discovery exists to catch.
func discoverConfigFile(paths []string) (string, error) {
	var chosen string
	var unsupported []string
	for _, dir := range paths {
		for _, ext := range acceptedConfigExts {
			p := filepath.Join(dir, "config."+ext)
			if isRegularFile(p) && chosen == "" {
				chosen = p
			}
		}
		for _, ext := range unsupportedConfigExts {
			p := filepath.Join(dir, "config."+ext)
			if isRegularFile(p) {
				unsupported = append(unsupported, absOrSelf(p))
			}
		}
	}
	if len(unsupported) > 0 {
		return "", fmt.Errorf(
			"unsupported config file(s): %s; the gateway accepts only config.{yaml|yml|json|toml} or environment variables",
			strings.Join(unsupported, ", "))
	}
	return chosen, nil
}

// isRegularFile reports whether path exists and is a regular file.
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// absOrSelf resolves path for operator-facing messages; a file found via
// the "." search path must be named absolutely, not as bare "config.ini".
func absOrSelf(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
