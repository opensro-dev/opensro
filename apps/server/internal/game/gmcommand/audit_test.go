/*
===========================================================================

audit_test.go - refused GM commands are reported, rate-limited and bounded

===========================================================================
*/
package gmcommand

import (
	"fmt"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

/*
================
recordingAudit
================
*/
func recordingAudit() (*privilegeAudit, *[]log.Fields) {
	reports := []log.Fields{}
	audit := newPrivilegeAudit()
	audit.report = func(fields log.Fields) { reports = append(reports, fields) }
	return audit, &reports
}

func TestPrivilegeAuditReportsOncePerIntervalAndCarriesTheCount(t *testing.T) {
	audit, reports := recordingAudit()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range 50 {
		audit.deny("global-official", "EfsaneSro", 7, 12, start.Add(time.Duration(i)*time.Second))
	}
	if len(*reports) != 1 {
		t.Fatalf("reports within one interval = %d, want 1", len(*reports))
	}
	audit.deny("global-official", "EfsaneSro", 7, 12, start.Add(privilegeAuditInterval+time.Second))
	if len(*reports) != 2 || (*reports)[1]["suppressed"] != 49 {
		t.Fatalf("second report = %v, want suppressed 49", *reports)
	}
	if (*reports)[0]["character"] != "EfsaneSro" || (*reports)[0]["division"] != "global-official" {
		t.Fatalf("report fields = %v", (*reports)[0])
	}
}

func TestPrivilegeAuditLimitsEachCharacterSeparately(t *testing.T) {
	audit, reports := recordingAudit()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	audit.deny("global-official", "a", 1, 1, now)
	audit.deny("global-official", "b", 2, 1, now)
	audit.deny("other-division", "a", 3, 1, now)
	if len(*reports) != 3 {
		t.Fatalf("reports = %d, want one per division:character", len(*reports))
	}
}

func TestPrivilegeAuditTableStaysBounded(t *testing.T) {
	audit, _ := recordingAudit()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range privilegeAuditMaxEntries * 3 {
		audit.deny("global-official", fmt.Sprintf("c%d", i), 1, 1, now)
	}
	if len(audit.entries) > privilegeAuditMaxEntries {
		t.Fatalf("audit table grew to %d entries, limit %d", len(audit.entries), privilegeAuditMaxEntries)
	}
}
