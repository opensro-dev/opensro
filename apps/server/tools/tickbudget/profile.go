/*
===========================================================================

profile.go - CPU profile reduction with no node dropping

The hand analysis this tool replaces lost time to pprof's node
dropping (-focus totals that silently trim). Here every sample is
walked: total CPU is the sum of the profile's own cpu sample values,
and a function's cumulative time credits the sample once per distinct
function on its stack. Nothing is dropped, folded or hidden.

===========================================================================
*/

package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/google/pprof/profile"
)

/*
================
funcTime

One function's cumulative CPU over a set of samples.
================
*/
type funcTime struct {
	Func string
	Ms   float64
}

/*
================
cpuProfile

A parsed CPU profile reduced to exact totals.
================
*/
type cpuProfile struct {
	totalNs   float64
	samples   int
	perFuncNs map[string]float64
	prof      *profile.Profile
	cpuIndex  int
}

/*
================
findCPUProfile

The CPU profile in a capture directory: cpu.pb.gz as capture writes
it, else any *.pb.gz whose sample types carry cpu time. Heap profiles
never match (their sample types are alloc/inuse), so a heap pull in
the same directory is never mistaken for CPU.
================
*/
func findCPUProfile(dir string) (string, bool) {
	names := []string{"cpu.pb.gz", "cpu.prof", "profile.pb.gz"}
	entries, _ := os.ReadDir(dir)
	seen := make(map[string]bool)
	for _, entry := range entries {
		seen[entry.Name()] = true
	}
	for _, name := range names {
		if seen[name] {
			return dir + string(os.PathSeparator) + name, true
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) > 6 && name[len(name)-6:] == ".pb.gz" {
			path := dir + string(os.PathSeparator) + name
			if prof, err := parseProfile(path); err == nil && cpuValueIndex(prof) >= 0 {
				return path, true
			}
		}
	}
	return "", false
}

/*
================
parseProfile
================
*/
func parseProfile(path string) (*profile.Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	prof, err := profile.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return prof, nil
}

/*
================
cpuValueIndex

The sample-value index carrying CPU nanoseconds, or the sample count
index when the profile has no cpu type (then the sampling period
turns counts into time). -1 when neither exists - a heap profile.
================
*/
func cpuValueIndex(prof *profile.Profile) int {
	samplesIndex := -1
	for i, st := range prof.SampleType {
		if st.Type == "cpu" {
			return i
		}
		if st.Type == "samples" {
			samplesIndex = i
		}
	}
	return samplesIndex
}

/*
================
sampleCPU

One sample's CPU nanoseconds: the cpu value itself when the profile
carries one, else count x period.
================
*/
func (p *cpuProfile) sampleCPU(sample *profile.Sample) float64 {
	if p.cpuIndex < 0 || p.cpuIndex >= len(sample.Value) {
		return 0
	}
	value := float64(sample.Value[p.cpuIndex])
	if p.prof.SampleType[p.cpuIndex].Type == "samples" {
		return value * float64(p.prof.Period)
	}
	return value
}

/*
================
loadCPUProfile

Parse and reduce a CPU profile: exact total CPU and per-function
cumulative nanoseconds over every sample, no node dropping.
================
*/
func loadCPUProfile(path string) (*cpuProfile, error) {
	prof, err := parseProfile(path)
	if err != nil {
		return nil, err
	}
	idx := cpuValueIndex(prof)
	if idx < 0 {
		return nil, fmt.Errorf("%s: no cpu or samples sample type - not a CPU profile", path)
	}
	out := &cpuProfile{prof: prof, cpuIndex: idx, perFuncNs: make(map[string]float64)}
	for _, sample := range prof.Sample {
		ns := out.sampleCPU(sample)
		out.totalNs += ns
		out.samples++
		seen := make(map[string]bool)
		for _, loc := range sample.Location {
			for _, line := range loc.Line {
				if line.Function == nil || seen[line.Function.Name] {
					continue
				}
				seen[line.Function.Name] = true
				out.perFuncNs[line.Function.Name] += ns
			}
		}
	}
	return out, nil
}

/*
================
totalMs
================
*/
func (p *cpuProfile) totalMs() float64 {
	return p.totalNs / 1e6
}

/*
================
stackFuncs

One sample's distinct function names, leaf first as recorded.
================
*/
func stackFuncs(sample *profile.Sample) []string {
	var names []string
	seen := make(map[string]bool)
	for _, loc := range sample.Location {
		for _, line := range loc.Line {
			if line.Function != nil && !seen[line.Function.Name] {
				seen[line.Function.Name] = true
				names = append(names, line.Function.Name)
			}
		}
	}
	return names
}

/*
================
topFuncsUnder

The top cumulative functions among the samples whose stack matches
the pattern - the "frames under a zone" read. A sample counts once
per distinct function on its stack, matched or not; the pattern only
chooses which samples belong to the zone.
================
*/
func (p *cpuProfile) topFuncsUnder(pattern *regexp.Regexp, limit int) []funcTime {
	perFunc := make(map[string]float64)
	for _, sample := range p.prof.Sample {
		names := stackFuncs(sample)
		matched := false
		for _, name := range names {
			if pattern.MatchString(name) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		ns := p.sampleCPU(sample)
		for _, name := range names {
			perFunc[name] += ns
		}
	}
	return topFuncTimes(perFunc, limit)
}

/*
================
topFuncs
================
*/
func (p *cpuProfile) topFuncs(limit int) []funcTime {
	return topFuncTimes(p.perFuncNs, limit)
}

/*
================
topFuncTimes
================
*/
func topFuncTimes(perFuncNs map[string]float64, limit int) []funcTime {
	out := make([]funcTime, 0, len(perFuncNs))
	for fn, ns := range perFuncNs {
		out = append(out, funcTime{Func: fn, Ms: ns / 1e6})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ms != out[j].Ms {
			return out[i].Ms > out[j].Ms
		}
		return out[i].Func < out[j].Func
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

/*
================
heapLiveBytes

The live heap a heap profile reports (inuse_space), for the capture's
two heap pulls. 0 when the profile carries no inuse_space type.
================
*/
func heapLiveBytes(path string) (uint64, error) {
	prof, err := parseProfile(path)
	if err != nil {
		return 0, err
	}
	idx := -1
	for i, st := range prof.SampleType {
		if st.Type == "inuse_space" {
			idx = i
		}
	}
	if idx < 0 {
		return 0, nil
	}
	var total int64
	for _, sample := range prof.Sample {
		total += sample.Value[idx]
	}
	return uint64(total), nil
}
