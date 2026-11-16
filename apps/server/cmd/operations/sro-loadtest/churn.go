/*
===========================================================================

churn.go - sro-loadtest -churn: one account entering and dropping at a rate

The #3321 load test. One account signs in once, then starts a fresh
attempt at the given rate for the given time: a new HELLO ticket, a new
socket, a new EnterWorld bind token and the EnterWorld frame, and the
socket is dropped the moment the answer arrives (or the attempt fails).
That is the enter/drop loop #3303 found on the live service. The run
reports, per attempt, how far it got and what refused it, and around
the run the GameWorld metrics (tick window, account_session_evictions,
live sessions) and one CPU profile, so "the loop costs the server
nothing" is a number: the profile's GameWorld share of one core, and
how many attempts reached the GameWorld at all past the ticket pacing
(#3311).

Attempts may overlap: a slow answer never delays the next start, as a
scripted loop would not wait either.

===========================================================================
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/transport"
)

// maxChurnRate bounds -churn-rate: the ticker period is a second over the
// rate, and a period that rounds to zero panics time.NewTicker.
const maxChurnRate = 1000

// churnOptions is one churn run's settings.
type churnOptions struct {
	rate        float64
	duration    time.Duration
	metricsURL  string
	pprofBase   string
	profileSecs int
}

// churnAttempt is one enter/drop attempt's row. Stage is "entered" for an
// attempt the GameWorld admitted, else the stage that refused it.
type churnAttempt struct {
	StartedMs int64  `json:"startedMs"`
	Stage     string `json:"stage"`
	Error     string `json:"error,omitempty"`
	Millis    int64  `json:"millis"`
}

// churnSummary is the run's report (churn/summary.json).
type churnSummary struct {
	Account        string         `json:"account"`
	Character      string         `json:"character"`
	RatePerSecond  float64        `json:"ratePerSecond"`
	Seconds        float64        `json:"seconds"`
	Attempts       int            `json:"attempts"`
	Entered        int            `json:"entered"`
	Stages         map[string]int `json:"stages"`
	EnterP50Ms     int64          `json:"enterP50Ms"`
	EnterMaxMs     int64          `json:"enterMaxMs"`
	EvictionsDelta *int64         `json:"accountSessionEvictionsDelta,omitempty"`
	Tick           *tickWindow    `json:"tick,omitempty"`
	Rows           []churnAttempt `json:"rows"`
}

/*
================
runChurn

Signs the account in once, runs the attempts, and writes churn/summary.json
plus the metrics snapshots and the CPU profile beside it.
================
*/
func runChurn(cfg runConfig, opts churnOptions, acct botAccount, outDir string) (churnSummary, error) {
	summary := churnSummary{Account: acct.ID, RatePerSecond: opts.rate, Stages: map[string]int{}}
	// Written as one positive range test so a NaN rate fails it too.
	if !(opts.rate > 0 && opts.rate <= maxChurnRate) || opts.duration <= 0 {
		return summary, fmt.Errorf("churn needs a rate in (0, %d] a second and a positive duration", maxChurnRate)
	}
	dir := filepath.Join(outDir, "churn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return summary, err
	}
	agent, err := newAgentClient(cfg.agentURL, cfg.serverID, cfg.division, cfg.timeout, botSourceIP(0))
	if err != nil {
		return summary, err
	}
	if err := agent.login(acct.ID, acct.Password); err != nil {
		return summary, fmt.Errorf("login: %w", err)
	}
	summary.Character = acct.Character
	if summary.Character == "" {
		names, err := agent.characterNames()
		if err != nil {
			return summary, fmt.Errorf("character list: %w", err)
		}
		if len(names) == 0 {
			return summary, fmt.Errorf("account %s has no characters on division %s", acct.ID, cfg.division)
		}
		summary.Character = names[0]
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

	ctx, cancel := context.WithTimeout(context.Background(), opts.duration)
	defer cancel()
	if opts.pprofBase != "" && opts.duration > time.Duration(opts.profileSecs)*time.Second {
		delay := (opts.duration - time.Duration(opts.profileSecs)*time.Second) / 2
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

	started := time.Now()
	var mu sync.Mutex
	var wg sync.WaitGroup
	ticker := time.NewTicker(time.Duration(float64(time.Second) / opts.rate))
	defer ticker.Stop()
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
		case at := <-ticker.C:
			wg.Add(1)
			go func(offset time.Duration) {
				defer wg.Done()
				row := churnOnce(cfg, agent, summary.Character)
				row.StartedMs = offset.Milliseconds()
				mu.Lock()
				summary.Rows = append(summary.Rows, row)
				mu.Unlock()
			}(at.Sub(started))
		}
	}
	wg.Wait()
	summary.Seconds = time.Since(started).Seconds()

	var enterMillis []int64
	for _, row := range summary.Rows {
		summary.Attempts++
		summary.Stages[row.Stage]++
		if row.Stage == "entered" {
			summary.Entered++
			enterMillis = append(enterMillis, row.Millis)
		}
	}
	sort.Slice(summary.Rows, func(i, j int) bool { return summary.Rows[i].StartedMs < summary.Rows[j].StartedMs })
	summary.EnterP50Ms, summary.EnterMaxMs = enterStats(enterMillis)
	if opts.metricsURL != "" {
		var raw []byte
		afterSnap, raw, afterErr = fetchMetrics(opts.metricsURL, cfg.timeout)
		if raw != nil {
			_ = os.WriteFile(filepath.Join(dir, "metrics-after.json"), raw, 0o644)
		}
		summary.Tick = tickOutcome(beforeSnap, afterSnap, beforeErr, afterErr)
		if beforeErr == nil && afterErr == nil {
			delta := int64(afterSnap.AccountSessionEvictions) - int64(beforeSnap.AccountSessionEvictions)
			summary.EvictionsDelta = &delta
		}
	}
	raw, err := json.MarshalIndent(summary, "", "\t")
	if err != nil {
		return summary, err
	}
	return summary, os.WriteFile(filepath.Join(dir, "summary.json"), append(raw, '\n'), 0o644)
}

