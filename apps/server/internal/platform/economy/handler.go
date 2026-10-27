/*
===========================================================================

handler.go - /economy/v1/* on the GameWorld's local operator listener

GET only, for the staff console on the same host (operatorauth: loopback,
local diagnostics header, no Origin or forwarding, the bearer token from
the shard's economy-token file; without that file every route is 404).
The one exception is POST /economy/v1/sealcensus, which runs a census
(census.go). Contract v1 errors are {"error":{"code","message"}} with
the codes bad_metric, bad_bucket, range_too_wide, no_snapshot,
no_census, census_running and unauthorized. /economy/v1/trade is not
served yet (its shape follows with the trade ledger reasons).

===========================================================================
*/
package economy

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/operatorauth"
)

// Path is the routes' prefix.
const Path = "/economy/v1/"

// contractVersion is the meta block's contract number.
const contractVersion = 1

/*
================
Service.Handler

The routes, guarded by tokenPath.
================
*/
func (s *Service) Handler(tokenPath string) (http.Handler, error) {
	guard, found, err := operatorauth.Load(tokenPath, "economy")
	if err != nil {
		return nil, err
	}
	if !found {
		return http.NotFoundHandler(), nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !guard.Allows(r) {
			writeError(w, http.StatusForbidden, queryError{"unauthorized", "operator authentication required"})
			return
		}
		route := strings.TrimPrefix(r.URL.Path, Path)
		if r.Method != http.MethodGet && !(r.Method == http.MethodPost && route == "sealcensus" && s.sealCensus) {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.serve(w, r)
	}), nil
}

/*
================
Service.serve
================
*/
func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	route := strings.TrimPrefix(r.URL.Path, Path)
	if route == "meta" {
		s.serveMeta(w)
		return
	}
	// Kill stats read the history rollup, not a snapshot, so they
	// answer before the snapshot gate; uncounted shards (SRO_KILL_STATS
	// off) refuse the route outright (#1639).
	if route == "killstats" {
		if !s.killStats {
			http.NotFound(w, r)
			return
		}
		s.serveKillStats(w, r.URL.Query())
		return
	}
	// The seal pity rollup also reads the history, not a snapshot;
	// uncounted shards (SRO_SEAL_PITY off) refuse the route (#1690).
	if route == "sealpity" {
		if !s.sealPityStats {
			http.NotFound(w, r)
			return
		}
		s.serveSealPityStats(w, r.URL.Query())
		return
	}
	// The seal census reads its own fresh copy, never the snapshot, so
	// it also answers before the snapshot gate; disabled shards
	// (SRO_SEAL_CENSUS off, the default) refuse the route outright (#1691).
	if route == "sealcensus" {
		if !s.sealCensus {
			http.NotFound(w, r)
			return
		}
		s.serveSealCensus(w, r)
		return
	}
	s.mu.Lock()
	at, blocks := s.snapshotAt, s.distribution
	s.mu.Unlock()
	if at.IsZero() {
		writeError(w, http.StatusServiceUnavailable, queryError{"no_snapshot", "no snapshot has been taken yet"})
		return
	}
	q := r.URL.Query()
	if route == "distribution" {
		block, known := blocks[q.Get("division")]
		if !known {
			writeError(w, http.StatusNotFound, queryError{"bad_metric", "unknown division"})
			return
		}
		writeJSON(w, block)
		return
	}
	if route != "series" && route != "flows" && route != "flags" {
		http.NotFound(w, r)
		return
	}
	win, err := parseWindow(q.Get("bucket"), q.Get("from"), q.Get("to"), q.Get("division"), s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	db, err := openSnapshot(filepath.Join(s.dir, SnapshotFile))
	if err != nil {
		s.fail(w, err)
		return
	}
	defer db.Close()
	switch route {
	case "series":
		s.serveSeries(w, db, q.Get("metric"), win)
	case "flows":
		groups, err := readGroups(db, win)
		if err != nil {
			s.fail(w, err)
			return
		}
		buckets, reasons := foldFlows(win, groups)
		writeJSON(w, map[string]any{"bucket": win.bucket, "t": buckets, "reasons": reasons})
	case "flags":
		flags, err := netMintFlags(db, win, win.division)
		if err != nil {
			s.fail(w, err)
			return
		}
		if flags == nil {
			flags = []Flag{}
		}
		writeJSON(w, map[string]any{"flags": flags})
	}
}

