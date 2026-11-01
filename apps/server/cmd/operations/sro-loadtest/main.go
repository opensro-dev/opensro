/*
===========================================================================

main.go - sro-loadtest: the load staircase against an isolated GameWorld

One command runs the whole staircase: optionally provision the seeded
accounts, then for each session step ramp the bots in through the
ordinary Agent sign-in and the GameWorld's two ticket gates, hold them
for a step, and write the step report - who entered, who failed at
which stage, the metrics snapshots that bracket the step, one CPU
profile mid-step, and the tick window computed from the snapshots in
the #2003 reader's arithmetic (report.go).

The default targets are the local development stack (Agent 127.0.0.1:
8787, the global-official shard on 8788, its provisioning API on 8789).
Any other target must be loopback too, unless -allow-remote names an
isolated staging stack; the live service is refused always (target.go).

	sro-loadtest -provision \
		-provision-token-file <cluster-state>/agent-provisioning-token \
		-accounts accounts.json -out report-dir

===========================================================================
*/

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"opensro.online/server/internal/transport"
)

// runConfig is what every bot shares.
type runConfig struct {
	agentURL string
	gameURL  string
	serverID string
	division string
	timeout  time.Duration
}

// stepSummary is the step's headline row in its report directory.
type stepSummary struct {
	Sessions   int            `json:"sessions"`
	Attempted  int            `json:"attempted"`
	Entered    int            `json:"entered"`
	Failed     int            `json:"failed"`
	FailStages map[string]int `json:"failStages"`
	EnterP50Ms int64          `json:"enterP50Ms"`
	EnterMaxMs int64          `json:"enterMaxMs"`
	Tick       *tickWindow    `json:"tick,omitempty"`
	Bots       []botResult    `json:"bots"`
}

// runHeader is the report directory's first file: what ran, where, and
// against which build, so a baseline posted from the directory names
// its host and its game data (the two facts #2144's report must head
// with) without a reader hunting through step files.
type runHeader struct {
	StartedAt           string `json:"startedAt"`
	Host                string `json:"host"`
	NumCPU              int    `json:"numCPU"`
	GoVersion           string `json:"goVersion"`
	ServerBuildRevision string `json:"serverBuildRevision,omitempty"`
	ServerBuildSubject  string `json:"serverBuildSubject,omitempty"`
	GameDataBuild       string `json:"gameDataBuild,omitempty"`
	AgentURL            string `json:"agentURL"`
	GameURL             string `json:"gameURL"`
	Steps               []int  `json:"steps"`
}

