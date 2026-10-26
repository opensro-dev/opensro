/*
===========================================================================

main.go - sro-wtprobe: transport smoke and protocol measurements

Health evidence for the WebTransport path that shallow endpoints cannot
give: fetching /transport/cert-hash proves the TCP edge answers, but only
a completed QUIC handshake plus a HELLO answered by WELCOME proves the
UDP listener, the certificate it serves, and transport admission all work
together. One code path serves local, LAN and cloud targets: the probe
verifies the certificate served on UDP normally, then additionally
requires it to be the certificate whose hash the TCP endpoint
advertises. A development endpoint's self-signed certificate joins the
trust roots through -ca-cert instead of weakening verification.

The probe never logs in. The caller supplies a fresh one-use admission
ticket (minted for the release probe account through the Agent, exactly
like the browser smoke) in an environment variable; the ticket is
consumed only when the server writes WELCOME, so a probe that fails
before that leaves it usable for the WebSocket fallback.

Usage:
	sro-wtprobe -url https://play.example.net [-wt-addr host:port]

Exit status: 0 after WELCOME, 1 with the failed stage named, 2 on usage.
With -measure, the selected transport runs a ping train and paced burst
and emits one JSON arm, including incomplete observations on failure.
The default remains the original WebTransport WELCOME smoke.

===========================================================================
*/

package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"opensro.online/server/internal/transport"
)

const (
	defaultTimeout   = 20 * time.Second
	defaultTicketEnv = "SRO_WT_PROBE_TICKET"
	certHashBodyCap  = 4096
)

//============================================================================

/*
================
certHashDoc

The subset of the served certificate document the probe pins against.
================
*/
type certHashDoc struct {
	SHA256Hex string `json:"sha256Hex"`
	NotAfter  string `json:"notAfter"`
}

/*
================
stageError

One failed probe stage: the name an operator routes on, plus the cause.
================
*/
type stageError struct {
	stage string
	err   error
}

/*
================
Error
================
*/
func (e *stageError) Error() string { return fmt.Sprintf("stage=%s: %v", e.stage, e.err) }

/*
================
fail

Name the stage that broke and exit non-zero for schedulers and operators.
================
*/
func fail(err error) {
	var se *stageError
	if errors.As(err, &se) {
		fmt.Fprintf(os.Stderr, "FAIL stage=%s: %v\n", se.stage, se.err)
	} else {
		fmt.Fprintf(os.Stderr, "FAIL stage=probe: %v\n", err)
	}
	os.Exit(1)
}

/*
================
probeConfig

Everything one probe run needs; the command line is one way to fill it.
================
*/
type probeConfig struct {
	Base      string
	WTAddr    string
	Ticket    string
	Timeout   time.Duration
	Transport string
	// CACert is a PEM bundle added to the trust roots - the way to probe
	// a development endpoint whose certificate is self-signed.
	CACert string
}

/*
================
fetchCertHash

Read the advertised certificate hash over the plain TCP path.
================
*/
func fetchCertHash(ctx context.Context, client *http.Client, base string) (certHashDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+transport.PathCertHash, nil)
	if err != nil {
		return certHashDoc{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return certHashDoc{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return certHashDoc{}, fmt.Errorf("cert-hash status %s", resp.Status)
	}
	var doc certHashDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, certHashBodyCap)).Decode(&doc); err != nil {
		return certHashDoc{}, err
	}
	if _, err := hex.DecodeString(doc.SHA256Hex); err != nil {
		return certHashDoc{}, fmt.Errorf("cert-hash sha256Hex is not hex: %w", err)
	}
	return doc, nil
}

/*
================
trustPool

The verification roots for one probe: the system pool, plus the
operator's bundle when one is named (a development endpoint's
self-signed certificate). A bundle that will not parse fails here,
before any dial, rather than weakening the dial.
================
*/
func trustPool(caCertFile string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if caCertFile == "" {
		return pool, nil
	}
	pem, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s holds no PEM certificate", caCertFile)
	}
	return pool, nil
}

