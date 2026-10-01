/*
===========================================================================

submission.go - reading one bug report from the browser

A report arrives as multipart/form-data with up to three parts:

	description  the player's text (required)
	meta         JSON {"context":[{"name","value"}],"errors":[...]}
	clip         video/mp4 replay, or
	screenshot   image/jpeg or image/png when there is no replay

Everything here is player input that ends up in a Discord channel, so each
part has a hard size bound and the attachment must carry the magic bytes of
its declared type. Identity (account, division, character) is never read
from the body: the Agent adds it from the authenticated session.

===========================================================================
*/
package bugreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode/utf8"
)

const (
	minDescriptionRunes = 10
	maxDescriptionRunes = 2000
	maxDescriptionBytes = 4 * maxDescriptionRunes
	maxMetaBytes        = 16 << 10
	maxContextFields    = 16
	maxFieldNameRunes   = 40
	maxFieldValueRunes  = 200
	maxErrorLines       = 50
	maxErrorRunes       = 500
	maxScreenshotBytes  = 4 << 20

	// multipartOverhead bounds the boundaries and part headers on top of the
	// attachment and text parts, so the whole body can be capped up front.
	multipartOverhead = 64 << 10
)

var (
	// ErrInvalid is any malformed or out-of-bounds submission.
	ErrInvalid = errors.New("invalid bug report")
	// ErrTooLarge is an attachment (or body) over the configured cap.
	ErrTooLarge = errors.New("bug report too large")
)

/*
================
Field

One name/value pair of client context (build, browser, location...).
================
*/
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

/*
================
Attachment
================
*/
type Attachment struct {
	FileName    string
	ContentType string
	Data        []byte
}

/*
================
Submission

The player-supplied half of a report.
================
*/
type Submission struct {
	Description string
	Context     []Field
	Errors      []string
	Attachment  *Attachment
}

/*
================
BodyLimit

The largest request body a submission can legitimately have.
================
*/
func BodyLimit(maxBytes int64) int64 {
	return maxBytes + maxDescriptionBytes + maxMetaBytes + multipartOverhead
}

/*
================
ReadSubmission

Parses the multipart body. The caller has already wrapped r.Body in
http.MaxBytesReader with BodyLimit, so an oversized body surfaces here as
ErrTooLarge instead of being read to the end.
================
*/
func ReadSubmission(r *http.Request, maxBytes int64) (Submission, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return Submission{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var submission Submission
	seen := make(map[string]bool)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Submission{}, classifyReadError(err)
		}
		name := part.FormName()
		if seen[name] {
			return Submission{}, fmt.Errorf("%w: duplicate part %q", ErrInvalid, name)
		}
		seen[name] = true
		switch name {
		case "description":
			err = readDescription(part, &submission)
		case "meta":
			err = readMeta(part, &submission)
		case "clip", "screenshot":
			if submission.Attachment != nil {
				return Submission{}, fmt.Errorf("%w: more than one attachment", ErrInvalid)
			}
			submission.Attachment, err = readAttachment(part, name, maxBytes)
		default:
			err = fmt.Errorf("%w: unknown part %q", ErrInvalid, name)
		}
		if err != nil {
			return Submission{}, err
		}
	}
	if !seen["description"] {
		return Submission{}, fmt.Errorf("%w: description is required", ErrInvalid)
	}
	return submission, nil
}

/*
================
readDescription
================
*/
func readDescription(part *multipart.Part, submission *Submission) error {
	data, err := readBounded(part, maxDescriptionBytes)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
	count := utf8.RuneCountInString(text)
	if !utf8.ValidString(text) || count < minDescriptionRunes || count > maxDescriptionRunes {
		return fmt.Errorf("%w: description must be %d-%d characters", ErrInvalid, minDescriptionRunes, maxDescriptionRunes)
	}
	submission.Description = text
	return nil
}

/*
================
readMeta
================
*/
func readMeta(part *multipart.Part, submission *Submission) error {
	data, err := readBounded(part, maxMetaBytes)
	if err != nil {
		return err
	}
	var meta struct {
		Context []Field  `json:"context"`
		Errors  []string `json:"errors"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&meta); err != nil {
		return fmt.Errorf("%w: meta: %v", ErrInvalid, err)
	}
	if len(meta.Context) > maxContextFields || len(meta.Errors) > maxErrorLines {
		return fmt.Errorf("%w: meta has too many entries", ErrInvalid)
	}
	for _, field := range meta.Context {
		name := singleLine(field.Name)
		value := singleLine(field.Value)
		if name == "" || utf8.RuneCountInString(name) > maxFieldNameRunes ||
			utf8.RuneCountInString(value) > maxFieldValueRunes {
			return fmt.Errorf("%w: meta context field out of bounds", ErrInvalid)
		}
		submission.Context = append(submission.Context, Field{Name: name, Value: value})
	}
	for _, line := range meta.Errors {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		submission.Errors = append(submission.Errors, truncateRunes(line, maxErrorRunes))
	}
	return nil
}

/*
================
readAttachment

The declared type must match the file's magic bytes: Discord renders what
it receives, so a mislabeled upload is refused rather than forwarded.
================
*/
func readAttachment(part *multipart.Part, name string, maxBytes int64) (*Attachment, error) {
	limit := maxBytes
	if name == "screenshot" {
		limit = min(maxBytes, maxScreenshotBytes)
	}
	data, err := readBounded(part, limit)
	if err != nil {
		return nil, err
	}
	contentType := part.Header.Get("Content-Type")
	var fileName string
	switch {
	case name == "clip" && contentType == "video/mp4" && isMP4(data):
		fileName = "replay.mp4"
	case name == "screenshot" && contentType == "image/jpeg" && bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		fileName = "screenshot.jpg"
	case name == "screenshot" && contentType == "image/png" && bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		fileName = "screenshot.png"
	default:
		return nil, fmt.Errorf("%w: %s is not a valid %s", ErrInvalid, name, contentType)
	}
	return &Attachment{FileName: fileName, ContentType: contentType, Data: data}, nil
}

/*
================
isMP4

An ISO BMFF file opens with a box whose type is "ftyp".
================
*/
func isMP4(data []byte) bool {
	return len(data) >= 12 && string(data[4:8]) == "ftyp"
}

/*
================
readBounded

Reads at most limit bytes; one byte more means the part is too large.
================
*/
func readBounded(part *multipart.Part, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err != nil {
		return nil, classifyReadError(err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: part %q exceeds %d bytes", ErrTooLarge, part.FormName(), limit)
	}
	return data, nil
}

/*
================
classifyReadError
================
*/
func classifyReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("%w: body exceeds %d bytes", ErrTooLarge, tooLarge.Limit)
	}
	return fmt.Errorf("%w: %v", ErrInvalid, err)
}

/*
================
singleLine
================
*/
func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

/*
================
truncateRunes
================
*/
func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}
