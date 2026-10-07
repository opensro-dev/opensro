/*
===========================================================================

service.go - the bug report service owned by the Agent

One Service exists per Agent when the feature is enabled (nil otherwise).
It owns the pacing state, the bound on concurrent uploads (each holds a
whole attachment in memory) and the delivery to Discord and/or a directory.

Request order, driven by the Agent's HTTP handler:

	Admit (pacing + upload slot) -> ReadSubmission -> Deliver -> Ticket.Done

===========================================================================
*/
package bugreport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	maxConcurrentUploads = 2
	busyRetry            = 30 * time.Second
	deliveryTimeout      = 90 * time.Second
)

var (
	// ErrBusy means the Agent or Discord cannot take a report right now.
	ErrBusy = errors.New("bug reports are busy")
	// ErrDelivery means Discord refused or never answered, or the directory
	// could not be written.
	ErrDelivery = errors.New("bug report delivery failed")
)

/*
================
Report

A submission plus the identity the Agent resolved from the session.
================
*/
type Report struct {
	Account   string
	Division  string
	Character string
	Submission
}

/*
================
Refusal

Why Admit said no, and how long the client should wait before retrying.
================
*/
type Refusal struct {
	RateLimited bool
	RetryAfter  time.Duration
}

/*
================
Ticket

An admitted report. Done must be called exactly once.
================
*/
type Ticket struct {
	service *Service
	account string
}

/*
================
Service
================
*/
type Service struct {
	config  Config
	client  *http.Client
	now     func() time.Time
	limiter *limiter
	uploads chan struct{}
	// forum is learned from Discord's first refusal: webhooks on forum
	// channels must name the thread each message opens.
	forum atomic.Bool
}

/*
================
New

The config must come from LoadConfig with a sink set. Tests may pass a
loopback webhook; the Discord-host check lives in LoadConfig on purpose.
================
*/
func New(config Config, client *http.Client, now func() time.Time) (*Service, error) {
	if !config.Enabled() {
		return nil, fmt.Errorf("bugreport: a webhook URL or a directory is required")
	}
	if config.MaxBytes <= 0 {
		return nil, fmt.Errorf("bugreport: max bytes must be positive")
	}
	if client == nil {
		client = &http.Client{Timeout: deliveryTimeout}
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		config:  config,
		client:  client,
		now:     now,
		limiter: newLimiter(now),
		uploads: make(chan struct{}, maxConcurrentUploads),
	}, nil
}

/*
================
Settings
================
*/
func (service *Service) Settings() Settings {
	return Settings{
		Enabled:       true,
		ReplayDefault: service.config.ReplayDefault,
		MaxBytes:      service.config.MaxBytes,
		ReplaySeconds: ReplaySeconds,
		// The part shares the request bound with the clip (submission.go).
		MaxDiagnosticsBytes: min(service.config.MaxBytes, maxDiagnosticsBytes),
	}
}

/*
================
MaxBytes
================
*/
func (service *Service) MaxBytes() int64 {
	return service.config.MaxBytes
}

/*
================
Admit

Checked before the body is read, so a refused player does not upload a
video for nothing.
================
*/
func (service *Service) Admit(account string) (*Ticket, *Refusal) {
	allowed, wait := service.limiter.begin(account)
	if !allowed {
		return nil, &Refusal{RateLimited: true, RetryAfter: wait}
	}
	select {
	case service.uploads <- struct{}{}:
		return &Ticket{service: service, account: account}, nil
	default:
		service.limiter.finish(account, false)
		return nil, &Refusal{RateLimited: false, RetryAfter: busyRetry}
	}
}

/*
================
Ticket.Done
================
*/
func (ticket *Ticket) Done(delivered bool) {
	<-ticket.service.uploads
	ticket.service.limiter.finish(ticket.account, delivered)
}

/*
================
Deliver

Writes the report to the directory, then posts it, and returns the Discord
message id, or the directory entry when there is no webhook. The directory
goes first: it is local and cannot be slow, and a report that reached disk
is not lost if Discord then fails.
================
*/
func (service *Service) Deliver(ctx context.Context, report Report) (string, error) {
	var entry string
	if service.config.Directory != "" {
		written, err := writeDirectory(service.config.Directory, report, service.now())
		if err != nil {
			return "", err
		}
		entry = written
	}
	if service.config.WebhookURL == "" {
		return entry, nil
	}
	ctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	id, err := service.post(ctx, report, service.forum.Load())
	if errors.Is(err, errForumChannel) {
		service.forum.Store(true)
		id, err = service.post(ctx, report, true)
	}
	return id, err
}
