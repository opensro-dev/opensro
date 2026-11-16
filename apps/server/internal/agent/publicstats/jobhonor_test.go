/*
===========================================================================

jobhonor_test.go - the job honor board: aliases, privacy, filters (#3530)

===========================================================================
*/
package publicstats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/jobhonor"
)

var honorNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

/*
================
honorService

A board over a fixed roster: a trader, a thief, a hidden trader and a
character without a job alias, each with live standings.
================
*/
func honorService() *Service {
	char := func(id int64, name, alias string, hidden bool) *domain.Character {
		c := &domain.Character{ID: id, Name: name, PublicHidden: hidden}
		c.Job.Alias = alias
		return c
	}
	chars := []*domain.Character{
		char(1, "TraderChar", "GoldRoad", false),
		char(2, "ThiefChar", "NightCat", false),
		char(3, "HiddenChar", "Shade", true),
		char(4, "NoAliasChar", "", false),
	}
	standings := []jobhonor.Standing{
		{CharacterID: 1, Job: jobhonor.JobTrader, Points: 400, Rank: 1, Tier: jobhonor.TierKing, Held: jobhonor.TierGold},
		{CharacterID: 3, Job: jobhonor.JobTrader, Points: 900, Rank: 1, Tier: jobhonor.TierKing},
		{CharacterID: 4, Job: jobhonor.JobTrader, Points: 800, Rank: 2, Tier: jobhonor.TierGold},
		{CharacterID: 2, Job: jobhonor.JobThief, Points: 600, Rank: 1, Tier: jobhonor.TierKing, Held: jobhonor.TierKing},
	}
	return New(Sources{
		Characters:      func() []*domain.Character { return chars },
		Now:             func() time.Time { return honorNow },
		JobHonorCurrent: func() ([]jobhonor.Standing, error) { return standings, nil },
		JobHonorPrevious: func() ([]jobhonor.Standing, time.Time, error) {
			return standings[:1], time.Date(2026, 11, 16, 5, 0, 0, 0, time.UTC), nil
		},
	})
}

/*
================
honorGet
================
*/
func honorGet(t *testing.T, s *Service, query string) (int, JobHonorResponse, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/public/v1/leaderboards/job-honor"+query, nil))
	var body JobHonorResponse
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return recorder.Code, body, recorder.Body.String()
}

func TestJobHonorBoardNamesAliasesOnlyAndHidesWhatIsHidden(t *testing.T) {
	code, body, raw := honorGet(t, honorService(), "")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, raw)
	}
	if len(body.Rows) != 2 || body.Rows[0].Alias != "NightCat" || body.Rows[1].Alias != "GoldRoad" {
		t.Fatalf("rows %+v", body.Rows)
	}
	for _, name := range []string{"TraderChar", "ThiefChar", "HiddenChar", "Shade", "NoAliasChar"} {
		if strings.Contains(raw, name) {
			t.Fatalf("the board names %q: %s", name, raw)
		}
	}
	trader := body.Rows[1]
	if trader.Job != "trader" || trader.Tier != "gold" || trader.Tonight != "king" || trader.Points != 400 {
		t.Fatalf("trader row %+v", trader)
	}
	if body.DailyCap != jobhonor.DailyCap || body.NextChange != "2026-11-17T05:00:00Z" {
		t.Fatalf("header %+v", body)
	}
}

func TestJobHonorBoardFiltersByJobAndPeriod(t *testing.T) {
	_, body, _ := honorGet(t, honorService(), "?job=thief")
	if len(body.Rows) != 1 || body.Rows[0].Alias != "NightCat" {
		t.Fatalf("thief rows %+v", body.Rows)
	}
	_, body, _ = honorGet(t, honorService(), "?period=previous")
	if len(body.Rows) != 1 || body.PeriodEnd != "2026-11-16T05:00:00Z" {
		t.Fatalf("previous %+v", body)
	}
	for _, query := range []string{"?job=hunterz", "?period=last"} {
		if code, _, _ := honorGet(t, honorService(), query); code != http.StatusBadRequest {
			t.Fatalf("%s answered %d", query, code)
		}
	}
}

func TestJobHonorBoardIsUnservedWithTheFlagOff(t *testing.T) {
	s := New(Sources{Characters: func() []*domain.Character { return nil }})
	if code, _, _ := honorGet(t, s, ""); code != http.StatusNotFound {
		t.Fatalf("off answered %d", code)
	}
}