/*
================
churnOnce

One attempt: HELLO ticket, socket, bind token, EnterWorld, then the
socket is dropped as soon as the answer (or a failure) arrives. The row
names the first stage that refused, or "entered".
================
*/
func churnOnce(cfg runConfig, agent *agentClient, character string) churnAttempt {
	began := time.Now()
	row := churnAttempt{}
	done := func(stage string, err error) churnAttempt {
		row.Stage = stage
		if err != nil {
			row.Error = err.Error()
		}
		row.Millis = time.Since(began).Milliseconds()
		return row
	}
	ticket, err := agent.transportToken()
	if err != nil {
		return done("transport-token", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	conn, _, err := dialGame(ctx, cfg.gameURL, ticket, cfg.timeout)
	if err != nil {
		return done("connect", err)
	}
	// The drop: the socket closes however the attempt ends.
	defer conn.close()
	bind, err := agent.enterWorldToken(character)
	if err != nil {
		return done("enterworld-token", err)
	}
	if err := conn.writeFrame(transport.Frame{
		Opcode: transport.OpEnterWorld,
		Payload: transport.EncodeEnterWorld(transport.EnterWorld{
			Division:  cfg.division,
			CharName:  character,
			AuthToken: []byte(bind),
		}),
	}); err != nil {
		return done("enterworld-send", err)
	}
	go conn.readLoop()
	for {
		select {
		case <-ctx.Done():
			return done("enterworld", ctx.Err())
		case frame, ok := <-conn.frames:
			if !ok {
				return done("enterworld", fmt.Errorf("connection closed before the enter-world answer"))
			}
			if frame.Opcode != transport.OpEnterWorldResult {
				continue
			}
			result, err := transport.DecodeEnterWorldResult(frame.Payload)
			if err != nil {
				return done("enterworld", err)
			}
			if !result.OK {
				return done("enterworld", fmt.Errorf("refused with native error code %d", result.NativeErrorCode))
			}
			return done("entered", nil)
		}
	}
}
