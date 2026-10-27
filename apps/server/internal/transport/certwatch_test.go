/*
===========================================================================

certwatch_test.go - the served pair's ops alert, on a fake clock (#1447)

The watch logs every problem it sees, posts each condition at most once a
day, retries a failed post, and watches only the public posture. The
clock and the poster are injected; nothing sleeps.

===========================================================================
*/

package transport

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"testing"
	"time"
)

var certWatchEpoch = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

/*
================
certWatchFixture

A public-posture server whose served pair expires at notAfter, a clock
the test moves, and a poster that records (or refuses) every post.
================
*/
type certWatchFixture struct {
	server *Server
	now    time.Time
	posts  []string
	refuse bool
	watch  *CertificateWatch
}

func newCertWatchFixture(public bool, notAfter time.Time) *certWatchFixture {
	f := &certWatchFixture{now: certWatchEpoch}
	f.server = &Server{
		cfg:  Config{WebTransportPublic: public},
		Cert: &Certificate{Leaf: &x509.Certificate{NotAfter: notAfter}},
	}
	f.watch = NewCertificateWatch(f.server, "global-official", func() time.Time { return f.now }, func(text string) error {
		if f.refuse {
			return errors.New("webhook refused")
		}
		f.posts = append(f.posts, text)
		return nil
	})
	return f
}

/*
================
TestCertificateWatchPostsAnExpiringPairOncePerDay
================
*/
func TestCertificateWatchPostsAnExpiringPairOncePerDay(t *testing.T) {
	f := newCertWatchFixture(true, certWatchEpoch.Add(10*24*time.Hour))
	f.watch.Check()
	if len(f.posts) != 1 || !strings.Contains(f.posts[0], "expires in 10 days") ||
		!strings.HasPrefix(f.posts[0], "OpenSRO global-official: ") {
		t.Fatalf("posts = %q, want one alert naming 10 days", f.posts)
	}
	// The 6 h checks inside the day log but do not post again.
	for step := 1; step <= 3; step++ {
		f.now = certWatchEpoch.Add(time.Duration(step) * CertificateCheckInterval)
		f.watch.Check()
	}
	if len(f.posts) != 1 {
		t.Fatalf("posted %d times within a day, want 1", len(f.posts))
	}
	f.now = certWatchEpoch.Add(CertificateAlertRepeat)
	f.watch.Check()
	if len(f.posts) != 2 || !strings.Contains(f.posts[1], "expires in 9 days") {
		t.Fatalf("posts = %q, want the next day's alert at 9 days", f.posts)
	}
}

/*
================
TestCertificateWatchStaysQuietWithTimeLeft
================
*/
func TestCertificateWatchStaysQuietWithTimeLeft(t *testing.T) {
	f := newCertWatchFixture(true, certWatchEpoch.Add(CertificateWarnRemaining))
	f.watch.Check()
	if len(f.posts) != 0 {
		t.Fatalf("posts = %q at exactly the warning margin, want none", f.posts)
	}
	f.now = certWatchEpoch.Add(time.Second)
	f.watch.Check()
	if len(f.posts) != 1 || !strings.Contains(f.posts[0], "expires in 13 days") {
		t.Fatalf("posts = %q one second inside the margin, want the alert", f.posts)
	}
}

/*
================
TestCertificateWatchReportsAnExpiredPairAsZeroDays
================
*/
func TestCertificateWatchReportsAnExpiredPairAsZeroDays(t *testing.T) {
	f := newCertWatchFixture(true, certWatchEpoch.Add(-48*time.Hour))
	f.watch.Check()
	if len(f.posts) != 1 || !strings.Contains(f.posts[0], "expires in 0 days") {
		t.Fatalf("posts = %q, want an expired pair reported as 0 days", f.posts)
	}
}

/*
================
TestCertificateWatchPagesADisabledRun
================
*/
func TestCertificateWatchPagesADisabledRun(t *testing.T) {
	f := newCertWatchFixture(true, time.Time{})
	f.server.Cert = nil
	f.server.wtDisabled = "loading TLS certificate pair: open /etc/opensro-tls/privkey.pem: permission denied"
	f.watch.Check()
	if len(f.posts) != 1 || !strings.Contains(f.posts[0], "WebSocket only") ||
		!strings.Contains(f.posts[0], "permission denied") {
		t.Fatalf("posts = %q, want the disabled-run alert with its reason", f.posts)
	}
	f.now = certWatchEpoch.Add(CertificateCheckInterval)
	f.watch.Check()
	if len(f.posts) != 1 {
		t.Fatalf("posted %d times within a day, want 1", len(f.posts))
	}
}

/*
================
TestCertificateWatchRetriesAFailedPost
================
*/
func TestCertificateWatchRetriesAFailedPost(t *testing.T) {
	f := newCertWatchFixture(true, certWatchEpoch.Add(time.Hour))
	f.refuse = true
	f.watch.Check()
	f.refuse = false
	f.now = certWatchEpoch.Add(CertificateCheckInterval)
	f.watch.Check()
	if len(f.posts) != 1 {
		t.Fatalf("posts = %q, want the refused alert delivered on the next check", f.posts)
	}
}

/*
================
TestCertificateWatchIgnoresTheDevelopmentPosture

A development certificate is short-lived by design: without the public
switch nothing is reported, and a nil poster only ever logs.
================
*/
func TestCertificateWatchIgnoresTheDevelopmentPosture(t *testing.T) {
	f := newCertWatchFixture(false, certWatchEpoch.Add(time.Hour))
	f.watch.Check()
	if len(f.posts) != 0 {
		t.Fatalf("posts = %q without the public switch, want none", f.posts)
	}
	if condition, _ := f.server.CertificateProblem(certWatchEpoch); condition != "" {
		t.Fatalf("condition = %q without the public switch, want none", condition)
	}
	silent := NewCertificateWatch(newCertWatchFixture(true, certWatchEpoch).server, "x", time.Now, nil)
	silent.Check()
}

/*
================
TestCertificateWatchRunChecksAtBootAndOnEachTick
================
*/
func TestCertificateWatchRunChecksAtBootAndOnEachTick(t *testing.T) {
	f := newCertWatchFixture(true, certWatchEpoch.Add(time.Hour))
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.watch.Run(ctx, ticks)
		close(done)
	}()
	// The boot check posts. The tick checks again inside the same day, so
	// it logs without posting. The unbuffered send returns only once Run
	// has taken it, and done orders every post before the read below.
	ticks <- certWatchEpoch
	cancel()
	<-done
	if len(f.posts) != 1 {
		t.Fatalf("posts = %q after boot and one tick inside the day, want 1", f.posts)
	}
}
