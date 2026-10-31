/*
===========================================================================

main.go - sro-loadtest: real-admission load against an isolated GameWorld

Stands headless bots up through the ordinary Agent sign-in and the
GameWorld's two ticket gates, holds them in the world for a step, and
writes the step report: who entered, who failed at which stage, and the
server metrics snapshots that bracket the step (the #2003 acceptance
reader turns those into the tick table). With a pprof base it also
captures one CPU profile mid-step and a heap profile at step end.

Never point this at the live shard: it signs in with the accounts it is
given and plays on whatever they can reach. The isolated GameWorld with
its copied store and game data is the only intended target.

Usage:
	sro-loadtest -agent http://127.0.0.1:PORT -game http://127.0.0.1:PORT \
		-server SHARD -division SHARD -accounts accounts.json \
		-sessions 50,100,200,500 -hold 3m -out report-dir

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
	"sort"
	"strconv"
	"strings"
	"time"
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
	Bots       []botResult    `json:"bots"`
}

/*
================
main
================
*/
func main() {
	var (
		agentURL    = flag.String("agent", "", "Agent base URL (http://host:port)")
		gameURL     = flag.String("game", "", "GameWorld transport base URL (http://host:port)")
		serverID    = flag.String("server", "", "shard/server id the bots sign in to")
		division    = flag.String("division", "", "division the bots enter")
		accountsArg = flag.String("accounts", "", "JSON file of seeded load accounts [{id,password,character}]")
		sessionsArg = flag.String("sessions", "5", "comma-separated session steps, e.g. 50,100,200,500")
		hold        = flag.Duration("hold", 30*time.Second, "how long each step holds its bots in the world")
		ramp        = flag.Duration("ramp", 100*time.Millisecond, "stagger between bot starts")
		timeout     = flag.Duration("timeout", 20*time.Second, "per-request and dial timeout")
		metricsURL  = flag.String("metrics", "", "full URL of the GameWorld /transport/metrics endpoint (optional)")
		pprofBase   = flag.String("pprof", "", "base URL of the GameWorld loopback pprof endpoint (optional)")
		profileSecs = flag.Int("profile-seconds", 60, "CPU profile length captured mid-step")
		outDir      = flag.String("out", "", "report directory to write")
	)
	flag.Parse()
	if *agentURL == "" || *gameURL == "" || *serverID == "" || *division == "" || *accountsArg == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: -agent, -game, -server, -division, -accounts and -out are required")
		os.Exit(2)
	}
	accounts, err := loadAccounts(*accountsArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "accounts: %v\n", err)
		os.Exit(2)
	}
	steps, err := parseSteps(*sessionsArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sessions: %v\n", err)
		os.Exit(2)
	}
	cfg := runConfig{agentURL: *agentURL, gameURL: *gameURL, serverID: *serverID, division: *division, timeout: *timeout}
	opts := stepOptions{hold: *hold, ramp: *ramp, metricsURL: *metricsURL, pprofBase: *pprofBase, profileSecs: *profileSecs}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "out: %v\n", err)
		os.Exit(1)
	}
	for _, sessions := range steps {
		summary, err := runStep(cfg, opts, accounts, sessions, *outDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "step %d: %v\n", sessions, err)
			os.Exit(1)
		}
		fmt.Printf("step %d: attempted %d, entered %d, failed %d, enter p50 %d ms, max %d ms\n",
			sessions, summary.Attempted, summary.Entered, summary.Failed, summary.EnterP50Ms, summary.EnterMaxMs)
	}
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
mid-step, snapshot again, write the step's report files.
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
	if opts.metricsURL != "" {
		_ = saveURL(opts.metricsURL, filepath.Join(dir, "metrics-before.json"), cfg.timeout)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rows := make(chan botResult, sessions)
	for i := 0; i < sessions; i++ {
		go func(acct botAccount) {
			rows <- runBot(ctx, cfg, acct)
		}(accounts[i])
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
		_ = saveURL(opts.metricsURL, filepath.Join(dir, "metrics-after.json"), cfg.timeout)
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