/*
================
probeTLS

Normal PKI verification against the given roots, plus one additive
check: the leaf actually served must be the certificate whose hash the
TCP endpoint advertised. Verification is never skipped - the pin only
narrows what a verified chain may end in, so a publicly valid but
substituted certificate fails closed too.
================
*/
func probeTLS(serverName string, sha256Hex string, roots *x509.CertPool) *tls.Config {
	want, _ := hex.DecodeString(sha256Hex)
	return &tls.Config{
		ServerName: serverName,
		RootCAs:    roots,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("server presented no certificate")
			}
			got := sha256.Sum256(state.PeerCertificates[0].Raw)
			if !equalBytes(got[:], want) {
				return fmt.Errorf("served certificate sha256 %x does not match advertised %x", got, want)
			}
			return nil
		},
	}
}

/*
================
equalBytes

Compare two digests without pulling in a helper for one call site.
================
*/
func equalBytes(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

/*
================
probe

Open the selected transport through the shared admission client. The
default WT smoke retains its certificate evidence and success-line shape.
================
*/
func probe(ctx context.Context, cfg probeConfig) (string, error) {
	client, err := newProbeClient(cfg)
	if err != nil {
		return "", err
	}
	defer client.close()
	if err := client.open(ctx); err != nil {
		return "", err
	}
	if client.cfg.Transport == "ws" {
		return fmt.Sprintf("OK welcome session=%d transport=ws", client.welcome.SessionID), nil
	}
	return fmt.Sprintf("OK welcome session=%d cert-sha256=%s notAfter=%s",
		client.welcome.SessionID, client.cert.SHA256Hex, client.cert.NotAfter), nil
}

/*
================
main

cert-hash over TCP, HELLO/WELCOME over UDP: both halves must pass.
================
*/
func main() {
	base := flag.String("url", "", "public base URL of the environment, e.g. https://play.example.net")
	wtAddr := flag.String("wt-addr", "", "WebTransport host:port (default: the base URL host on port 443)")
	kind := flag.String("transport", "wt", "transport to probe: wt or ws")
	ticketEnv := flag.String("ticket-env", defaultTicketEnv, "environment variable holding a fresh admission ticket")
	caCert := flag.String("ca-cert", "", "PEM bundle added to the trust roots (a development endpoint's self-signed certificate)")
	timeout := flag.Duration("timeout", defaultTimeout, "smoke budget; measurement handshake and response-drain timeout")
	measure := flag.Bool("measure", false, "run both measurement schedules and emit one JSON document")
	samples := flag.Int("samples", defaultPingSamples, "ping train samples in measurement mode")
	interval := flag.Duration("ping-interval", defaultPingInterval, "ping train send interval in measurement mode")
	burst := flag.Int("burst", defaultBurstSamples, "ordered burst frames in measurement mode")
	burstInterval := flag.Duration("burst-interval", defaultBurstInterval, "burst send interval; zero requests an explicit unpaced arm")
	flag.Parse()

	if *base == "" {
		fmt.Fprintln(os.Stderr, "usage: sro-wtprobe -url https://play.example.net [-wt-addr host:port]")
		os.Exit(2)
	}
	ticket := strings.TrimSpace(os.Getenv(*ticketEnv))
	if ticket == "" {
		fmt.Fprintf(os.Stderr, "admission ticket env %s is empty; mint one for the probe account first\n", *ticketEnv)
		os.Exit(2)
	}

	cfg := probeConfig{Base: *base, WTAddr: *wtAddr, Ticket: ticket, Timeout: *timeout, CACert: *caCert, Transport: *kind}
	if *measure {
		settings := measurementConfig{
			Probe: cfg, PingSamples: *samples, PingInterval: *interval,
			BurstSamples: *burst, BurstInterval: *burstInterval,
		}
		budget, err := settings.budget()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		defer cancel()
		report, err := measureProbe(ctx, settings)
		if encodeError := json.NewEncoder(os.Stdout).Encode(report); encodeError != nil {
			fail(&stageError{"json", encodeError})
		}
		if err != nil {
			fail(err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	line, err := probe(ctx, cfg)
	if err != nil {
		fail(err)
	}
	fmt.Println(line)
}
