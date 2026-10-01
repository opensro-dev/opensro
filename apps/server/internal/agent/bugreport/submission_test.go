/*
===========================================================================

submission_test.go - reading reports from the browser

===========================================================================
*/
package bugreport

import (
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
