/*
===========================================================================

submission_test.go - reading reports from the browser

===========================================================================
*/
package bugreport

import (
	"archive/zip"
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

const testMaxBytes = 1 << 20

// testMP4 is the smallest prefix isMP4 accepts: a box size and "ftyp".
var testMP4 = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 64)...)

/*
================
testPart
================
*/
type testPart struct {
	name        string
	contentType string
	data        []byte
}

/*
================
multipartRequest
================
*/
func multipartRequest(t *testing.T, parts ...testPart) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		header := textproto.MIMEHeader{}
		disposition := `form-data; name="` + part.name + `"`
		if part.contentType != "" {
			disposition += `; filename="` + part.name + `"`
			header.Set("Content-Type", part.contentType)
		}
		header.Set("Content-Disposition", disposition)
		w, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(part.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/title/bug-report", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

/*
================
TestReadSubmissionAcceptsFullReport
================
*/
func TestReadSubmissionAcceptsFullReport(t *testing.T) {
	request := multipartRequest(t,
		testPart{name: "description", data: []byte("  The bridge eats my character  \r\nevery time ")},
		testPart{name: "meta", data: []byte(`{"context":[{"name":"Build","value":"  abc\n123 "}],"errors":["boom",""," "]}`)},
		testPart{name: "clip", contentType: "video/mp4", data: testMP4},
	)
	submission, err := ReadSubmission(request, testMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if submission.Description != "The bridge eats my character  \nevery time" {
		t.Fatalf("description %q", submission.Description)
	}
	if len(submission.Context) != 1 || submission.Context[0] != (Field{Name: "Build", Value: "abc 123"}) {
		t.Fatalf("context %+v", submission.Context)
	}
	if len(submission.Errors) != 1 || submission.Errors[0] != "boom" {
		t.Fatalf("errors %+v", submission.Errors)
	}
	if submission.Attachment == nil || submission.Attachment.FileName != "replay.mp4" || !bytes.Equal(submission.Attachment.Data, testMP4) {
		t.Fatal("clip not kept")
	}
}

/*
================
TestReadSubmissionRejectsMalformedReports
================
*/
func TestReadSubmissionRejectsMalformedReports(t *testing.T) {
	description := testPart{name: "description", data: []byte("Something broke here")}
	cases := map[string][]testPart{
		"no description":     {{name: "meta", data: []byte(`{}`)}},
		"short description":  {{name: "description", data: []byte("too short")}},
		"long description":   {{name: "description", data: []byte(strings.Repeat("x", maxDescriptionRunes+1))}},
		"unknown part":       {description, {name: "account", data: []byte("admin")}},
		"duplicate part":     {description, description},
		"unknown meta field": {description, {name: "meta", data: []byte(`{"account":"admin"}`)}},
		"clip not mp4":       {description, {name: "clip", contentType: "video/mp4", data: []byte("<html>not a video</html>")}},
		"clip wrong type":    {description, {name: "clip", contentType: "text/html", data: testMP4}},
		"screenshot as mp4":  {description, {name: "screenshot", contentType: "video/mp4", data: testMP4}},
		"two attachments": {
			description,
			{name: "clip", contentType: "video/mp4", data: testMP4},
			{name: "screenshot", contentType: "image/png", data: []byte("\x89PNG\r\n\x1a\n....")},
		},
	}
	for name, parts := range cases {
		_, err := ReadSubmission(multipartRequest(t, parts...), testMaxBytes)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}

/*
================
TestReadSubmissionRejectsOversizedAttachment
================
*/
func TestReadSubmissionRejectsOversizedAttachment(t *testing.T) {
	clip := append(append([]byte{}, testMP4...), make([]byte, testMaxBytes)...)
	request := multipartRequest(t,
		testPart{name: "description", data: []byte("Something broke here")},
		testPart{name: "clip", contentType: "video/mp4", data: clip},
	)
	if _, err := ReadSubmission(request, testMaxBytes); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

/*
================
TestReadSubmissionReportsBodyLimitAsTooLarge
================
*/
func TestReadSubmissionReportsBodyLimitAsTooLarge(t *testing.T) {
	request := multipartRequest(t,
		testPart{name: "description", data: []byte("Something broke here")},
		testPart{name: "clip", contentType: "video/mp4", data: testMP4},
	)
	request.Body = http.MaxBytesReader(httptest.NewRecorder(), request.Body, 64)
	if _, err := ReadSubmission(request, testMaxBytes); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

// testZip is a zip's local file header magic followed by filler.
var testZip = mustZip(map[string]string{"movement.json": "{}"})

/*
================
TestReadSubmissionKeepsDiagnosticsBesideTheClip

The journal archive travels with the replay, each in its own attachment.
================
*/
func TestReadSubmissionKeepsDiagnosticsBesideTheClip(t *testing.T) {
	submission, err := ReadSubmission(multipartRequest(t,
		testPart{name: "description", data: []byte("Ghost Walk stops before sliding")},
		testPart{name: "clip", contentType: "video/mp4", data: testMP4},
		testPart{name: "diagnostics", contentType: "application/zip", data: testZip},
	), testMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if submission.Attachment == nil || submission.Attachment.FileName != "replay.mp4" {
		t.Fatalf("clip lost: %+v", submission.Attachment)
	}
	if submission.Diagnostics == nil || submission.Diagnostics.FileName != "diagnostics.zip" ||
		submission.Diagnostics.ContentType != "application/zip" || !bytes.Equal(submission.Diagnostics.Data, testZip) {
		t.Fatalf("diagnostics not kept: %+v", submission.Diagnostics)
	}
}

/*
================
TestReadSubmissionRefusesBadDiagnostics

Only a declared and genuine zip is forwarded; a second copy is a duplicate.
================
*/
func TestReadSubmissionRefusesBadDiagnostics(t *testing.T) {
	description := testPart{name: "description", data: []byte("Ghost Walk stops before sliding")}
	cases := map[string][]testPart{
		"not a zip":     {description, {name: "diagnostics", contentType: "application/zip", data: testMP4}},
		"wrong type":    {description, {name: "diagnostics", contentType: "application/json", data: testZip}},
		"twice":         {description, {name: "diagnostics", contentType: "application/zip", data: testZip}, {name: "diagnostics", contentType: "application/zip", data: testZip}},
		"untyped bytes": {description, {name: "diagnostics", data: testZip}},
	}
	for name, parts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadSubmission(multipartRequest(t, parts...), testMaxBytes); !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

/*
================
TestReadSubmissionBoundsDiagnosticsSize

The archive has its own ceiling, and it shares the attachment budget with
the clip because both leave in one Discord message.
================
*/
func TestReadSubmissionBoundsDiagnosticsSize(t *testing.T) {
	description := testPart{name: "description", data: []byte("Ghost Walk stops before sliding")}
	huge := append([]byte("PK\x03\x04"), make([]byte, maxDiagnosticsBytes)...)
	if _, err := ReadSubmission(multipartRequest(t, description,
		testPart{name: "diagnostics", contentType: "application/zip", data: huge}), 4*maxDiagnosticsBytes); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("archive over its ceiling: want ErrTooLarge, got %v", err)
	}
	clip := append(append([]byte{}, testMP4...), make([]byte, testMaxBytes-len(testMP4)-len(testZip)+1)...)
	if _, err := ReadSubmission(multipartRequest(t, description,
		testPart{name: "clip", contentType: "video/mp4", data: clip},
		testPart{name: "diagnostics", contentType: "application/zip", data: testZip}), testMaxBytes); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("clip and archive over the shared budget: want ErrTooLarge, got %v", err)
	}
	clip = clip[:len(clip)-1]
	if _, err := ReadSubmission(multipartRequest(t, description,
		testPart{name: "clip", contentType: "video/mp4", data: clip},
		testPart{name: "diagnostics", contentType: "application/zip", data: testZip}), testMaxBytes); err != nil {
		t.Fatalf("exactly the shared budget must pass: %v", err)
	}
}

/*
================
zipOf

A real zip of the given members, for the diagnostics checks.
================
*/
func zipOf(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range members {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

/*
================
TestReadSubmissionChecksDiagnosticsShape

Only the five known journals, flat, each valid JSON, within the unpacked
bound; anything else (an extra file, a path, non-JSON, a zip bomb) is
refused before it can reach Discord.
================
*/
func TestReadSubmissionChecksDiagnosticsShape(t *testing.T) {
	description := testPart{name: "description", data: []byte("Ghost Walk stops before sliding")}
	read := func(data []byte) error {
		_, err := ReadSubmission(multipartRequest(t, description,
			testPart{name: "diagnostics", contentType: "application/zip", data: data}), testMaxBytes)
		return err
	}
	if err := read(zipOf(t, map[string]string{"manifest.json": "{}", "movement.json": `{"events":[]}`, "timeline.json": "[]",
		"state.json": "{}", "environment.json": "{}"})); err != nil {
		t.Fatalf("the five journals: %v", err)
	}
	for name, members := range map[string]map[string]string{
		"extra file":   {"movement.json": "{}", "chat.txt": "hello"},
		"nested path":  {"logs/movement.json": "{}"},
		"parent path":  {"../movement.json": "{}"},
		"not json":     {"movement.json": "not json"},
		"empty member": {"state.json": ""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := read(zipOf(t, members)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
	bomb := zipOf(t, map[string]string{"movement.json": "[" + strings.Repeat("0,", maxDiagnosticsUnpacked/2) + "0]"})
	if err := read(bomb); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a member past the unpacked bound: want ErrTooLarge, got %v (archive %d bytes)", err, len(bomb))
	}
}

/*
================
mustZip

A package-level stand-in archive for tests that only need a valid journal.
================
*/
func mustZip(members map[string]string) []byte {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range members {
		w, err := writer.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			panic(err)
		}
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}
