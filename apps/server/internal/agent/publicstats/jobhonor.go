/*
===========================================================================

jobhonor.go - the job honor board answer (#3530, opensro-web#149)

GET /public/v1/leaderboards/job-honor?job=all|trader|thief|hunter&period=
current|previous. The ledger (platform/jobhonor) knows only character ids;
this answer joins them to the roster at read time and names each row by
its job alias, never the character: a job alias is what keeps a thief or
a hunter anonymous in the field, as the native job ranking (0x737E) and
the seal board keep it. A deleted or public-hidden character, or one with
no job alias, has no row.

Current is the window the next tier change will rank, with the tier that
change would give ("tonight") and the tier held now. Previous is the
snapshot that set today's tiers.

Port-only, not native. Unserved (404) while SRO_JOB_HONOR is off.

===========================================================================
*/
package publicstats

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"opensro.online/server/internal/platform/jobhonor"
)

// The board's job filter and period values.
const (
	jobHonorAll      = "all"
	jobHonorCurrent  = "current"
	jobHonorPrevious = "previous"
)

// jobHonorJobs names the ledger's jobs on the wire.
var jobHonorJobs = map[int]string{jobhonor.JobTrader: "trader", jobhonor.JobThief: "thief", jobhonor.JobHunter: "hunter"}

// jobHonorTiers names the tiers on the wire.
var jobHonorTiers = map[int]string{jobhonor.TierNone: "none", jobhonor.TierSilver: "silver",
	jobhonor.TierGold: "gold", jobhonor.TierKing: "king"}

/*
================
jobHonorBoard
================
*/
func (s *Service) jobHonorBoard(r *http.Request) (any, error) {
	if s.src.JobHonorCurrent == nil || s.src.JobHonorPrevious == nil {
		return nil, errNotFound
	}
	query := r.URL.Query()
	job := query.Get("job")
	if job == "" {
		job = jobHonorAll
	}
	wanted := 0
	if job != jobHonorAll {
		for id, name := range jobHonorJobs {
			if name == job {
				wanted = id
			}
		}
		if wanted == 0 {
			return nil, fmt.Errorf("%w: job must be all, trader, thief or hunter", errBadRequest)
		}
	}
	period := query.Get("period")
	if period == "" {
		period = jobHonorCurrent
	}
	now := s.now()
	out := JobHonorResponse{Job: job, Period: period, GeneratedAt: rfc3339(now.UnixMilli()),
		NextChange: rfc3339(jobhonor.NextChange(now).UnixMilli()), DailyCap: jobhonor.DailyCap,
		WindowDays: jobhonor.WindowDays, Rows: []JobHonorRow{}}
	var standings []jobhonor.Standing
	var err error
	switch period {
	case jobHonorCurrent:
		standings, err = s.src.JobHonorCurrent()
	case jobHonorPrevious:
		var at time.Time
		standings, at, err = s.src.JobHonorPrevious()
		if !at.IsZero() {
			out.PeriodEnd = rfc3339(at.UnixMilli())
		}
	default:
		return nil, fmt.Errorf("%w: period must be current or previous", errBadRequest)
	}
	if err != nil {
		return nil, err
	}
	out.Rows = s.jobHonorRows(standings, wanted)
	return out, nil
}

/*
================
jobHonorRows

The visible rows of one job (wanted) or of every job (0), points down;
All ranks across jobs while each row keeps its own job's tier. At most
leaderboardSize rows.
================
*/
func (s *Service) jobHonorRows(standings []jobhonor.Standing, wanted int) []JobHonorRow {
	r := s.loadRoster()
	visible := make([]jobhonor.Standing, 0, len(standings))
	aliases := map[int64]string{}
	for _, row := range standings {
		if wanted != 0 && row.Job != wanted {
			continue
		}
		c := r.byID[row.CharacterID]
		if c == nil || c.PublicHidden || c.Job.Alias == "" {
			continue
		}
		aliases[row.CharacterID] = c.Job.Alias
		visible = append(visible, row)
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].Points != visible[j].Points {
			return visible[i].Points > visible[j].Points
		}
		return aliases[visible[i].CharacterID] < aliases[visible[j].CharacterID]
	})
	rows := []JobHonorRow{}
	for i, row := range visible {
		if i == leaderboardSize {
			break
		}
		rows = append(rows, JobHonorRow{Rank: i + 1, Alias: aliases[row.CharacterID], Job: jobHonorJobs[row.Job],
			Points: row.Points, Tier: jobHonorTiers[row.Held], Tonight: jobHonorTiers[row.Tier]})
	}
	return rows
}
