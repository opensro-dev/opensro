/*
===========================================================================

directory_test.go - the local directory sink

A report written to disk carries what Discord would have received, appears
atomically under a server-chosen name, and never grows the directory past
its bound.

===========================================================================
*/
package bugreport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var directoryTestNow = time.Date(2026, 10, 7, 2, 40, 1, 500_000_000, time.UTC)

/*
================
directoryTestReport
================
*/
func directoryTestReport(id string) Report {
	return Report{
		Account:   "probe",
		Division:  "GlobalOfficial",
		Character: "TurnProbe",
		Submission: Submission{
			Description: "Ghost Walk stops before sliding",
			Context:     []Field{{Name: reportIDField, Value: id}, {Name: "Phase", Value: "world"}},
			Errors:      []string{"cast rejected: 5"},
			Attachment:  &Attachment{FileName: "replay.mp4", ContentType: "video/mp4", Data: []byte("clip")},
			Diagnostics: &Attachment{FileName: "diagnostics.zip", ContentType: "application/zip", Data: testZip},
		},
	}
}

/*
================
listReports

The report directories under root, without staging entries.
================
*/
func listReports(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), stagingPrefix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

/*
================
TestDeliverWritesDirectoryWithoutWebhook

A directory alone enables the service; Deliver returns the entry name and
the entry holds report.json and both files byte for byte.
================
*/
func TestDeliverWritesDirectoryWithoutWebhook(t *testing.T) {
	root := filepath.Join(t.TempDir(), "reports")
	service, err := New(Config{Directory: root, MaxBytes: DefaultMaxBytes}, nil, func() time.Time { return directoryTestNow })
	if err != nil {
		t.Fatal(err)
	}
	if settings := service.Settings(); !settings.Enabled ||
		!reflect.DeepEqual(settings.Destinations, []string{DestinationDirectory}) {
		t.Fatalf("a directory alone must enable reports: %+v", settings)
	}
	report := directoryTestReport("BR-261007-0240-ABCD")
	entry, err := service.Deliver(context.Background(), report)
	if err != nil {
		t.Fatal(err)
	}
	if want := "20261007T024001.500Z-BR-261007-0240-ABCD"; entry != want {
		t.Fatalf("entry %q, want %q", entry, want)
	}
	if names := listReports(t, root); !reflect.DeepEqual(names, []string{entry}) {
		t.Fatalf("directory holds %v", names)
	}
	encoded, err := os.ReadFile(filepath.Join(root, entry, directoryReportFile))
	if err != nil {
		t.Fatal(err)
	}
	var document directoryReport
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.Account != "probe" || document.Server != "GlobalOfficial" || document.Character != "TurnProbe" ||
		document.Description != report.Description || !reflect.DeepEqual(document.Context, report.Context) ||
		!reflect.DeepEqual(document.Errors, report.Errors) ||
		!reflect.DeepEqual(document.Files, []string{"replay.mp4", "diagnostics.zip"}) {
		t.Fatalf("report.json %+v", document)
	}
	for _, attachment := range []*Attachment{report.Attachment, report.Diagnostics} {
		data, err := os.ReadFile(filepath.Join(root, entry, attachment.FileName))
		if err != nil || string(data) != string(attachment.Data) {
			t.Fatalf("%s: %v", attachment.FileName, err)
		}
	}
}

/*
================
TestDirectoryIgnoresUnexpectedReportID

A client id that is not the BR-... shape never becomes part of a path.
================
*/
func TestDirectoryIgnoresUnexpectedReportID(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"../escape", "BR-1/../../x", `BR-1\x`, "br-lowercase", ""} {
		entry, err := writeDirectory(root, directoryTestReport(id), directoryTestNow)
		if err != nil {
			t.Fatal(err)
		}
		if entry != "20261007T024001.500Z" {
			t.Fatalf("id %q named entry %q", id, entry)
		}
		if err := os.RemoveAll(filepath.Join(root, entry)); err != nil {
			t.Fatal(err)
		}
	}
	if names := listReports(t, root); len(names) != 0 {
		t.Fatalf("unexpected entries %v", names)
	}
}

/*
================
TestDirectoryKeepsNewestReports

Writing past the bound removes the oldest reports first, and a staging
directory a crash left behind is removed once it is stale.
================
*/
func TestDirectoryKeepsNewestReports(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, stagingPrefix+"crashed")
	if err := os.Mkdir(stale, directoryMode); err != nil {
		t.Fatal(err)
	}
	old := directoryTestNow.Add(-2 * staleStaging)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	var written []string
	for index := 0; index < maxDirectoryReports+3; index++ {
		now := directoryTestNow.Add(time.Duration(index) * time.Second)
		entry, err := writeDirectory(root, directoryTestReport(fmt.Sprintf("BR-%03d", index)), now)
		if err != nil {
			t.Fatal(err)
		}
		written = append(written, entry)
	}
	names := listReports(t, root)
	if !reflect.DeepEqual(names, written[3:]) {
		t.Fatalf("kept %d reports from %q, want the newest %d from %q",
			len(names), names[0], maxDirectoryReports, written[3])
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale staging directory survived: %v", err)
	}
}

/*
================
TestLoadConfigDirectory

Only an absolute directory is accepted; a relative one would follow the
allocation's working directory and vanish with it.
================
*/
func TestLoadConfigDirectory(t *testing.T) {
	absolute := t.TempDir()
	config, warnings := LoadConfig(func(name string) string {
		if name == EnvDirectory {
			return " " + absolute + " "
		}
		return ""
	})
	if config.Directory != filepath.Clean(absolute) || !config.Enabled() || len(warnings) != 0 {
		t.Fatalf("absolute: %+v %v", config, warnings)
	}
	config, warnings = LoadConfig(func(name string) string {
		if name == EnvDirectory {
			return "reports"
		}
		return ""
	})
	if config.Directory != "" || config.Enabled() || len(warnings) != 1 {
		t.Fatalf("relative: %+v %v", config, warnings)
	}
}

/*
================
TestDirectoryWriteFailureLeavesNothing

A write that fails part way (a full disk, here an attachment that cannot be
created) returns ErrDelivery, which the Agent answers as DELIVERY_FAILED,
and removes its staging directory at once instead of leaving it for the
stale sweep.
================
*/
func TestDirectoryWriteFailureLeavesNothing(t *testing.T) {
	root := t.TempDir()
	report := directoryTestReport("BR-261007-0240-ABCD")
	report.Diagnostics = &Attachment{FileName: filepath.Join("missing", "diagnostics.zip"), Data: testZip}
	if _, err := writeDirectory(root, report, directoryTestNow); !errors.Is(err, ErrDelivery) {
		t.Fatalf("want ErrDelivery, got %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a failed write left %d entries, first %q", len(entries), entries[0].Name())
	}
}
