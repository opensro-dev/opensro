/*
===========================================================================

analyze.go - one capture directory read as a single tick budget

Three independent sources are read and then reconciled, never merged
silently: the exact zone ledger (metrics snapshot diffs), the CPU
profile (every sample, no node dropping), and the execution trace
(scheduler truth for the tick goroutines). Where two sources disagree
beyond tolerance the budget prints a BLIND SPOT line naming both -
the tool never picks a winner, because a disagreement is exactly the
blind spot the hand analysis kept walking into (#2824).

===========================================================================
*/

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Reconciliation tolerances (named, per #2824): the phase sum must
// land on phase:total within phaseTolerance; the trace and profile
// reads of running time must agree with the zones and each other
// within crossTolerance.
const (
	phaseTolerance = 0.02
	crossTolerance = 0.15
	// childTolerance is float dust for the children-vs-parent check:
	// children are sub-spans of the parent measured on the same
	// clock, so any real excess is a ledger bug, not noise.
	childTolerance = 0.001
	// hooksChildTolerance is the one wider allowance: each hook is
	// timed by its own span inside phase:hooks, so the hooks' sum
	// legitimately exceeds the parent by the per-hook timer overhead
	// (a few percent across dozens of hooks). Only the phase:hooks
	// breach uses it (#3103).
	hooksChildTolerance = 0.03
	topFramesPerZone    = 3
	topFuncsOverall     = 15
)

/*
================
zoneRow

One printed row of the nested table.
================
*/
type zoneRow struct {
	Key       string
	Depth     int
	MsPerTick float64
	Children  []*zoneRow
}

/*
================
check

One reconciliation verdict, printed and carried into budget.json.
================
*/
type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // PASS, FAIL or SKIP
	Detail string `json:"detail"`
}

/*
================
measurements

Everything the reconciliation judges, gathered from the three
sources. A nil pointer means that source is absent from the capture.
================
*/
type measurements struct {
	overall   window
	cpuWindow window
	traceWin  window
	zones     map[string]float64
	cpu       *cpuProfile
	cpuTicks  int64
	trace     *traceStats
	traceTick int64
}

/*
================
budget

The budget.json document: windows, zone rows flattened, checks and
target gaps, so a later read (or #2150's table) needs no re-parse of
the printed table.
================
*/
type budget struct {
	Windows struct {
		Ticks          int64   `json:"ticks"`
		WallMs         float64 `json:"wall_ms"`
		MeanMsPerTick  float64 `json:"mean_ms_per_tick"`
		AllocBytesTick float64 `json:"alloc_bytes_per_tick"`
		SessionsFrom   int     `json:"sessions_from"`
		SessionsTo     int     `json:"sessions_to"`
		CPUTicks       int64   `json:"cpu_window_ticks"`
		TraceTicks     int64   `json:"trace_window_ticks"`
	} `json:"windows"`
	Zones []struct {
		Key       string  `json:"key"`
		MsPerTick float64 `json:"ms_per_tick"`
	} `json:"zones"`
	Checks     []check  `json:"checks"`
	BlindSpots []string `json:"blind_spots,omitempty"`
	Targets    []struct {
		Key    string  `json:"key"`
		Now    float64 `json:"now_ms_per_tick"`
		Target float64 `json:"target_ms_per_tick"`
		Gap    float64 `json:"gap_ms_per_tick"`
	} `json:"targets,omitempty"`
	ProfileTop []funcTime `json:"profile_top,omitempty"`
	Trace      *struct {
		Goroutines        int     `json:"goroutines"`
		RunningMsPerTick  float64 `json:"running_ms_per_tick"`
		RunnableMsPerTick float64 `json:"runnable_ms_per_tick"`
		BlockedMsPerTick  float64 `json:"blocked_ms_per_tick"`
		IdleMsPerTick     float64 `json:"idle_ms_per_tick"`
	} `json:"trace,omitempty"`
}

/*
================
splitZoneKey
================
*/
func splitZoneKey(key string) (kind, name string) {
	kind, name, _ = strings.Cut(key, ":")
	return kind, name
}

