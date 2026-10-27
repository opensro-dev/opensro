package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nomad "github.com/hashicorp/nomad/api"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/transport"
)

func TestDurableGMAllowlistSurvivesDefaultRedeployAndSupportsRevocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gm-characters.txt")
	for _, value := range []string{"global-official:asd2,global-official:[GM]Test2", ""} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := configuredGMCharacters("", "loopback", dir)
		if err != nil || got != value {
			t.Fatalf("durable value %q: %q %v", value, got, err)
		}
	}
	got, err := configuredGMCharacters("global-official:Operator", "loopback", dir)
	if err != nil || got != "global-official:Operator" {
		t.Fatalf("explicit override: %q %v", got, err)
	}
	if err := os.WriteFile(path, []byte("malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := configuredGMCharacters("", "loopback", dir); err == nil {
		t.Fatal("invalid config silently fell back")
	}
}

func TestRenderGameWorldTemplatePinsJobAndVariableIdentity(t *testing.T) {
	template := []byte(
		`job "sro-gameworld-__SHARD_ID__" {}` +
			`nomad/jobs/sro-gameworld-__SHARD_ID__/gameworld/gameworld`,
	)
	rendered := string(renderGameWorldTemplate(template, "global-official"))
	if strings.Contains(rendered, gameWorldShardToken) {
		t.Fatalf("rendered template retains shard token: %s", rendered)
	}
	for _, want := range []string{
		`job "sro-gameworld-global-official"`,
		"nomad/jobs/sro-gameworld-global-official/gameworld/gameworld",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered template lacks %q: %s", want, rendered)
		}
	}
}

func TestRenderAgentTemplatePinsOrderedAccountChunks(t *testing.T) {
	template := []byte("before\n" + agentAccountToken + "\nafter\n")
	rendered, err := renderAgentTemplate(template, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := string(rendered)
	if strings.Contains(got, agentAccountToken) {
		t.Fatalf("rendered template retains token: %s", got)
	}
	first := strings.Index(got, accountChunkVariablePath(0))
	second := strings.Index(got, accountChunkVariablePath(1))
	if first < 0 || second <= first {
		t.Fatalf("account chunks are not rendered in order: %s", got)
	}
}

func TestChunkAccountCatalogReassemblesUTF8WithinVariableBudget(t *testing.T) {
	payload := bytes.Repeat([]byte("账户-a"), accountChunkBytes/4)
	chunks, err := chunkAccountCatalog(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("chunk count = %d, want several", len(chunks))
	}
	var rebuilt strings.Builder
	for _, chunk := range chunks {
		if len(chunk) > accountChunkBytes {
			t.Fatalf(
				"chunk size = %d, limit = %d",
				len(chunk),
				accountChunkBytes,
			)
		}
		rebuilt.WriteString(chunk)
	}
	if rebuilt.String() != string(payload) {
		t.Fatal("account chunks did not reassemble exactly")
	}
}

func TestChunkAccountCatalogSupportsApplicationLimit(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 1<<20)
	chunks, err := chunkAccountCatalog(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 22 {
		t.Fatalf("chunk count = %d, want 22 for one MiB", len(chunks))
	}
	if rebuilt := strings.Join(chunks, ""); rebuilt != string(payload) {
		t.Fatal("one-MiB account catalog did not reassemble exactly")
	}
}

func TestChunkAccountCatalogRejectsInvalidUTF8(t *testing.T) {
	if _, err := chunkAccountCatalog([]byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 account catalog was accepted")
	}
}

func TestNormalizeHostNetworkRequiresPrivateAcknowledgment(t *testing.T) {
	for _, test := range []struct {
		name    string
		private bool
		want    string
		ok      bool
	}{
		{" loopback ", false, "loopback", true},
		{"LOOPBACK", false, "loopback", true},
		{" game-private ", true, "game-private", true},
		{"game-private", false, "", false},
		{" ", true, "", false},
	} {
		got, err := normalizeHostNetwork(test.name, test.private)
		if test.ok && (err != nil || got != test.want) {
			t.Errorf(
				"normalizeHostNetwork(%q, %t) = %q, %v; want %q",
				test.name,
				test.private,
				got,
				err,
				test.want,
			)
		}
		if !test.ok && err == nil {
			t.Errorf(
				"normalizeHostNetwork(%q, %t) = %q, want error",
				test.name,
				test.private,
				got,
			)
		}
	}
}

func TestNormalizeAllowedOriginsDefaultsOnlyOnLoopback(t *testing.T) {
	got, err := normalizeAllowedOrigins("", "loopback")
	if err != nil {
		t.Fatal(err)
	}
	if got != developmentAllowedOrigins {
		t.Fatalf(
			"loopback origins = %q, want %q",
			got,
			developmentAllowedOrigins,
		)
	}
	if _, err := normalizeAllowedOrigins("", "game-private"); err == nil {
		t.Fatal("production host network accepted no browser origins")
	}
}

func TestNormalizeGMCharactersDefaultsOnlyOnLoopback(t *testing.T) {
	got, err := normalizeGMCharacters("", "loopback")
	if err != nil {
		t.Fatal(err)
	}
	if got != developmentGMCharacters {
		t.Fatalf(
			"loopback GM allowlist = %q, want %q",
			got,
			developmentGMCharacters,
		)
	}
	got, err = normalizeGMCharacters("", "game-private")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("production GM allowlist = %q, want empty", got)
	}
}

