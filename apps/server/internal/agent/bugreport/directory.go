/*
===========================================================================

directory.go - bug report delivery into a local directory

The second delivery sink beside the Discord webhook. A deployment without a
Discord channel (a developer's localhost, a self-hosted server) still keeps
every accepted report: each one becomes its own directory holding
report.json and the files the player sent, exactly what the webhook would
have posted.

A report is written under a temporary name and renamed into place, so a
reader listing the directory never sees half a report. Names come from the
server clock and a validated report id; nothing the client sends becomes a
path except an id that matches the client's own BR-... shape.

Storage is bounded: before each write the oldest reports are pruned so at
most maxDirectoryReports remain. Each report is at most MaxBytes plus its
text, so the default MaxBytes (10 MiB) bounds the directory near 1 GiB.

===========================================================================
*/
package bugreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// directoryReportFile holds everything but the attachments.
	directoryReportFile = "report.json"
	// directoryStampLayout sorts reports by arrival in a plain listing.
	directoryStampLayout = "20060102T150405.000Z"
	// reportIDField is the context field the client fills first
	// (bug-report.ts: "Report ID").
	reportIDField = "Report ID"
	directoryMode = 0o750
	fileMode      = 0o640
	// maxDirectoryReports bounds the directory, oldest first out.
	maxDirectoryReports = 100
	// stagingPrefix names reports still being written; one left by a crash
	// is removed once it is older than staleStaging.
	stagingPrefix = ".incoming-"
	staleStaging  = time.Hour
)

// The client's report id: "BR-" plus date, time and a random suffix.
var reportIDPattern = regexp.MustCompile(`^BR-[0-9A-Z-]{1,40}$`)

/*
================
directoryReport

The report.json layout: the session identity the Agent resolved, then what
the client sent, then the names of the files stored beside it.
================
*/
type directoryReport struct {
	ReceivedAt  string   `json:"receivedAt"`
	Account     string   `json:"account"`
	Server      string   `json:"server"`
	Character   string   `json:"character"`
	Description string   `json:"description"`
	Context     []Field  `json:"context"`
	Errors      []string `json:"errors"`
	Files       []string `json:"files"`
}

/*
================
reportID

The client's id when it has the expected shape, else empty. It only names
the directory, so an unexpected value is dropped rather than refused.
================
*/
func reportID(report Report) string {
	for _, field := range report.Context {
		if field.Name == reportIDField && reportIDPattern.MatchString(field.Value) {
			return field.Value
		}
	}
	return ""
}

/*
================
writeDirectory

Writes one report and returns its directory name, which stands in for the
Discord message id in the Agent's reply and log.
================
*/
func writeDirectory(root string, report Report, now time.Time) (string, error) {
	name := now.UTC().Format(directoryStampLayout)
	if id := reportID(report); id != "" {
		name += "-" + id
	}
	if err := os.MkdirAll(root, directoryMode); err != nil {
		return "", fmt.Errorf("%w: %v", ErrDelivery, err)
	}
	if err := pruneDirectory(root, maxDirectoryReports-1, now); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(root, stagingPrefix)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDelivery, err)
	}
	if err := fillDirectory(staging, report, now); err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if err := os.Rename(staging, filepath.Join(root, name)); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("%w: %v", ErrDelivery, err)
	}
	return name, nil
}

/*
================
fillDirectory

The attachment names are fixed by ReadSubmission (replay.mp4,
screenshot.png/jpg, diagnostics.zip), never taken from the client.
================
*/
func fillDirectory(directory string, report Report, now time.Time) error {
	document := directoryReport{
		ReceivedAt:  now.UTC().Format(time.RFC3339Nano),
		Account:     report.Account,
		Server:      report.Division,
		Character:   report.Character,
		Description: report.Description,
		// Empty lists stay lists: a reader of report.json never meets null.
		Context: append([]Field{}, report.Context...),
		Errors:  append([]string{}, report.Errors...),
		Files:   []string{},
	}
	for _, attachment := range []*Attachment{report.Attachment, report.Diagnostics} {
		if attachment == nil {
			continue
		}
		path := filepath.Join(directory, attachment.FileName)
		if err := os.WriteFile(path, attachment.Data, fileMode); err != nil {
			return fmt.Errorf("%w: %v", ErrDelivery, err)
		}
		document.Files = append(document.Files, attachment.FileName)
	}
	encoded, err := json.MarshalIndent(document, "", "\t")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDelivery, err)
	}
	if err := os.WriteFile(filepath.Join(directory, directoryReportFile), append(encoded, '\n'), fileMode); err != nil {
		return fmt.Errorf("%w: %v", ErrDelivery, err)
	}
	return nil
}

/*
================
pruneDirectory

Removes the oldest reports until at most keep remain, and staging
directories a crash left behind. Report names start with their UTC stamp,
so name order is arrival order.
================
*/
func pruneDirectory(root string, keep int, now time.Time) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDelivery, err)
	}
	var reports []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), stagingPrefix) {
			info, err := entry.Info()
			if err == nil && now.Sub(info.ModTime()) > staleStaging {
				_ = os.RemoveAll(filepath.Join(root, entry.Name()))
			}
			continue
		}
		reports = append(reports, entry.Name())
	}
	sort.Strings(reports)
	for len(reports) > keep {
		if err := os.RemoveAll(filepath.Join(root, reports[0])); err != nil {
			return fmt.Errorf("%w: %v", ErrDelivery, err)
		}
		reports = reports[1:]
	}
	return nil
}