/*
================
main
================
*/
func main() {
	var (
		agentURL    = flag.String("agent", "http://127.0.0.1:8787", "Agent base URL (agentapi.DefaultAddr)")
		gameURL     = flag.String("game", "http://127.0.0.1:8788", "GameWorld transport base URL (the shard catalog's transportUrl)")
		serverID    = flag.String("server", "global-official", "shard/server id the bots sign in to")
		division    = flag.String("division", "global-official", "division the bots enter")
		accountsArg = flag.String("accounts", "", "JSON file of seeded load accounts [{id,password,character}]; generated when absent and -bots names a size")
		bots        = flag.Int("bots", 500, "accounts to generate when the -accounts file is absent")
		sessionsArg = flag.String("sessions", "50,100,200,500", "comma-separated session steps")
		hold        = flag.Duration("hold", 3*time.Minute, "how long each step holds its bots in the world")
		ramp        = flag.Duration("ramp", 100*time.Millisecond, "stagger between bot starts")
		timeout     = flag.Duration("timeout", 20*time.Second, "per-request and dial timeout")
		metricsURL  = flag.String("metrics", "", "full URL of the GameWorld /transport/metrics endpoint (default: -game + the metrics path)")
		pprofBase   = flag.String("pprof", "", "base URL of the GameWorld loopback pprof endpoint (default: transport.DefaultPprofAddr)")
		profileSecs = flag.Int("profile-seconds", 60, "CPU profile length captured mid-step")
		outDir      = flag.String("out", "", "report directory to write")
		allowRemote = flag.Bool("allow-remote", false, "allow non-loopback targets (an isolated staging stack only; the live service stays refused)")
		provision   = flag.Bool("provision", false, "create the accounts and their characters on the target before running")
		provURL     = flag.String("provision-url", "http://127.0.0.1:8789", "Agent account provisioning API base URL (provisioning.DefaultPort)")
		provToken   = flag.String("provision-token-file", "", "file holding the cluster's agent-provisioning-token (required with -provision)")
		dataBuild   = flag.String("data-build", "", "game-data build id the target runs (its manifest contentDigest), recorded in the report header")
	)
	flag.Parse()
	if *accountsArg == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: -accounts and -out are required")
		os.Exit(2)
	}
	steps, err := parseSteps(*sessionsArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sessions: %v\n", err)
		os.Exit(2)
	}
	if *metricsURL == "" {
		*metricsURL = strings.TrimSuffix(*gameURL, "/") + transport.PathMetrics
	}
	if *pprofBase == "" {
		*pprofBase = "http://" + transport.DefaultPprofAddr
	}
	// The target guard runs before any socket opens, provisioning
	// included: a refused target is a usage error, never a run.
	targets := []string{*agentURL, *gameURL, *metricsURL, *pprofBase}
	if *provision {
		targets = append(targets, *provURL)
	}
	for _, target := range targets {
		if err := checkTargetURL(target, *allowRemote); err != nil {
			fmt.Fprintf(os.Stderr, "target: %v\n", err)
			os.Exit(2)
		}
	}
	accounts, err := ensureAccounts(*accountsArg, *bots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "accounts: %v\n", err)
		os.Exit(2)
	}
	for _, sessions := range steps {
		if sessions > len(accounts) {
			fmt.Fprintf(os.Stderr, "step %d wants %d sessions but only %d accounts are seeded; provision more or lower the step\n", sessions, sessions, len(accounts))
			os.Exit(2)
		}
	}
	cfg := runConfig{agentURL: *agentURL, gameURL: *gameURL, serverID: *serverID, division: *division, timeout: *timeout}
	opts := stepOptions{hold: *hold, ramp: *ramp, metricsURL: *metricsURL, pprofBase: *pprofBase, profileSecs: *profileSecs}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "out: %v\n", err)
		os.Exit(1)
	}
	if *provision {
		if *provToken == "" {
			fmt.Fprintln(os.Stderr, "provision: -provision-token-file is required with -provision")
			os.Exit(2)
		}
		prov, err := newProvisionClient(*provURL, *provToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "provision: %v\n", err)
			os.Exit(2)
		}
		if err := provisionRun(cfg, prov, accounts); err != nil {
			fmt.Fprintf(os.Stderr, "provision: %v\n", err)
			os.Exit(1)
		}
	}
	if err := writeHeader(*outDir, cfg, steps, *dataBuild); err != nil {
		fmt.Fprintf(os.Stderr, "report header: %v\n", err)
		os.Exit(1)
	}
	for _, sessions := range steps {
		summary, err := runStep(cfg, opts, accounts, sessions, *outDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "step %d: %v\n", sessions, err)
			os.Exit(1)
		}
		fmt.Printf("step %d: attempted %d, entered %d, failed %d, enter p50 %d ms, max %d ms%s\n",
			sessions, summary.Attempted, summary.Entered, summary.Failed, summary.EnterP50Ms, summary.EnterMaxMs, tickLine(summary.Tick))
	}
}

/*
================
tickLine

The step's tick verdict for the console line: the window's mean and
p99 (the number #2150's 10 ms target is judged by), or why the window
could not be read.
================
*/
func tickLine(tick *tickWindow) string {
	if tick == nil {
		return ""
	}
	if tick.Error != "" {
		return fmt.Sprintf(", tick unavailable (%s)", tick.Error)
	}
	return fmt.Sprintf(", tick mean %.1f ms p50 %.0f ms p99 %.0f ms over %d ticks", tick.MeanMs, tick.P50Ms, tick.P99Ms, tick.Ticks)
}

/*
================
writeHeader
================
*/
func writeHeader(outDir string, cfg runConfig, steps []int, dataBuild string) error {
	host, _ := os.Hostname()
	revision, subject := fetchServerBuild(cfg.agentURL, cfg.timeout)
	header := runHeader{
		StartedAt:           time.Now().UTC().Format(time.RFC3339),
		Host:                host,
		NumCPU:              runtime.NumCPU(),
		GoVersion:           runtime.Version(),
		ServerBuildRevision: revision,
		ServerBuildSubject:  subject,
		GameDataBuild:       dataBuild,
		AgentURL:            cfg.agentURL,
		GameURL:             cfg.gameURL,
		Steps:               steps,
	}
	raw, err := json.MarshalIndent(header, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "report.json"), append(raw, '\n'), 0o644)
}

// stepOptions is one staircase step's shared settings.
type stepOptions struct {
	hold        time.Duration
	ramp        time.Duration
	metricsURL  string
	pprofBase   string
	profileSecs int
}

/*
================
loadAccounts
================
*/
func loadAccounts(path string) ([]botAccount, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var accounts []botAccount
	if err := json.Unmarshal(raw, &accounts); err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no accounts in %s", path)
	}
	return accounts, nil
}

/*
================
parseSteps
================
*/
func parseSteps(arg string) ([]int, error) {
	var steps []int
	for _, part := range strings.Split(arg, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("bad session step %q", part)
		}
		steps = append(steps, n)
	}
	return steps, nil
}

