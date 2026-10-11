/*
===========================================================================

status.go - P7 server status and P8 event schedule

P7 answers from samples, never from a request: a sampler goroutine reads
the online count and the hub's cumulative tick counters every
statusSampleEvery. The tick figures are the window between the oldest and
newest sample of the last statusWindow; each hour closes one history point
(the hour's peak online and the 99th percentile of its ticks), and the
last statusHistoryPoints are kept. History lives in memory, so a restart
starts it again.

P8 lists what this server runs, from its own schedules (the fortress war
lane) and the maintenance gate, through Sources.Schedule.

Port-only, not native: the original has no public read API.

===========================================================================
*/
package publicstats

import (
	"math"
	"net/http"
	"sync"
	"time"

	"opensro.online/server/internal/transport"
)

// Status sampling: every 10 s, a 5-minute tick window, 48 hourly points.
const (
	statusSampleEvery   = 10 * time.Second
	statusWindow        = 5 * time.Minute
	statusHistoryPoints = 48
)

/*
================
ScheduledEvent

One event the server runs, as Sources.Schedule reports it. A zero End is
an open end (maintenance in progress).
================
*/
type ScheduledEvent struct {
	Kind, Title string
	Start, End  time.Time
	Running     bool
}

/*
================
statusSample
================
*/
type statusSample struct {
	at      time.Time
	online  int
	metrics transport.Metrics
}

/*
================
statusState

The sampler's memory, guarded by its own mutex: the sampler goroutine
writes it while answer builds (under Service.mu) read it.
================
*/
type statusState struct {
	mu        sync.Mutex
	samples   []statusSample
	hourStart statusSample
	hourPeak  int
	history   []StatusPoint
	stop      chan struct{}
	done      chan struct{}
}

/*
================
recordStatus

Takes one sample: trims the window, and closes the hour's history point
when the sample falls in a new hour.
================
*/
func (s *Service) recordStatus(now time.Time) {
	if s.src.Sample == nil {
		return
	}
	online, metrics := s.src.Sample()
	sample := statusSample{at: now, online: online, metrics: metrics}
	st := &s.status
	st.mu.Lock()
	defer st.mu.Unlock()
	st.samples = append(st.samples, sample)
	// Older samples leave the window, the newest always stays: after a gap
	// in sampling the window is short rather than reaching back hours.
	for len(st.samples) > 1 && st.samples[0].at.Before(now.Add(-statusWindow)) {
		st.samples = st.samples[1:]
	}
	if st.hourStart.at.IsZero() {
		st.hourStart, st.hourPeak = sample, online
		return
	}
	if now.Truncate(time.Hour).After(st.hourStart.at.Truncate(time.Hour)) {
		window := transport.TickWindowBetween(st.hourStart.metrics, metrics)
		st.history = append(st.history, StatusPoint{At: st.hourStart.at.Truncate(time.Hour).UTC().Format(time.RFC3339),
			Online: st.hourPeak, P99Ms: window.P99Ms})
		if len(st.history) > statusHistoryPoints {
			st.history = st.history[len(st.history)-statusHistoryPoints:]
		}
		st.hourStart, st.hourPeak = sample, online
		return
	}
	st.hourPeak = max(st.hourPeak, online)
}

/*
================
startStatusSampler

Samples once at once, then every statusSampleEvery until stopStatusSampler.
================
*/
func (s *Service) startStatusSampler() {
	if s.src.Sample == nil {
		return
	}
	s.status.stop, s.status.done = make(chan struct{}), make(chan struct{})
	s.recordStatus(s.now())
	go func() {
		defer close(s.status.done)
		ticker := time.NewTicker(statusSampleEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.status.stop:
				return
			case <-ticker.C:
				s.recordStatus(s.now())
			}
		}
	}()
}

/*
================
stopStatusSampler
================
*/
func (s *Service) stopStatusSampler() {
	if s.status.stop == nil {
		return
	}
	close(s.status.stop)
	<-s.status.done
	s.status.stop = nil
}

/*
================
serverStatus

P7: up, online, uptime, the recent tick window and the hourly history.
================
*/
func (s *Service) serverStatus(*http.Request) (any, error) {
	now := s.now()
	out := StatusResponse{Up: true, History: []StatusPoint{}, MasteryTotalOverride: s.src.MasteryTotalOverride}
	if !s.src.StartedAt.IsZero() {
		out.UptimeSeconds = int64(now.Sub(s.src.StartedAt) / time.Second)
	}
	st := &s.status
	st.mu.Lock()
	defer st.mu.Unlock()
	if n := len(st.samples); n > 0 {
		out.Online = st.samples[n-1].online
		window := transport.TickWindowBetween(st.samples[0].metrics, st.samples[n-1].metrics)
		out.Tick = StatusTick{P50Ms: window.P50Ms, P99Ms: window.P99Ms}
		if window.Count > 0 {
			out.Tick.SlowPercent = math.Round(float64(window.Overruns)*1000/float64(window.Count)) / 10
		}
	}
	out.History = append(out.History, st.history...)
	return out, nil
}

/*
================
schedule

P8: the server time and the events it runs, soonest first as the source
orders them.
================
*/
func (s *Service) schedule(*http.Request) (any, error) {
	now := s.now()
	out := ScheduleResponse{ServerTime: now.UTC().Format(time.RFC3339), Events: []ScheduleEvent{}}
	if s.src.Schedule == nil {
		return out, nil
	}
	for _, event := range s.src.Schedule(now) {
		row := ScheduleEvent{Kind: event.Kind, Title: event.Title, StartsAt: event.Start.UTC().Format(time.RFC3339), Running: event.Running}
		if !event.End.IsZero() {
			ends := event.End.UTC().Format(time.RFC3339)
			row.EndsAt = &ends
		}
		out.Events = append(out.Events, row)
	}
	return out, nil
}