func TestNormalizeGMCharactersHonorsExplicitOperatorAllowlist(t *testing.T) {
	const allowlist = "global-official:Operator,test:Probe"
	for _, network := range []string{"loopback", "game-private"} {
		got, err := normalizeGMCharacters(allowlist, network)
		if err != nil {
			t.Fatal(err)
		}
		if got != allowlist {
			t.Fatalf("%s GM allowlist = %q, want %q", network, got, allowlist)
		}
	}
}

func TestNormalizeAllowedOriginsCanonicalizesExactOrigins(t *testing.T) {
	got, err := normalizeAllowedOrigins(
		" HTTPS://PLAY.EXAMPLE.COM/, http://localhost:5174,"+
			"https://play.example.com ",
		"game-private",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "http://localhost:5174,https://play.example.com"
	if got != want {
		t.Fatalf("origins = %q, want %q", got, want)
	}
}

func TestNormalizeAllowedOriginsRejectsUnsafeOrMalformedValues(t *testing.T) {
	for _, raw := range []string{
		"https://play.example.com,,https://admin.example.com",
		"*",
		"https://*.example.com",
		"file://play.example.com",
		"https://user@play.example.com",
		"https://play.example.com/game",
		"https://play.example.com?tenant=test",
		"https://play.example.com#fragment",
		"https://:443",
		"https://play.example.com:0",
		"https://play.example.com:65536",
	} {
		if _, err := normalizeAllowedOrigins(
			raw,
			"game-private",
		); err == nil {
			t.Errorf("unsafe allowed-origin value %q was accepted", raw)
		}
	}
}

func TestNomadJobVariablesAlwaysCarryAllowedOrigins(t *testing.T) {
	const origins = "https://play.example.com"
	deployment := &deployment{
		AllowedOrigins: origins,
	}
	if got := deployment.agentVariables()["allowed_origins"]; got != origins {
		t.Fatalf("Agent allowed_origins = %#v, want %q", got, origins)
	}
	if got := deployment.gameVariables(
		shardDeployment{},
	)["allowed_origins"]; got != origins {
		t.Fatalf("GameWorld allowed_origins = %#v, want %q", got, origins)
	}
}

func TestNomadGameWorldVariablesCarryGMAllowlist(t *testing.T) {
	const allowlist = "global-official:asd2"
	deployment := &deployment{GMCharacters: allowlist}
	if got := deployment.gameVariables(shardDeployment{})["gm_characters"]; got != allowlist {
		t.Fatalf("GameWorld gm_characters = %#v, want %q", got, allowlist)
	}
}

func TestRawExecBoundariesDenyInheritedControlPlaneCredentials(t *testing.T) {
	moduleRoot := filepath.Clean("../../..")
	paths := []string{
		filepath.Join(moduleRoot, "ops", "nomad", "jobs", agentTemplateName),
		filepath.Join(moduleRoot, "ops", "nomad", "jobs", gameTemplateName),
		filepath.Join(
			moduleRoot,
			"ops",
			"nomad",
			"config",
			"client-windows-agent-production.hcl.example",
		),
		filepath.Join(
			moduleRoot,
			"ops",
			"nomad",
			"config",
			"client-windows-production.hcl.example",
		),
	}
	for _, path := range paths {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(payload)
		for _, required := range []string{
			"denied_envvars",
			`"NOMAD_TOKEN"`,
			`"CONSUL_*"`,
			`"VAULT_*"`,
			`"AWS_*"`,
			`"AZURE_*"`,
			`"ARM_*"`,
			`"GOOGLE_*"`,
		} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s does not deny %s", path, required)
			}
		}
	}
}