/*
================
sortedZoneKeys

Zone keys of one kind, heaviest per tick first.
================
*/
func sortedZoneKeys(zones map[string]float64, kind string) []string {
	var keys []string
	for key := range zones {
		if k, _ := splitZoneKey(key); k == kind {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if zones[keys[i]] != zones[keys[j]] {
			return zones[keys[i]] > zones[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

/*
================
sumKeys
================
*/
func sumKeys(zones map[string]float64, keys []string) float64 {
	total := 0.0
	for _, key := range keys {
		total += zones[key]
	}
	return total
}

/*
================
buildTable

The nested table: phases under phase:total; divisions (with the
sub-steps that run inside them) under phase:divisions; hooks (with
the action steps that run inside them) under phase:hooks. Every
container gets an explicit unattributed row - parent minus children -
so time no child claims is printed, never absorbed. Subs and steps
are global ledger keys (they name a sub-step, not a division or
hook), so they nest under their container as a group.
================
*/
func buildTable(zones map[string]float64) []*zoneRow {
	row := func(key string, depth int, ms float64) *zoneRow {
		return &zoneRow{Key: key, Depth: depth, MsPerTick: ms}
	}
	var roots []*zoneRow

	phaseKeys := sortedZoneKeys(zones, "phase")
	divisionKeys := sortedZoneKeys(zones, "division")
	subKeys := sortedZoneKeys(zones, "sub")
	hookKeys := sortedZoneKeys(zones, "hook")
	stepKeys := sortedZoneKeys(zones, "step")

	total, hasTotal := zones["phase:total"]
	phaseSum := 0.0
	var phaseRows []*zoneRow
	for _, key := range phaseKeys {
		if key == "phase:total" {
			continue
		}
		phaseSum += zones[key]
		switch key {
		case "phase:divisions":
			divRow := row(key, 1, zones[key])
			divSum := sumKeys(zones, divisionKeys)
			for _, dk := range divisionKeys {
				divRow.Children = append(divRow.Children, row(dk, 2, zones[dk]))
			}
			divRow.Children = append(divRow.Children, row("unattributed (divisions)", 2, zones[key]-divSum))
			subSum := sumKeys(zones, subKeys)
			for _, sk := range subKeys {
				divRow.Children = append(divRow.Children, row(sk, 2, zones[sk]))
			}
			if len(subKeys) > 0 {
				divRow.Children = append(divRow.Children, row("unattributed (sub-steps)", 2, divSum-subSum))
			}
			phaseRows = append(phaseRows, divRow)
		case "phase:hooks":
			hookRow := row(key, 1, zones[key])
			hookSum := sumKeys(zones, hookKeys)
			for _, hk := range hookKeys {
				hookRow.Children = append(hookRow.Children, row(hk, 2, zones[hk]))
			}
			hookRow.Children = append(hookRow.Children, row("unattributed (hooks)", 2, zones[key]-hookSum))
			stepSum := sumKeys(zones, stepKeys)
			for _, sk := range stepKeys {
				hookRow.Children = append(hookRow.Children, row(sk, 2, zones[sk]))
			}
			if len(stepKeys) > 0 {
				hookRow.Children = append(hookRow.Children, row("unattributed (steps)", 2, hookSum-stepSum))
			}
			phaseRows = append(phaseRows, hookRow)
		default:
			phaseRows = append(phaseRows, row(key, 1, zones[key]))
		}
	}
	if hasTotal {
		root := row("phase:total", 0, total)
		root.Children = append(root.Children, phaseRows...)
		root.Children = append(root.Children, row("unattributed (phases)", 1, total-phaseSum))
		roots = append(roots, root)
	} else {
		roots = append(roots, phaseRows...)
		// No phase container recorded: show the groups flat.
		for _, key := range divisionKeys {
			roots = append(roots, row(key, 0, zones[key]))
		}
		for _, key := range subKeys {
			roots = append(roots, row(key, 0, zones[key]))
		}
		for _, key := range hookKeys {
			roots = append(roots, row(key, 0, zones[key]))
		}
		for _, key := range stepKeys {
			roots = append(roots, row(key, 0, zones[key]))
		}
	}
	known := map[string]bool{"phase": true, "division": true, "sub": true, "hook": true, "step": true}
	var other []string
	for key := range zones {
		if kind, _ := splitZoneKey(key); !known[kind] {
			other = append(other, key)
		}
	}
	sort.Strings(other)
	for _, key := range other {
		roots = append(roots, row(key, 0, zones[key]))
	}
	return roots
}

/*
================
flattenRows
================
*/
func flattenRows(rows []*zoneRow, out *[]*zoneRow) {
	for _, r := range rows {
		*out = append(*out, r)
		flattenRows(r.Children, out)
	}
}

/*
================
within

Relative agreement: |a-b| within tol of the larger read.
================
*/
func within(a, b, tol float64) bool {
	scale := math.Max(math.Abs(a), math.Abs(b))
	if scale == 0 {
		return true
	}
	return math.Abs(a-b)/scale <= tol
}

/*
================
reconcile

The five reconciliation reads (#2824). Every FAIL becomes a BLIND
SPOT line naming the two disagreeing sources and both numbers; a
missing source is a SKIP, never a silent pass.
================
*/
func reconcile(m *measurements) []check {
	var checks []check
	fail := func(name, format string, args ...any) check {
		return check{Name: name, Status: "FAIL", Detail: "BLIND SPOT: " + name + ": " + fmt.Sprintf(format, args...)}
	}
	pass := func(name, format string, args ...any) check {
		return check{Name: name, Status: "PASS", Detail: fmt.Sprintf(format, args...)}
	}
	skip := func(name, why string) check {
		return check{Name: name, Status: "SKIP", Detail: why}
	}

	zones := m.zones
	// (a) the phases sum to phase:total.
	if total, ok := zones["phase:total"]; ok {
		sum := 0.0
		for _, key := range sortedZoneKeys(zones, "phase") {
			if key != "phase:total" {
				sum += zones[key]
			}
		}
		name := "phases sum to phase:total (metrics zones)"
		if within(sum, total, phaseTolerance) {
			checks = append(checks, pass(name, "sum of phases %.3f ms/tick vs phase:total %.3f ms/tick (within %.0f%%)", sum, total, phaseTolerance*100))
		} else {
			checks = append(checks, fail(name, "metrics zones disagree with themselves: sum of phases %.3f ms/tick vs phase:total %.3f ms/tick (tolerance %.0f%%)", sum, total, phaseTolerance*100))
		}
	} else {
		checks = append(checks, skip("phases sum to phase:total (metrics zones)", "no phase:total in tick_zones (server build predates the zone ledger, or no zones in the window)"))
	}

	zoneSum, zoneSumOK := zones["phase:total"]
	if !zoneSumOK {
		zoneSum = sumKeys(zones, sortedZoneKeys(zones, "phase"))
		zoneSumOK = len(zones) > 0
	}

	// (b) trace running vs zones.
	name := "trace running vs metrics zones"
	if m.trace != nil && zoneSumOK && m.traceTick > 0 {
		tracePerTick := m.trace.runningMs() / float64(m.traceTick)
		if within(tracePerTick, zoneSum, crossTolerance) {
			checks = append(checks, pass(name, "trace tick goroutines ran %.3f ms/tick vs zones %.3f ms/tick (within %.0f%%)", tracePerTick, zoneSum, crossTolerance*100))
		} else {
			checks = append(checks, fail(name, "execution trace says the tick goroutines ran %.3f ms/tick but metrics zones sum to %.3f ms/tick (tolerance %.0f%%)", tracePerTick, zoneSum, crossTolerance*100))
		}
	} else {
		checks = append(checks, skip(name, "needs both an execution trace and tick_zones in the window"))
	}

	// (c) profile CPU vs trace running: both sides are the tick
	// goroutines. The rest of the process (session loops, store lanes,
	// the collector) is reported beside it, never compared.
	name = "profile CPU vs trace running"
	if m.cpu != nil && m.trace != nil && m.cpuTicks > 0 && m.traceTick > 0 {
		cpuPerTick := m.cpu.tickMs() / float64(m.cpuTicks)
		offTick := (m.cpu.totalMs() - m.cpu.tickMs()) / float64(m.cpuTicks)
		tracePerTick := m.trace.runningMs() / float64(m.traceTick)
		if within(cpuPerTick, tracePerTick, crossTolerance) {
			checks = append(checks, pass(name, "CPU profile on the tick goroutines %.3f ms/tick vs trace running %.3f ms/tick (within %.0f%%); the rest of the process used %.3f ms/tick", cpuPerTick, tracePerTick, crossTolerance*100, offTick))
		} else {
			checks = append(checks, fail(name, "CPU profile measured %.3f ms/tick of CPU on the tick goroutines but the execution trace ran them %.3f ms/tick (tolerance %.0f%%) - one of the two is being misread", cpuPerTick, tracePerTick, crossTolerance*100))
		}
	} else {
		checks = append(checks, skip(name, "needs both a CPU profile and an execution trace"))
	}

	// (d) runnable and blocked, reported with their top stacks.
	name = "runnable and blocked (trace report)"
	if m.trace != nil && m.traceTick > 0 {
		checks = append(checks, pass(name, "tick goroutines: runnable %.3f ms/tick, blocked %.3f ms/tick over %d ticks (%d tick goroutines; the wait between ticks, %.3f ms/tick, is not counted as blocked)",
			m.trace.RunnableMs/float64(m.traceTick), m.trace.BlockedMs/float64(m.traceTick), m.traceTick, m.trace.Goroutines, m.trace.IdleMs/float64(m.traceTick)))
	} else {
		checks = append(checks, skip(name, "no execution trace in the capture"))
	}

	// (e) children never exceed their parent.
	name = "children within parents (metrics zones)"
	if len(zones) > 0 {
		var breaches []string
		breach := func(parent string, parentMs float64, children []string, tolerance float64) {
			sum := sumKeys(zones, children)
			if sum > parentMs*(1+tolerance) {
				breaches = append(breaches, fmt.Sprintf("children of %s sum to %.3f ms/tick, above the parent's %.3f ms/tick (tolerance %.0f%%)", parent, sum, parentMs, tolerance*100))
			}
		}
		if total, ok := zones["phase:total"]; ok {
			var phases []string
			for _, key := range sortedZoneKeys(zones, "phase") {
				if key != "phase:total" {
					phases = append(phases, key)
				}
			}
			breach("phase:total", total, phases, childTolerance)
		}
		if divisions, ok := zones["phase:divisions"]; ok {
			breach("phase:divisions", divisions, sortedZoneKeys(zones, "division"), childTolerance)
			breach("the divisions (sub-steps)", sumKeys(zones, sortedZoneKeys(zones, "division")), sortedZoneKeys(zones, "sub"), childTolerance)
		}
		if hooks, ok := zones["phase:hooks"]; ok {
			breach("phase:hooks", hooks, sortedZoneKeys(zones, "hook"), hooksChildTolerance)
			breach("the hooks (steps)", sumKeys(zones, sortedZoneKeys(zones, "hook")), sortedZoneKeys(zones, "step"), childTolerance)
		}
		if len(breaches) == 0 {
			checks = append(checks, pass(name, "every container's children fit inside it (phase:hooks children allowed %.0f%% over for per-hook timer overhead)", hooksChildTolerance*100))
		} else {
			checks = append(checks, fail(name, "metrics zones disagree with themselves: %s", strings.Join(breaches, "; ")))
		}
	} else {
		checks = append(checks, skip(name, "no tick_zones in the window"))
	}
	return checks
}

/*
================
zonePattern

The function regex a zone's CPU frames are gathered under: the zone
name as it appears in server function names; the whole-tick phases
gather under the tick entries.
================
*/
func zonePattern(key string) *regexp.Regexp {
	kind, name := splitZoneKey(key)
	if kind == "phase" {
		return regexp.MustCompile(`Ticker\)\.(runScheduledTick|runDivision)`)
	}
	if name == "" {
		return nil
	}
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta(name))
}

/*
================
loadTargets
================
*/
func loadTargets(path string) (map[string]float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]float64)
	if err := json.Unmarshal(raw, &targets); err != nil {
		return nil, fmt.Errorf("%s: targets are a JSON object of zone key -> ms/tick: %w", path, err)
	}
	return targets, nil
}

/*
================
gatherMeasurements

Read every source a capture directory holds.
================
*/
func gatherMeasurements(dir string) (*measurements, []string, error) {
	snaps, err := loadSnapshots(dir)
	if err != nil {
		return nil, nil, err
	}
	overall, cpuWin, traceWin, err := pickWindows(snaps)
	if err != nil {
		return nil, nil, err
	}
	m := &measurements{overall: overall, cpuWindow: cpuWin, traceWin: traceWin, zones: overall.ZoneMsPerTick, cpuTicks: cpuWin.Ticks, traceTick: traceWin.Ticks}
	var notes []string
	if len(overall.ZoneMsPerTick) == 0 {
		notes = append(notes, "note: the metrics snapshots carry no tick_zones (a build from before the zone ledger) - zone rows and zone checks are skipped, profile and trace still reconcile")
	}
	if cpuPath, ok := findCPUProfile(dir); ok {
		if cpu, err := loadCPUProfile(cpuPath); err == nil {
			m.cpu = cpu
		} else {
			notes = append(notes, "note: CPU profile not read: "+err.Error())
		}
	}
	tracePath := filepath.Join(dir, "trace.out")
	if _, err := os.Stat(tracePath); err == nil {
		if stats, err := reduceTrace(tracePath); err == nil {
			m.trace = stats
		} else {
			notes = append(notes, "note: execution trace not read: "+err.Error())
		}
	}
	return m, notes, nil
}

/*
================
analyzeDir

Print the budget for one capture directory and write its budget.json.
Returns the number of BLIND SPOT lines printed.
================
*/
func analyzeDir(dir, targetsPath string, w io.Writer) (int, error) {
	m, notes, err := gatherMeasurements(dir)
	if err != nil {
		return 0, err
	}
	var targets map[string]float64
	if targetsPath != "" {
		if targets, err = loadTargets(targetsPath); err != nil {
			return 0, err
		}
	}

	fmt.Fprintf(w, "tick budget: %s\n", dir)
	for _, note := range notes {
		fmt.Fprintln(w, note)
	}
	fmt.Fprintf(w, "window: %d ticks, %.1f ms of tick wall (%.3f ms/tick mean), allocation %.0f bytes/tick, live sessions %d -> %d\n",
		m.overall.Ticks, m.overall.WallMs, m.overall.MeanMsPerTick, m.overall.AllocBytesTick, m.overall.SessionsFrom, m.overall.SessionsTo)
	fmt.Fprintf(w, "windows for profile and trace: cpu %d ticks, trace %d ticks\n\n", m.cpuTicks, m.traceTick)

	rows := buildTable(m.zones)
	var flat []*zoneRow
	flattenRows(rows, &flat)
	if len(flat) > 0 {
		fmt.Fprintln(w, "zones (ms/tick, exact from tick_zones):")
		for _, r := range flat {
			fmt.Fprintf(w, "%s%-28s %9.3f\n", strings.Repeat("  ", r.Depth+1), r.Key, r.MsPerTick)
		}
		fmt.Fprintln(w)
	}

	printLegCensus(w, m.overall)

	if m.cpu != nil {
		fmt.Fprintf(w, "cpu profile: %.1f ms over %d samples = %.3f ms/tick whole process, %.3f ms/tick on the tick goroutines (cpu window)\n",
			m.cpu.totalMs(), m.cpu.samples, m.cpu.totalMs()/float64(m.cpuTicks), m.cpu.tickMs()/float64(m.cpuTicks))
		fmt.Fprintln(w, "top cumulative functions (no node dropping):")
		for _, ft := range m.cpu.topFuncs(topFuncsOverall) {
			fmt.Fprintf(w, "  %9.3f ms/tick  %s\n", ft.Ms/float64(m.cpuTicks), ft.Func)
		}
		fmt.Fprintln(w)
		if len(flat) > 0 {
			fmt.Fprintln(w, "top frames under each zone (cumulative, samples whose stack names the zone):")
			for _, r := range flat {
				if strings.HasPrefix(r.Key, "unattributed") {
					continue
				}
				pattern := zonePattern(r.Key)
				if pattern == nil {
					continue
				}
				top := m.cpu.topFuncsUnder(pattern, topFramesPerZone)
				if len(top) == 0 {
					continue
				}
				fmt.Fprintf(w, "  %s:\n", r.Key)
				for _, ft := range top {
					fmt.Fprintf(w, "    %9.3f ms/tick  %s\n", ft.Ms/float64(m.cpuTicks), ft.Func)
				}
			}
			fmt.Fprintln(w)
		}
	}

	if m.trace != nil {
		fmt.Fprintf(w, "trace: %d tick goroutines over %.1f ms: running %.3f ms/tick, runnable %.3f ms/tick, blocked %.3f ms/tick, waiting between ticks %.3f ms/tick\n",
			m.trace.Goroutines, m.trace.DurationMs, m.trace.runningMs()/float64(m.traceTick),
			m.trace.RunnableMs/float64(m.traceTick), m.trace.BlockedMs/float64(m.traceTick), m.trace.IdleMs/float64(m.traceTick))
		if len(m.trace.topBlocking) > 0 {
			fmt.Fprintln(w, "top blocking stacks (transitions into waiting):")
			for _, sc := range m.trace.topBlocking {
				fmt.Fprintf(w, "  %5d  %s\n", sc.Count, sc.Stack)
			}
		}
		if len(m.trace.topRunnable) > 0 {
			fmt.Fprintln(w, "top runnable stacks (transitions into runnable):")
			for _, sc := range m.trace.topRunnable {
				fmt.Fprintf(w, "  %5d  %s\n", sc.Count, sc.Stack)
			}
		}
		fmt.Fprintln(w)
	}

	checks := reconcile(m)
	blindSpots := 0
	fmt.Fprintln(w, "reconciliation:")
	for _, c := range checks {
		if c.Status == "FAIL" {
			blindSpots++
			fmt.Fprintf(w, "  FAIL  %s\n", c.Detail)
		} else {
			fmt.Fprintf(w, "  %-4s  %s: %s\n", c.Status, c.Name, c.Detail)
		}
	}
	fmt.Fprintln(w)

	doc := budget{}
	doc.Windows.Ticks = m.overall.Ticks
	doc.Windows.WallMs = m.overall.WallMs
	doc.Windows.MeanMsPerTick = m.overall.MeanMsPerTick
	doc.Windows.AllocBytesTick = m.overall.AllocBytesTick
	doc.Windows.SessionsFrom = m.overall.SessionsFrom
	doc.Windows.SessionsTo = m.overall.SessionsTo
	doc.Windows.CPUTicks = m.cpuTicks
	doc.Windows.TraceTicks = m.traceTick
	for _, r := range flat {
		doc.Zones = append(doc.Zones, struct {
			Key       string  `json:"key"`
			MsPerTick float64 `json:"ms_per_tick"`
		}{Key: r.Key, MsPerTick: r.MsPerTick})
	}
	doc.Checks = checks
	for _, c := range checks {
		if c.Status == "FAIL" {
			doc.BlindSpots = append(doc.BlindSpots, c.Detail)
		}
	}
	if m.cpu != nil {
		for _, ft := range m.cpu.topFuncs(topFuncsOverall) {
			doc.ProfileTop = append(doc.ProfileTop, funcTime{Func: ft.Func, Ms: ft.Ms / float64(m.cpuTicks)})
		}
	}
	if m.trace != nil {
		doc.Trace = &struct {
			Goroutines        int     `json:"goroutines"`
			RunningMsPerTick  float64 `json:"running_ms_per_tick"`
			RunnableMsPerTick float64 `json:"runnable_ms_per_tick"`
			BlockedMsPerTick  float64 `json:"blocked_ms_per_tick"`
			IdleMsPerTick     float64 `json:"idle_ms_per_tick"`
		}{Goroutines: m.trace.Goroutines, RunningMsPerTick: m.trace.runningMs() / float64(m.traceTick),
			RunnableMsPerTick: m.trace.RunnableMs / float64(m.traceTick), BlockedMsPerTick: m.trace.BlockedMs / float64(m.traceTick),
			IdleMsPerTick: m.trace.IdleMs / float64(m.traceTick)}
	}

	if len(targets) > 0 {
		keys := make([]string, 0, len(targets))
		for key := range targets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fmt.Fprintln(w, "targets (ms/tick):")
		fmt.Fprintf(w, "  %-28s %9s %9s %9s\n", "zone", "now", "target", "gap")
		for _, key := range keys {
			now := m.zones[key]
			gap := now - targets[key]
			fmt.Fprintf(w, "  %-28s %9.3f %9.3f %+9.3f\n", key, now, targets[key], gap)
			doc.Targets = append(doc.Targets, struct {
				Key    string  `json:"key"`
				Now    float64 `json:"now_ms_per_tick"`
				Target float64 `json:"target_ms_per_tick"`
				Gap    float64 `json:"gap_ms_per_tick"`
			}{Key: key, Now: now, Target: targets[key], Gap: gap})
		}
		fmt.Fprintln(w)
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return blindSpots, err
	}
	if err := os.WriteFile(filepath.Join(dir, "budget.json"), append(raw, '\n'), 0o644); err != nil {
		return blindSpots, err
	}
	if blindSpots > 0 {
		fmt.Fprintf(w, "%d BLIND SPOT line(s) above: the sources disagree and the tool does not pick a winner.\n", blindSpots)
	}
	return blindSpots, nil
}

/*
================
printLegCensus

The monster leg's window read (#3119): actors dispatched, steps and
settled re-checks per tick, the rest guards as shares of the window's
steps, and the steps split by the mode each step read. A capture from
before the monster_leg block existed prints nothing here - the census
was not recorded, and silence says so.
================
*/
func printLegCensus(w io.Writer, win window) {
	if len(win.LegDeltas) == 0 || win.Ticks <= 0 {
		return
	}
	ticks := float64(win.Ticks)
	steps := win.LegDeltas["steps"]
	fmt.Fprintln(w, "monster leg (window deltas per tick):")
	fmt.Fprintf(w, "  actors dispatched %.2f/tick, steps %.2f/tick, re-checks settled %.2f/tick\n",
		float64(win.LegDeltas["actors"])/ticks, float64(steps)/ticks, float64(win.LegDeltas["rechecked"])/ticks)
	if steps == 0 {
		fmt.Fprintln(w)
		return
	}
	var guards []string
	for key := range win.LegDeltas {
		if name, ok := strings.CutPrefix(key, "rest."); ok {
			guards = append(guards, name)
		}
	}
	sort.Slice(guards, func(i, j int) bool {
		left, right := win.LegDeltas["rest."+guards[i]], win.LegDeltas["rest."+guards[j]]
		if left != right {
			return left > right
		}
		return guards[i] < guards[j]
	})
	if len(guards) > 0 {
		parts := make([]string, 0, len(guards))
		for _, guard := range guards {
			parts = append(parts, fmt.Sprintf("%s %.1f%%", guard, 100*float64(win.LegDeltas["rest."+guard])/float64(steps)))
		}
		fmt.Fprintf(w, "  rest guards (share of steps): %s\n", strings.Join(parts, ", "))
	}
	modes := []string{"idle", "wandering", "chasing", "attacking", "returning", "pending", "other"}
	seen := make(map[string]bool, len(modes))
	parts := make([]string, 0, len(modes)+2)
	for _, mode := range modes {
		seen[mode] = true
		parts = append(parts, fmt.Sprintf("%s %.2f", mode, float64(win.LegDeltas["mode."+mode])/ticks))
	}
	var extra []string
	for key := range win.LegDeltas {
		if name, ok := strings.CutPrefix(key, "mode."); ok && !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, mode := range extra {
		parts = append(parts, fmt.Sprintf("%s %.2f", mode, float64(win.LegDeltas["mode."+mode])/ticks))
	}
	fmt.Fprintf(w, "  steps by mode (per tick): %s\n", strings.Join(parts, ", "))
	fmt.Fprintln(w)
}
