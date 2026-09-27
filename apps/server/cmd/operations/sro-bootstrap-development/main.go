// Command sro-bootstrap-development prepares the explicit local development
// cluster contract. It creates missing random secrets, one bcrypt account
// matching the browser client, and an empty authority for each enabled shard.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"opensro.online/server/internal/cluster/provision"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/config"
)

const (
	accountIDKey = "SRO_DEV_ACCOUNT_ID"
	passwordKey  = "SRO_DEV_ACCOUNT_PASSWORD"
	shardIDKey   = "SRO_DEV_ACCOUNT_SHARD"
	maxEnvBytes  = 64 << 10
)

type developmentPaths struct {
	clusterStateDir string
	devAccount      string
	shardCatalog    string
	shardStateRoot  string
}

type developmentCredentials struct {
	accountID string
	password  []byte
	shardID   string
}

func main() {
	stateDirFlag := flag.String(
		"state-dir",
		"",
		"cluster credential state directory (default: <module>/.state/cluster)",
	)
	devAccountFlag := flag.String(
		"dev-account",
		"",
		"development account file (default: <module>/config/dev-account.env)",
	)
	shardCatalogFlag := flag.String(
		"shard-catalog",
		"",
		"shard catalog (default: <module>/config/shards.json)",
	)
	shardStateRootFlag := flag.String(
		"shard-state-root",
		"",
		"per-shard state root (default: <module>/.state/shards)",
	)
	flag.Parse()
	if flag.NArg() != 0 {
		fatal("positional arguments are not accepted")
	}

	paths, err := resolvePaths(
		*stateDirFlag,
		*devAccountFlag,
		*shardCatalogFlag,
		*shardStateRootFlag,
	)
	if err != nil {
		fatal("%v", err)
	}
	credentials, err := readDevelopmentCredentials(paths.devAccount)
	if err != nil {
		fatal("%s: %v", paths.devAccount, err)
	}
	defer erase(credentials.password)
	catalog, err := shard.Load(paths.shardCatalog)
	if err != nil {
		fatal("%s: %v", paths.shardCatalog, err)
	}
	definition, found := catalog.Resolve(credentials.shardID)
	if !found || !definition.Enabled {
		fatal(
			"%s=%q does not name an enabled shard in %s",
			shardIDKey,
			credentials.shardID,
			paths.shardCatalog,
		)
	}

	fmt.Fprintln(
		os.Stderr,
		"DEVELOPMENT ONLY: provisioning the browser's local shared login; do not use this account catalog in production.",
	)
	identityResult, err := clusterprovision.EnsureIdentity(paths.clusterStateDir)
	if err != nil {
		fatal("%v", err)
	}
	tokenResult, err := clusterprovision.EnsureProvisioningToken(paths.clusterStateDir)
	if err != nil {
		fatal("%v", err)
	}
	accountResult, err := clusterprovision.EnsureDevelopmentAccount(
		filepath.Join(paths.clusterStateDir, "accounts.json"),
		credentials.accountID,
		credentials.password,
	)
	if err != nil {
		fatal("%v", err)
	}
	shardResults, err := clusterprovision.EnsureDevelopmentShards(
		catalog,
		paths.shardStateRoot,
	)
	if err != nil {
		fatal("%v", err)
	}
	for _, result := range append(
		[]clusterprovision.FileResult{identityResult, tokenResult, accountResult},
		shardResults...,
	) {
		if result.Created {
			fmt.Printf("created %s\n", result.Path)
		} else {
			fmt.Printf("validated existing %s\n", result.Path)
		}
	}
	fmt.Printf(
		"development login %q on shard %q is ready\n",
		credentials.accountID,
		credentials.shardID,
	)
}

