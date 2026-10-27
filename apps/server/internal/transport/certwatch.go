/*
===========================================================================

certwatch.go - the served WebTransport pair's ops alert (#1447)

Ops only: nothing here reaches a player. With public WebTransport on, the
GameWorld watches the TLS pair it actually serves. It checks at boot and
then on every tick it is given (the GameWorld ticks it every
CertificateCheckInterval). Two conditions page: the pair expires within
CertificateWarnRemaining, or the pair failed at boot so this run serves
WebSocket only. Every check that finds one logs an error; the staff poster
hears about each condition at most once per CertificateAlertRepeat.

The GameWorld reads its explicit pair once, in NewServer, so a renewed
file on disk changes nothing until the next deploy. The watch reads the
served pair, not the file, for exactly that reason.

===========================================================================
*/

package transport

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	CertificateCheckInterval = 6 * time.Hour
	CertificateWarnRemaining = 14 * 24 * time.Hour
	CertificateAlertRepeat   = 24 * time.Hour
)

// The conditions a check can report; each has its own repeat window, so
// a pair that crosses from expiring to expired pages again at once.
const (
	certificateExpiring = "expiring"
	certificateExpired  = "expired"
	certificateDisabled = "disabled"
)

/*
================
CertificateProblem

The condition the served pair is in at now, and the alert text for it;
both empty when there is nothing to report. Only the public posture is
watched: a development certificate is short-lived by design. A pair past
its notAfter is its own condition (#1868): browsers already refuse it,
so WebTransport sessions fail TLS and players are on WebSocket now.
================
*/
func (s *Server) CertificateProblem(now time.Time) (condition, text string) {
	if !s.cfg.WebTransportPublic {
		return "", ""
	}
	if s.wtDisabled != "" {
		return certificateDisabled, "WebTransport is disabled this run, players are on WebSocket only: " +
			s.wtDisabled + ". Fix the TLS pair and redeploy."
	}
	if s.Cert == nil || s.Cert.Leaf == nil {
		return "", ""
	}
	notAfter := s.Cert.Leaf.NotAfter.UTC().Format(time.RFC3339)
	remaining := s.Cert.Leaf.NotAfter.Sub(now)
	if remaining <= 0 {
		return certificateExpired, "the served WebTransport certificate expired at " + notAfter +
			": WebTransport sessions fail TLS and players are on WebSocket. Renew the pair and redeploy now; " +
			"the GameWorld reads it only at boot."
	}
	if remaining >= CertificateWarnRemaining {
		return "", ""
	}
	left := "in under a day"
	if days := int(remaining / (24 * time.Hour)); days > 0 {
		left = fmt.Sprintf("in %d days", days)
	}
	return certificateExpiring, "the served WebTransport certificate expires " + left + " (" + notAfter +
		"). Renew the pair and redeploy before then; the GameWorld reads it only at boot."
}

/*
================
CertificateWatch

The repeat bookkeeping between checks. post is the staff poster; nil, or
one that fails, leaves the error log as the only signal.
================
*/
type CertificateWatch struct {
	server *Server
	label  string
	now    func() time.Time
	post   func(text string) error
	posted map[string]time.Time
}

/*
================
NewCertificateWatch
================
*/
func NewCertificateWatch(server *Server, label string, now func() time.Time, post func(string) error) *CertificateWatch {
	return &CertificateWatch{server: server, label: label, now: now, post: post, posted: map[string]time.Time{}}
}

/*
================
Check

One look at the served pair: log the problem if any, and post it when its
condition has not been posted within CertificateAlertRepeat. A failed post
is retried on the next check rather than counted as delivered.
================
*/
func (w *CertificateWatch) Check() {
	now := w.now()
	condition, text := w.server.CertificateProblem(now)
	if condition == "" {
		return
	}
	message := "OpenSRO " + w.label + ": " + text
	log.WithField("condition", condition).Error("transport certificate: " + text)
	if w.post == nil {
		return
	}
	if last, seen := w.posted[condition]; seen && now.Sub(last) < CertificateAlertRepeat {
		return
	}
	if err := w.post(message); err != nil {
		log.WithError(err).Warn("transport certificate: staff alert not delivered")
		return
	}
	w.posted[condition] = now
}

/*
================
Run

Checks at once, then on every tick until ctx ends. The caller owns
the cadence (a CertificateCheckInterval ticker in the GameWorld).
================
*/
func (w *CertificateWatch) Run(ctx context.Context, ticks <-chan time.Time) {
	w.Check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			w.Check()
		}
	}
}
