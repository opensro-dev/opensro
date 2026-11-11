/*
===========================================================================

placement.go - why a Nomad plan could not place a task group

A plan that cannot place a group answers, per group, the scheduler's
own allocation metric: how many nodes it evaluated, which constraints
filtered them, and which resource dimension ran out on the rest. The
release rehearsal used to print only the group's name ("cannot place
task groups: gameworld", 2026-11-11), which left the operator guessing
between CPU, memory, disk and ports. This file turns the metric into
one line in the order `nomad job plan` prints it, so the refusal names
its own cause.

===========================================================================
*/

package main

import (
	"fmt"
	"sort"
	"strings"

	nomad "github.com/hashicorp/nomad/api"
)

/*
================
placementFailures

One "group (reason)" entry per unplaceable group, sorted by group name
so the message is stable. A group without a metric still gets named.
================
*/
func placementFailures(failed map[string]*nomad.AllocationMetric) string {
	groups := make([]string, 0, len(failed))
	for group := range failed {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	entries := make([]string, 0, len(groups))
	for _, group := range groups {
		reason := placementReason(failed[group])
		if reason == "" {
			entries = append(entries, group)
			continue
		}
		entries = append(entries, group+" ("+reason+")")
	}
	return strings.Join(entries, "; ")
}

/*
================
placementReason

The metric's facts, those that are set: nodes evaluated, filtered by
class or constraint, exhausted by dimension, the resources the
exhausted nodes were short of, and quotas. Empty for a nil metric.
================
*/
func placementReason(metric *nomad.AllocationMetric) string {
	if metric == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("%d node(s) evaluated", metric.NodesEvaluated)}
	if metric.NodesFiltered > 0 {
		parts = append(parts, fmt.Sprintf("%d filtered", metric.NodesFiltered))
	}
	parts = appendCounts(parts, "class filtered", metric.ClassFiltered)
	parts = appendCounts(parts, "constraint filtered", metric.ConstraintFiltered)
	if metric.NodesExhausted > 0 {
		parts = append(parts, fmt.Sprintf("%d exhausted", metric.NodesExhausted))
	}
	parts = appendCounts(parts, "class exhausted", metric.ClassExhausted)
	parts = appendCounts(parts, "dimension exhausted", metric.DimensionExhausted)
	if short := shortResources(metric.ResourcesExhausted); short != "" {
		parts = append(parts, "short of "+short)
	}
	if len(metric.QuotaExhausted) > 0 {
		parts = append(parts, "quota exhausted: "+strings.Join(metric.QuotaExhausted, ", "))
	}
	return strings.Join(parts, ", ")
}

/*
================
appendCounts

"label: key=count, key=count" with keys sorted; nothing for an empty map.
================
*/
func appendCounts(parts []string, label string, counts map[string]int) []string {
	if len(counts) == 0 {
		return parts
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return append(parts, label+": "+strings.Join(pairs, ", "))
}

/*
================
shortResources

The resources each task asked for on the nodes that ran out, as
"task cpu=N MHz memory=N MB": the numbers to set against the node.
================
*/
func shortResources(exhausted map[string]*nomad.Resources) string {
	if len(exhausted) == 0 {
		return ""
	}
	tasks := make([]string, 0, len(exhausted))
	for task := range exhausted {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)
	entries := make([]string, 0, len(tasks))
	for _, task := range tasks {
		resources := exhausted[task]
		if resources == nil {
			continue
		}
		fields := []string{task}
		if resources.CPU != nil && *resources.CPU > 0 {
			fields = append(fields, fmt.Sprintf("cpu=%d MHz", *resources.CPU))
		}
		if resources.Cores != nil && *resources.Cores > 0 {
			fields = append(fields, fmt.Sprintf("cores=%d", *resources.Cores))
		}
		if resources.MemoryMB != nil && *resources.MemoryMB > 0 {
			fields = append(fields, fmt.Sprintf("memory=%d MB", *resources.MemoryMB))
		}
		if resources.DiskMB != nil && *resources.DiskMB > 0 {
			fields = append(fields, fmt.Sprintf("disk=%d MB", *resources.DiskMB))
		}
		entries = append(entries, strings.Join(fields, " "))
	}
	return strings.Join(entries, "; ")
}