func TestResolveTransportCertificateFailsClosedOutsideLoopback(t *testing.T) {
	if _, _, _, _, err := resolveTransportCertificate(
		"",
		"",
		"game-private",
		true,
		nil,
	); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("missing production TLS pair error = %v", err)
	}
	if _, _, _, _, err := resolveTransportCertificate(
		"cert.pem",
		"",
		"loopback",
		false,
		nil,
	); err == nil || !strings.Contains(err.Error(), "configured together") {
		t.Fatalf("partial TLS pair error = %v", err)
	}
}

func TestResolveTransportCertificatePinsIdentityAndAdvertisedHosts(
	t *testing.T,
) {
	generated, err := transport.LoadCertificate("", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	game := shardDeployment{
		Definition: shard.Definition{
			ID:           "global-official",
			TransportURL: "https://localhost:8788",
		},
	}
	certFile, keyFile, identity, certificate, err := resolveTransportCertificate(
		generated.CertPath,
		generated.KeyPath,
		"game-private",
		true,
		[]shardDeployment{game},
	)
	if err != nil {
		t.Fatal(err)
	}
	if certFile != slashPath(cleanAbsolute(generated.CertPath)) ||
		keyFile != slashPath(cleanAbsolute(generated.KeyPath)) {
		t.Fatalf("resolved TLS pair = %q, %q", certFile, keyFile)
	}
	if identity != generated.SHA256Hex() {
		t.Fatalf("TLS identity = %q, want %q", identity, generated.SHA256Hex())
	}
	if certificate == nil || certificate.SHA256Hex() != identity {
		t.Fatalf("loaded certificate does not match the pinned identity")
	}

	game.Definition.TransportURL = "https://play.example.com:8788"
	if _, _, _, _, err := resolveTransportCertificate(
		generated.CertPath,
		generated.KeyPath,
		"game-private",
		true,
		[]shardDeployment{game},
	); err == nil || !strings.Contains(err.Error(), "does not cover") {
		t.Fatalf("wrong-host certificate error = %v", err)
	}

	game.Definition.TransportURL = "http://localhost:8788"
	if _, _, _, _, err := resolveTransportCertificate(
		generated.CertPath,
		generated.KeyPath,
		"game-private",
		true,
		[]shardDeployment{game},
	); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("plaintext production transport error = %v", err)
	}
}

func TestNomadGameWorldVariablesCarryTransportTLSIdentity(t *testing.T) {
	deployment := &deployment{
		TransportCert:   "C:/sro/tls/fullchain.pem",
		TransportKey:    "C:/sro/tls/private-key.pem",
		TransportTLSID:  "leaf-sha256",
		TransportWTAddr: "203.0.113.7:443",
	}
	variables := deployment.gameVariables(shardDeployment{})
	for key, want := range map[string]string{
		"transport_cert_file": "C:/sro/tls/fullchain.pem",
		"transport_key_file":  "C:/sro/tls/private-key.pem",
		"transport_tls_id":    "leaf-sha256",
		"transport_wt_addr":   "203.0.113.7:443",
	} {
		if got := variables[key]; got != want {
			t.Errorf("%s = %#v, want %q", key, got, want)
		}
	}
	blank := *deployment
	blank.TransportWTAddr = ""
	if got := blank.gameVariables(shardDeployment{})["transport_wt_addr"]; got != "" {
		t.Errorf("undeclared public bind rendered = %#v, want empty", got)
	}
}