/*
================
Service.serveMeta
================
*/
func (s *Service) serveMeta(w http.ResponseWriter) {
	s.mu.Lock()
	at, divisions := s.snapshotAt, s.divisions
	s.mu.Unlock()
	type reason struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Unit string `json:"unit"`
	}
	reasons := []reason{}
	for _, name := range domain.EconomyReasons() {
		reasons = append(reasons, reason{Name: name, Kind: domain.EconomyReasonKind(name), Unit: "gold"})
	}
	meta := map[string]any{"contract": contractVersion, "generatedAt": s.now().Unix(), "ledgerThrough": int64(0),
		"snapshotAt": nil, "divisions": append([]string{}, divisions...), "reasons": reasons}
	if !at.IsZero() {
		meta["snapshotAt"] = at.Unix()
		if db, err := openSnapshot(filepath.Join(s.dir, SnapshotFile)); err == nil {
			if through, err := ledgerThrough(db); err == nil {
				meta["ledgerThrough"] = through
			}
			db.Close()
		}
	}
	writeJSON(w, meta)
}

/*
================
Service.serveSeries
================
*/
func (s *Service) serveSeries(w http.ResponseWriter, db *sql.DB, metric string, win window) {
	var points []Point
	switch {
	case metric == "gold_total":
		var err error
		if points, err = s.goldTotals(win); err != nil {
			s.fail(w, err)
			return
		}
	case ledgerMetric(metric):
		groups, err := readGroups(db, win)
		if err != nil {
			s.fail(w, err)
			return
		}
		points = foldSeries(metric, groups)
	default:
		writeError(w, http.StatusBadRequest, queryError{"bad_metric", "unknown metric"})
		return
	}
	writeJSON(w, map[string]any{"metric": metric, "bucket": win.bucket, "points": points})
}

/*
================
Service.goldTotals

The gold held at each snapshot, the newest of each bucket; one division,
or the whole-shard row (division "", which includes account warehouses).
================
*/
func (s *Service) goldTotals(win window) ([]Point, error) {
	query := `SELECT (at_s / ?) * ? AS t, at_s, total FROM gold_totals WHERE at_s >= ? AND at_s <= ? AND division = ? ORDER BY at_s`
	rows, err := s.history.Query(query, win.size, win.size, win.from, win.to, win.division)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []Point{}
	for rows.Next() {
		var t, at, total int64
		if err := rows.Scan(&t, &at, &total); err != nil {
			return nil, err
		}
		point := Point{T: t, Sum: decimal(total), Min: decimal(total), Max: decimal(total), Count: 1}
		if n := len(points); n > 0 && points[n-1].T == t {
			points[n-1] = point
			continue
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

/*
================
Service.fail

A snapshot read that failed: no_snapshot when the copy is gone, else a
logged 500 that names no internals.
================
*/
func (s *Service) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, queryError{"no_snapshot", "no snapshot has been taken yet"})
		return
	}
	log.WithError(err).Warn("economy: aggregate read failed")
	writeError(w, http.StatusInternalServerError, queryError{"internal", "the aggregates could not be read"})
}

/*
================
writeError
================
*/
func writeError(w http.ResponseWriter, status int, err error) {
	var q queryError
	if !errors.As(err, &q) {
		q = queryError{"internal", "the aggregates could not be read"}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": q.code, "message": q.message}})
}

/*
================
writeJSON
================
*/
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