/*
================
runStep

One staircase step: snapshot metrics, ramp the bots in, hold, profile
mid-step, snapshot again, write the step's report files. The tick
window is computed from the two snapshots (report.go); the raw
snapshots stay in the step directory for the #2003 reader.
================
*/
func runStep(cfg runConfig, opts stepOptions, accounts []botAccount, sessions int, outDir string) (stepSummary, error) {
	if sessions > len(accounts) {
		return stepSummary{}, fmt.Errorf("step wants %d sessions but only %d accounts are seeded", sessions, len(accounts))
	}
	dir := filepath.Join(outDir, fmt.Sprintf("step-%d", sessions))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stepSummary{}, err
	}
	var beforeSnap, afterSnap metricsSnapshot
	var beforeErr, afterErr error
	if opts.metricsURL != "" {
		var raw []byte
		beforeSnap, raw, beforeErr = fetchMetrics(opts.metricsURL, cfg.timeout)
		if raw != nil {
			_ = os.WriteFile(filepath.Join(dir, "metrics-before.json"), raw, 0o644)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	rows := make(chan botResult, sessions)
	for i := 0; i < sessions; i++ {
		go func(acct botAccount, index int) {
			rows <- runBot(ctx, cfg, acct, index)
		}(accounts[i], i)
		if opts.ramp > 0 {
			time.Sleep(opts.ramp)
		}
	}

	// Mid-step CPU profile: start it so it ends before the hold does.
	if opts.pprofBase != "" && opts.hold > time.Duration(opts.profileSecs)*time.Second {
		delay := (opts.hold - time.Duration(opts.profileSecs)*time.Second) / 2
		go func() {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			url := fmt.Sprintf("%s/debug/pprof/profile?seconds=%d", strings.TrimSuffix(opts.pprofBase, "/"), opts.profileSecs)
			_ = saveURL(url, filepath.Join(dir, "cpu.prof"), time.Duration(opts.profileSecs)*time.Second+cfg.timeout)
		}()
	}

	time.Sleep(opts.hold)
	if opts.pprofBase != "" {
		_ = saveURL(strings.TrimSuffix(opts.pprofBase, "/")+"/debug/pprof/heap", filepath.Join(dir, "heap.prof"), cfg.timeout)
	}
	cancel()

	summary := stepSummary{Sessions: sessions, FailStages: map[string]int{}}
	var enterMillis []int64
	for i := 0; i < sessions; i++ {
		row := <-rows
		summary.Attempted++
		summary.Bots = append(summary.Bots, row)
		if row.FailedStage == "" {
			summary.Entered++
			enterMillis = append(enterMillis, row.EnterMillis)
		} else {
			summary.Failed++
			summary.FailStages[row.FailedStage]++
		}
	}
	sort.Slice(summary.Bots, func(i, j int) bool { return summary.Bots[i].Account < summary.Bots[j].Account })
	summary.EnterP50Ms, summary.EnterMaxMs = enterStats(enterMillis)
	if opts.metricsURL != "" {
		var raw []byte
		afterSnap, raw, afterErr = fetchMetrics(opts.metricsURL, cfg.timeout)
		if raw != nil {
			_ = os.WriteFile(filepath.Join(dir, "metrics-after.json"), raw, 0o644)
		}
		summary.Tick = tickOutcome(beforeSnap, afterSnap, beforeErr, afterErr)
	}
	raw, err := json.MarshalIndent(summary, "", "\t")
	if err != nil {
		return summary, err
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), append(raw, '\n'), 0o644); err != nil {
		return summary, err
	}
	return summary, nil
}

/*
================
tickOutcome

The step's tick window, or the named reason it cannot be read. A
failed scrape is reported, never silently dropped: the tick block is
the step's verdict, and an absent one would read as a pass.
================
*/
func tickOutcome(before, after metricsSnapshot, beforeErr, afterErr error) *tickWindow {
	if beforeErr != nil {
		return &tickWindow{Error: "metrics-before: " + beforeErr.Error()}
	}
	if afterErr != nil {
		return &tickWindow{Error: "metrics-after: " + afterErr.Error()}
	}
	w := computeTickWindow(before, after)
	return &w
}

/*
================
enterStats

Median and worst enter latency of the bots that got in; zero when none
did (the failed rows carry the story instead).
================
*/
func enterStats(millis []int64) (p50, max int64) {
	if len(millis) == 0 {
		return 0, 0
	}
	sorted := append([]int64(nil), millis...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2], sorted[len(sorted)-1]
}

/*
================
saveURL

Fetch one URL into one report file; a failed fetch leaves no file and
the caller decides whether that matters (metrics and profiles are
evidence, never the run itself).
================
*/
func saveURL(url, path string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