func TestValidateWebTransportPostureRefusesHalfPostures(t *testing.T) {
	certificate, err := transport.LoadCertificate("", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	game := shardDeployment{
		Definition: shard.Definition{
			ID:           "global-official",
			TransportURL: "https://localhost/transport/ws",
		},
	}
	on := func(wtAddr string, publicPort int) webTransportPosture {
		return webTransportPosture{Public: true, WTAddr: wtAddr, PublicPort: publicPort}
	}
	other := func(url string) []shardDeployment {
		return []shardDeployment{{Definition: shard.Definition{ID: "global-official", TransportURL: url}}}
	}
	for name, tt := range map[string]struct {
		certificate *transport.Certificate
		posture     webTransportPosture
		shards      []shardDeployment
		wantErr     string
	}{
		"off without a bind":              {nil, webTransportPosture{}, []shardDeployment{game}, ""},
		"off with a bind":                 {nil, webTransportPosture{WTAddr: "203.0.113.7:443"}, []shardDeployment{game}, "SRO_WEBTRANSPORT_PUBLIC is off"},
		"off with a public port":          {nil, webTransportPosture{PublicPort: 443}, []shardDeployment{game}, "-transport-wt-public-port 443 names a public port"},
		"on without a pair":               {nil, on("203.0.113.7:443", 0), []shardDeployment{game}, "no production TLS pair"},
		"on without a bind":               {certificate, on("", 0), []shardDeployment{game}, "-transport-wt-addr is required"},
		"on with a malformed bind":        {certificate, on("garbage", 0), []shardDeployment{game}, "must be a host:port"},
		"on with an anonymous bind":       {certificate, on(":443", 0), []shardDeployment{game}, "must be a host:port"},
		"on with an impossible port":      {certificate, on("203.0.113.7:99999", 0), []shardDeployment{game}, "invalid port"},
		"on with an impossible public":    {certificate, on("203.0.113.7:18443", 70000), []shardDeployment{game}, "-transport-wt-public-port 70000 is not a port"},
		"on with a matching posture":      {certificate, on("203.0.113.7:443", 0), []shardDeployment{game}, ""},
		"redirected public port matches":  {certificate, on("[::]:18443", 443), []shardDeployment{game}, ""},
		"redirected to the IPv4 wildcard": {certificate, on("0.0.0.0:18443", 443), []shardDeployment{game}, ""},
		"redirected to one address":       {certificate, on("203.0.113.7:18443", 443), []shardDeployment{game}, "must bind an unspecified host"},
		"redirected to loopback":          {certificate, on("127.0.0.1:18443", 443), []shardDeployment{game}, "must bind an unspecified host"},
		"redirected to a hostname":        {certificate, on("localhost:18443", 443), []shardDeployment{game}, "must bind an unspecified host"},
		"no redirect keeps any host":      {certificate, on("203.0.113.7:443", 443), []shardDeployment{game}, ""},
		"advertised port must match":      {certificate, on("203.0.113.7:8443", 0), []shardDeployment{game}, "advertises WebTransport on port 443, but clients reach port 8443"},
		"redirect must reach advertised":  {certificate, on("[::]:18443", 8443), []shardDeployment{game}, "clients reach port 8443"},
		"high bind needs the redirect":    {certificate, on("203.0.113.7:18443", 0), []shardDeployment{game}, "clients reach port 18443"},
		"advertised origin base":          {certificate, on("203.0.113.7:443", 0), other("https://localhost"), ""},
		"advertised origin base slash":    {certificate, on("203.0.113.7:443", 0), other("https://localhost/"), ""},
		"advertised route base":           {certificate, on("203.0.113.7:443", 0), other("https://localhost/shards/global-official"), ""},
		"socket form still tolerated":     {certificate, on("203.0.113.7:443", 0), other("https://localhost/transport/ws"), ""},
		"advertised WT path refused":      {certificate, on("203.0.113.7:443", 0), other("https://localhost/transport/wt"), "must be the transport base"},
		"advertised references refused":   {certificate, on("203.0.113.7:443", 0), other("https://localhost/transport/references"), "must be the transport base"},
		"advertised bare transport":       {certificate, on("203.0.113.7:443", 0), other("https://localhost/transport"), "must be the transport base"},
		"advertised URL must be https":    {certificate, on("203.0.113.7:443", 0), other("http://localhost/transport/ws"), "absolute HTTPS URL"},
		"certificate must cover host":     {certificate, on("203.0.113.7:443", 0), other("https://play.example.com/transport/ws"), "does not cover"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateWebTransportPosture(tt.certificate, tt.posture, tt.shards)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("posture error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("posture error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestConfiguredNomadNamespaceIsExplicitAndConservative(t *testing.T) {
	t.Setenv("NOMAD_NAMESPACE", "")
	got, err := configuredNomadNamespace("")
	if err != nil || got != defaultNomadNamespace {
		t.Fatalf("default namespace = %q, %v", got, err)
	}
	t.Setenv("NOMAD_NAMESPACE", "sro")
	got, err = configuredNomadNamespace("")
	if err != nil || got != "sro" {
		t.Fatalf("environment namespace = %q, %v", got, err)
	}
	got, err = configuredNomadNamespace("sro-staging")
	if err != nil || got != "sro-staging" {
		t.Fatalf("explicit namespace = %q, %v", got, err)
	}
	for _, invalid := range []string{"SRO", "sro_world", "sro/world"} {
		if _, err := configuredNomadNamespace(invalid); err == nil {
			t.Errorf("invalid namespace %q was accepted", invalid)
		}
	}
}

func TestProductionNetworkRefusesDefaultNomadNamespace(t *testing.T) {
	options := commandOptions{
		ModuleRoot: filepath.Clean("../../.."),
		Namespace:  defaultNomadNamespace,
		Network:    "game-private",
		AgentPort:  8787,
		PrivateNet: true,
	}
	if _, err := resolveDeployment(options, false); err == nil ||
		!strings.Contains(err.Error(), "dedicated namespace") {
		t.Fatalf("production default namespace error = %v", err)
	}
}

func TestNonDeploymentResolutionDoesNotRequireRuntimeTLSOrIdentity(
	t *testing.T,
) {
	options := commandOptions{
		ModuleRoot: filepath.Clean("../../.."),
		Namespace:  "sro",
		Network:    "game-private",
		AgentPort:  8787,
		PrivateNet: true,
	}
	deployment, err := resolveDeployment(options, false)
	if err != nil {
		t.Fatal(err)
	}
	if deployment.TransportCert != "" ||
		deployment.TransportKey != "" ||
		deployment.IdentityIssuer != "" {
		t.Fatalf(
			"non-deployment resolution leaked runtime prerequisites: cert=%q key=%q issuer=%q",
			deployment.TransportCert,
			deployment.TransportKey,
			deployment.IdentityIssuer,
		)
	}
}

func TestEndpointPortRequiresExplicitValidPort(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want int
		ok   bool
	}{
		{"http://127.0.0.1:8788", 8788, true},
		{"https://[::1]:8793", 8793, true},
		{"https://example.invalid", 0, false},
		{"https://example.invalid:70000", 0, false},
	} {
		got, err := endpointPort(test.raw)
		if test.ok && (err != nil || got != test.want) {
			t.Errorf("endpointPort(%q) = %d, %v; want %d", test.raw, got, err, test.want)
		}
		if !test.ok && err == nil {
			t.Errorf("endpointPort(%q) = %d, want error", test.raw, got)
		}
	}
}

func TestJobVariablesJSONPreservesTypesAndEscaping(t *testing.T) {
	got, err := jobVariablesJSON(map[string]any{
		"zeta":  7,
		"alpha": "first\\second",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["alpha"] != "first\\second" ||
		decoded["zeta"] != float64(7) {
		t.Fatalf("variables decoded as %#v", decoded)
	}
}

func TestJobVariablesJSONReturnsEncodingError(t *testing.T) {
	if _, err := jobVariablesJSON(
		map[string]any{"unsupported": make(chan struct{})},
	); err == nil {
		t.Fatal("unsupported job variable encoded without error")
	}
}

func TestStringMapsEqual(t *testing.T) {
	if !stringMapsEqual(
		map[string]string{"account": "tester", "secret": "value"},
		map[string]string{"secret": "value", "account": "tester"},
	) {
		t.Fatal("equal maps were reported different")
	}
	if stringMapsEqual(
		map[string]string{"secret": "old"},
		map[string]string{"secret": "new"},
	) {
		t.Fatal("different maps were reported equal")
	}
}

func TestManagedAccountChunkPathRequiresCanonicalIndex(t *testing.T) {
	for _, test := range []struct {
		path string
		ok   bool
	}{
		{accountChunkVariablePath(0), true},
		{accountChunkVariablePath(123), true},
		{accountChunkPathPrefix + "1", false},
		{accountChunkPathPrefix + "00000x", false},
		{agentVariablePath, false},
	} {
		if got := managedAccountChunkPath(test.path); got != test.ok {
			t.Errorf(
				"managedAccountChunkPath(%q) = %t, want %t",
				test.path,
				got,
				test.ok,
			)
		}
	}
}

func TestManagedGameWorldVariableJobID(t *testing.T) {
	for _, test := range []struct {
		path string
		job  string
		ok   bool
	}{
		{
			"nomad/jobs/sro-gameworld-test/gameworld/gameworld",
			"sro-gameworld-test",
			true,
		},
		{"nomad/jobs/sro-agent/agent/agent", "", false},
		{"nomad/jobs/sro-gameworld-test/other/task", "", false},
		{"unrelated/sro-gameworld-test/gameworld/gameworld", "", false},
	} {
		job, ok := managedGameWorldVariableJobID(test.path)
		if job != test.job || ok != test.ok {
			t.Errorf(
				"managedGameWorldVariableJobID(%q) = %q, %t; want %q, %t",
				test.path,
				job,
				ok,
				test.job,
				test.ok,
			)
		}
	}
}

func TestMaximumShardIDFitsNomadVariablePath(t *testing.T) {
	shardID := strings.Repeat("a", shard.MaxIDBytes)
	path := gameWorldVariablePrefix +
		gameWorldJobPrefix +
		shardID +
		gameWorldVariableSuffix
	if err := validateNomadVariablePath(path); err != nil {
		t.Fatalf(
			"maximum catalog shard ID generated invalid Nomad path: %v",
			err,
		)
	}
}

func TestValidateDesiredVariablesPinsNomadLimits(t *testing.T) {
	validPath := "nomad/jobs/sro-agent/agent/agent"
	exactValue := strings.Repeat(
		"x",
		nomadVariableItemsBytes-len("secret"),
	)
	if err := validateDesiredVariables([]desiredVariable{{
		jobID: "sro-agent",
		path:  validPath,
		items: nomad.VariableItems{"secret": exactValue},
	}}); err != nil {
		t.Fatalf("exact Nomad item budget was refused: %v", err)
	}

	tests := []struct {
		name      string
		variables []desiredVariable
	}{
		{
			name: "path too long",
			variables: []desiredVariable{{
				jobID: "job",
				path:  strings.Repeat("a", nomadVariablePathBytes+1),
			}},
		},
		{
			name: "path character",
			variables: []desiredVariable{{
				jobID: "job",
				path:  "nomad/jobs/not.allowed",
			}},
		},
		{
			name: "duplicate path",
			variables: []desiredVariable{
				{jobID: "job-a", path: validPath},
				{jobID: "job-b", path: validPath},
			},
		},
		{
			name: "items too large",
			variables: []desiredVariable{{
				jobID: "job",
				path:  validPath,
				items: nomad.VariableItems{
					"secret": exactValue + "x",
				},
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDesiredVariables(
				test.variables,
			); err == nil {
				t.Fatal("invalid Nomad Variable input was accepted")
			}
		})
	}
}

func TestJobReleaseIDsCollectsRetainedHistory(t *testing.T) {
	versions := []*nomad.Job{
		{
			TaskGroups: []*nomad.TaskGroup{{
				Tasks: []*nomad.Task{{
					Env: map[string]string{
						"SRO_RELEASE_ID": "current",
					},
				}},
			}},
		},
		{
			TaskGroups: []*nomad.TaskGroup{{
				Tasks: []*nomad.Task{{
					Env: map[string]string{
						"SRO_RELEASE_ID": "previous",
					},
				}},
			}},
		},
	}
	got := jobReleaseIDs(versions)
	for _, releaseID := range []string{"current", "previous"} {
		if _, exists := got[releaseID]; !exists {
			t.Errorf("retained releases lack %q: %#v", releaseID, got)
		}
	}
}

func TestDesiredAllocationCountSumsTaskGroups(t *testing.T) {
	one := 1
	two := 2
	job := &nomad.Job{TaskGroups: []*nomad.TaskGroup{
		{Count: &one},
		{Count: &two},
	}}
	if got := desiredAllocationCount(job); got != 3 {
		t.Fatalf("desired allocation count = %d, want 3", got)
	}
}

func TestReconciliationTimeoutComesFromNomadPolicy(t *testing.T) {
	progressDeadline := 5 * time.Minute
	jobID := "sro-gameworld-test"
	job := &nomad.Job{
		ID: &jobID,
		Update: &nomad.UpdateStrategy{
			ProgressDeadline: &progressDeadline,
		},
	}
	got, err := reconciliationTimeout(job)
	if err != nil {
		t.Fatal(err)
	}
	want := progressDeadline + reconciliationMargin
	if got != want {
		t.Fatalf("reconciliation timeout = %s, want %s", got, want)
	}

	job.Update = nil
	if _, err := reconciliationTimeout(job); err == nil {
		t.Fatal("job without a progress deadline was accepted")
	}
}

func TestDeploymentForJobVersionIgnoresHistoryAndDetectsAdvance(
	t *testing.T,
) {
	deployments := []*nomad.Deployment{
		{
			JobCreateIndex: 10,
			JobVersion:     4,
			CreateIndex:    100,
			Status:         nomad.DeploymentStatusFailed,
		},
		{
			JobCreateIndex: 10,
			JobVersion:     5,
			CreateIndex:    110,
			Status:         nomad.DeploymentStatusSuccessful,
		},
		{
			JobCreateIndex: 9,
			JobVersion:     4,
			CreateIndex:    120,
			Status:         nomad.DeploymentStatusSuccessful,
		},
	}
	got, newer := deploymentForJobVersion(deployments, 10, 4)
	if got != deployments[0] {
		t.Fatalf("deployment = %#v, want current incarnation version 4", got)
	}
	if newer != 5 {
		t.Fatalf("newer version = %d, want 5", newer)
	}
}

func TestRenderAgentTemplateCapsAccountReferences(t *testing.T) {
	template := []byte(agentAccountToken)
	if _, err := renderAgentTemplate(
		template,
		maxAccountChunks+1,
	); err == nil {
		t.Fatal("oversized account chunk reference set was accepted")
	}
}

func TestStageReleaseIsImmutable(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.exe")
	destination := filepath.Join(root, "release", "server.exe")
	if err := os.WriteFile(source, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stageRelease(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := stageRelease(source, destination); err != nil {
		t.Fatalf("idempotent stage: %v", err)
	}
	if err := os.WriteFile(source, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stageRelease(source, destination); err == nil {
		t.Fatal("mutable source replaced an immutable release")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("immutable release = %q, want first", got)
	}
}

func TestPruneReleasesKeepsCurrentIdentity(t *testing.T) {
	stateDir := t.TempDir()
	for _, path := range []string{
		filepath.Join(stateDir, "releases", "agent", "current"),
		filepath.Join(stateDir, "releases", "agent", "old"),
		filepath.Join(stateDir, "releases", "gameworld", "current"),
		filepath.Join(stateDir, "releases", "gameworld", "old"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	deployment := &deployment{
		StateDir:       stateDir,
		ReleaseDir:     filepath.Join(stateDir, "releases"),
		AgentReleaseID: "current",
		GameReleaseID:  "current",
	}
	retained := map[string]map[string]struct{}{
		"agent":     {"current": {}},
		"gameworld": {"current": {}},
	}
	if err := deployment.pruneReleases(retained); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"agent", "gameworld"} {
		if _, err := os.Stat(filepath.Join(
			stateDir,
			"releases",
			role,
			"current",
		)); err != nil {
			t.Fatalf("%s current release: %v", role, err)
		}
		if _, err := os.Stat(filepath.Join(
			stateDir,
			"releases",
			role,
			"old",
		)); !os.IsNotExist(err) {
			t.Fatalf("%s old release still exists: %v", role, err)
		}
	}
}
