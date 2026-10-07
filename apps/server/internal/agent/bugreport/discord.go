/*
===========================================================================

discord.go - posting a report through a Discord webhook

One message per report: an embed with the description and context, the
replay or screenshot as a file Discord plays inline. On a forum channel
each message opens its own post (thread_name).

Two rules matter more than the formatting:

  - allowed_mentions is empty, so nothing a player types can ping
    @everyone, a role or a user.
  - net/http errors embed the request URL, and this URL holds the webhook
    token. Every error leaving this file is rebuilt without it.

Embed limits (Discord): title 256, description 4096, 25 fields, field name
256 and value 1024, 6000 characters across the whole embed.

===========================================================================
*/
package bugreport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	embedColor        = 0xe5534b
	maxEmbedChars     = 5800
	maxFieldValue     = 1024
	maxThreadName     = 90
	maxResponseBytes  = 64 << 10
	errorsFieldName   = "Recent client errors"
	discordForumError = 220001
)

// errForumChannel is Discord's "webhooks posted to forum channels must have
// a thread_name or thread_id" refusal, retried once with a thread name.
var errForumChannel = errors.New("discord: forum channel requires a thread name")

/*
================
discordEmbed
================
*/
type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
}

/*
================
discordField
================
*/
type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

/*
================
discordPayload
================
*/
type discordPayload struct {
	ThreadName      string            `json:"thread_name,omitempty"`
	Embeds          []discordEmbed    `json:"embeds"`
	AllowedMentions map[string]any    `json:"allowed_mentions"`
	Attachments     []discordFileInfo `json:"attachments,omitempty"`
}

/*
================
discordFileInfo
================
*/
type discordFileInfo struct {
	ID       int    `json:"id"`
	FileName string `json:"filename"`
}

/*
================
post
================
*/
func (service *Service) post(ctx context.Context, report Report, forum bool) (string, error) {
	body, contentType, err := encodeMessage(report, forum)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.config.WebhookURL+"?wait=true", body)
	if err != nil {
		return "", fmt.Errorf("%w: building request", ErrDelivery)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := service.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrDelivery, redactedError(err))
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	switch {
	case response.StatusCode == http.StatusOK:
		var message struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(payload, &message) != nil || message.ID == "" {
			return "", fmt.Errorf("%w: Discord answered without a message id", ErrDelivery)
		}
		return message.ID, nil
	case response.StatusCode == http.StatusTooManyRequests:
		return "", fmt.Errorf("%w: Discord rate limit", ErrBusy)
	case response.StatusCode == http.StatusBadRequest && !forum && discordErrorCode(payload) == discordForumError:
		return "", errForumChannel
	}
	return "", fmt.Errorf("%w: Discord answered %d (code %d)", ErrDelivery, response.StatusCode, discordErrorCode(payload))
}

/*
================
encodeMessage
================
*/
func encodeMessage(report Report, forum bool) (io.Reader, string, error) {
	payload := discordPayload{
		Embeds:          []discordEmbed{buildEmbed(report)},
		AllowedMentions: map[string]any{"parse": []string{}},
	}
	if forum {
		payload.ThreadName = threadName(report)
	}
	// Files go out in a fixed order: the clip or screenshot, then the
	// journal archive. Each is files[id] with its id in attachments.
	var files []*Attachment
	for _, file := range []*Attachment{report.Attachment, report.Diagnostics} {
		if file != nil {
			payload.Attachments = append(payload.Attachments, discordFileInfo{ID: len(files), FileName: file.FileName})
			files = append(files, file)
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("%w: encoding payload", ErrDelivery)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="payload_json"`)
	header.Set("Content-Type", "application/json")
	part, err := writer.CreatePart(header)
	if err == nil {
		_, err = part.Write(encoded)
	}
	for id, file := range files {
		if err != nil {
			break
		}
		header = textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files[%d]"; filename=%q`, id, file.FileName))
		header.Set("Content-Type", file.ContentType)
		part, err = writer.CreatePart(header)
		if err == nil {
			_, err = part.Write(file.Data)
		}
	}
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		return nil, "", fmt.Errorf("%w: encoding body", ErrDelivery)
	}
	return &body, writer.FormDataContentType(), nil
}

/*
================
buildEmbed

Identity fields first (from the session, trusted), then client context,
then as many recent errors as the embed budget leaves room for.
================
*/
func buildEmbed(report Report) discordEmbed {
	title := "Bug report"
	if report.Character != "" {
		title += " — " + report.Character
	}
	embed := discordEmbed{
		Title:       truncateRunes(title, 256),
		Description: report.Description,
		Color:       embedColor,
	}
	used := utf8.RuneCountInString(embed.Title) + utf8.RuneCountInString(embed.Description)
	add := func(name, value string) {
		if value == "" {
			return
		}
		size := utf8.RuneCountInString(name) + utf8.RuneCountInString(value)
		if used+size > maxEmbedChars {
			return
		}
		used += size
		embed.Fields = append(embed.Fields, discordField{Name: name, Value: value, Inline: true})
	}
	add("Account", report.Account)
	add("Server", report.Division)
	add("Character", report.Character)
	for _, field := range report.Context {
		add(field.Name, field.Value)
	}
	if len(report.Errors) > 0 {
		budget := min(maxFieldValue, maxEmbedChars-used-utf8.RuneCountInString(errorsFieldName))
		if value := errorsBlock(report.Errors, budget); value != "" {
			embed.Fields = append(embed.Fields, discordField{Name: errorsFieldName, Value: value, Inline: false})
		}
	}
	return embed
}

/*
================
errorsBlock

Newest errors are the likeliest cause, so the block keeps the tail. Backticks
are neutralized so a message cannot close the code block early.
================
*/
func errorsBlock(lines []string, budget int) string {
	const fence = "```"
	room := budget - 2*len(fence) - 2
	if room <= 0 {
		return ""
	}
	var kept []string
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.ReplaceAll(lines[index], "`", "'")
		size := utf8.RuneCountInString(line) + 1
		if size > room {
			break
		}
		room -= size
		kept = append([]string{line}, kept...)
	}
	if len(kept) == 0 {
		return ""
	}
	return fence + "\n" + strings.Join(kept, "\n") + "\n" + fence
}

/*
================
threadName
================
*/
func threadName(report Report) string {
	line, _, _ := strings.Cut(report.Description, "\n")
	name := truncateRunes(strings.TrimSpace(line), maxThreadName)
	if report.Character != "" {
		name = truncateRunes(report.Character+": "+name, maxThreadName)
	}
	return name
}

/*
================
discordErrorCode
================
*/
func discordErrorCode(payload []byte) int {
	var body struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(payload, &body)
	return body.Code
}

/*
================
redactedError

*url.Error prints "Post \"<url>\": <cause>"; keep only the cause.
================
*/
func redactedError(err error) string {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		if urlError.Timeout() {
			return "timed out"
		}
		return urlError.Err.Error()
	}
	return "transport error"
}