func resolvePaths(
	stateDirFlag string,
	devAccountFlag string,
	shardCatalogFlag string,
	shardStateRootFlag string,
) (developmentPaths, error) {
	if stateDirFlag != "" &&
		devAccountFlag != "" &&
		shardCatalogFlag != "" &&
		shardStateRootFlag != "" {
		return developmentPaths{
			clusterStateDir: cleanAbsolute(stateDirFlag),
			devAccount:      cleanAbsolute(devAccountFlag),
			shardCatalog:    cleanAbsolute(shardCatalogFlag),
			shardStateRoot:  cleanAbsolute(shardStateRootFlag),
		}, nil
	}
	moduleRoot, err := config.FindModuleRoot()
	if err != nil {
		return developmentPaths{}, fmt.Errorf(
			"locate source checkout: %w; pass all four path flags",
			err,
		)
	}
	stateDir := stateDirFlag
	if stateDir == "" {
		stateDir = filepath.Join(moduleRoot, ".state", "cluster")
	}
	devAccount := devAccountFlag
	if devAccount == "" {
		devAccount = filepath.Join(moduleRoot, "config", "dev-account.env")
	}
	shardCatalog := shardCatalogFlag
	if shardCatalog == "" {
		shardCatalog = filepath.Join(moduleRoot, shard.DefaultCatalogPath)
	}
	shardStateRoot := shardStateRootFlag
	if shardStateRoot == "" {
		shardStateRoot = filepath.Join(moduleRoot, ".state", "shards")
	}
	return developmentPaths{
		clusterStateDir: cleanAbsolute(stateDir),
		devAccount:      cleanAbsolute(devAccount),
		shardCatalog:    cleanAbsolute(shardCatalog),
		shardStateRoot:  cleanAbsolute(shardStateRoot),
	}, nil
}

func cleanAbsolute(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

func readDevelopmentCredentials(
	path string,
) (developmentCredentials, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return developmentCredentials{}, err
	}
	if !info.Mode().IsRegular() {
		return developmentCredentials{}, fmt.Errorf("not a regular file")
	}
	if info.Size() > maxEnvBytes {
		return developmentCredentials{}, fmt.Errorf(
			"file is %d bytes, limit is %d",
			info.Size(),
			maxEnvBytes,
		)
	}
	file, err := os.Open(path)
	if err != nil {
		return developmentCredentials{}, err
	}
	defer file.Close()

	values := make(map[string]string, 3)
	scanner := bufio.NewScanner(io.LimitReader(file, maxEnvBytes+1))
	scanner.Buffer(make([]byte, 4096), maxEnvBytes+1)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rawValue, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found ||
			(key != accountIDKey && key != passwordKey && key != shardIDKey) {
			continue
		}
		if _, duplicate := values[key]; duplicate {
			return developmentCredentials{}, fmt.Errorf(
				"line %d repeats %s",
				lineNumber,
				key,
			)
		}
		value, err := parseDotenvValue(rawValue)
		if err != nil {
			return developmentCredentials{}, fmt.Errorf(
				"line %d %s: %w",
				lineNumber,
				key,
				err,
			)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return developmentCredentials{}, err
	}
	accountID := values[accountIDKey]
	password := values[passwordKey]
	shardID := values[shardIDKey]
	if accountID == "" {
		return developmentCredentials{}, fmt.Errorf(
			"%s is missing or empty",
			accountIDKey,
		)
	}
	if password == "" {
		return developmentCredentials{}, fmt.Errorf(
			"%s is missing or empty",
			passwordKey,
		)
	}
	if shardID == "" {
		return developmentCredentials{}, fmt.Errorf(
			"%s is missing or empty",
			shardIDKey,
		)
	}
	return developmentCredentials{
		accountID: accountID,
		password:  []byte(password),
		shardID:   shardID,
	}, nil
}

func parseDotenvValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) < 2 {
		return value, nil
	}
	if value[0] == '"' {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted value: %w", err)
		}
		return decoded, nil
	}
	if value[0] == '\'' {
		if value[len(value)-1] != '\'' {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return value[1 : len(value)-1], nil
	}
	return value, nil
}

func erase(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func fatal(format string, arguments ...any) {
	var message bytes.Buffer
	fmt.Fprintf(&message, format, arguments...)
	fmt.Fprintf(os.Stderr, "sro-bootstrap-development: %s\n", message.String())
	os.Exit(1)
}
