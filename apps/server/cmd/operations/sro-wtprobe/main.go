/*
===========================================================================

main.go - sro-wtprobe: open a real WebTransport session from outside

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
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/quic-go/webtransport-go"
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
	Base    string
	WTAddr  string
	Ticket  string
	Timeout time.Duration
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
func fetchCertHash(ctx context.Context, base string) (certHashDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+transport.PathCertHash, nil)
	if err != nil {
		return certHashDoc{}, err
	}
	resp, err := http.DefaultClient.Do(req)
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

Run both halves in order: the cert-hash document over TCP, then a HELLO
answered by WELCOME over UDP. Returns the success line for the caller.
================
*/
func probe(ctx context.Context, cfg probeConfig) (string, error) {
	base := strings.TrimSuffix(cfg.Base, "/")
	doc, err := fetchCertHash(ctx, base)
	if err != nil {
		return "", &stageError{"cert-hash", err}
	}

	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "", &stageError{"cert-hash", fmt.Errorf("invalid base URL %q", cfg.Base)}
	}
	hostport := cfg.WTAddr
	if hostport == "" {
		hostport = net.JoinHostPort(parsed.Hostname(), "443")
	}
	serverName := parsed.Hostname()
	if h, _, splitErr := net.SplitHostPort(hostport); splitErr == nil {
		serverName = h
	}

	roots, err := trustPool(cfg.CACert)
	if err != nil {
		return "", &stageError{"trust", err}
	}
	dialer := webtransport.Transport{TLSClientConfig: probeTLS(serverName, doc.SHA256Hex, roots)}
	defer dialer.Close()
	wtURL := "https://" + hostport + transport.PathWT
	// A 2xx response body is the session's own stream: the session owns
	// its lifetime and closing it would close the session, so only the
	// failure response below is closed.
	rsp, sess, err := dialer.Dial(ctx, wtURL, nil)
	if err != nil {
		if rsp != nil && rsp.Body != nil {
			_ = rsp.Body.Close()
		}
		return "", &stageError{"dial", fmt.Errorf("%s: %w", wtURL, err)}
	}
	stream, err := sess.OpenStreamSync(ctx)
	if err != nil {
		return "", &stageError{"stream", err}
	}
	if err := stream.SetReadDeadline(time.Now().Add(cfg.Timeout)); err != nil {
		return "", &stageError{"stream", err}
	}
	err = transport.WriteStreamFrame(stream, transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(cfg.Ticket)}),
	})
	if err != nil {
		return "", &stageError{"hello", err}
	}
	for {
		f, err := transport.ReadStreamFrame(stream)
		if err != nil {
			return "", &stageError{"welcome", err}
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		if f.Opcode != transport.OpWelcome {
			return "", &stageError{"welcome", fmt.Errorf("first frame = 0x%04X, want WELCOME", f.Opcode)}
		}
		w, err := transport.DecodeWelcome(f.Payload)
		if err != nil {
			return "", &stageError{"welcome", err}
		}
		_ = sess.CloseWithError(0, "probe complete")
		return fmt.Sprintf("OK welcome session=%d cert-sha256=%s notAfter=%s", w.SessionID, doc.SHA256Hex, doc.NotAfter), nil
	}
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
	ticketEnv := flag.String("ticket-env", defaultTicketEnv, "environment variable holding a fresh admission ticket")
	caCert := flag.String("ca-cert", "", "PEM bundle added to the trust roots (a development endpoint's self-signed certificate)")
	timeout := flag.Duration("timeout", defaultTimeout, "overall probe budget")
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

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	line, err := probe(ctx, probeConfig{Base: *base, WTAddr: *wtAddr, Ticket: ticket, Timeout: *timeout, CACert: *caCert})
	if err != nil {
		fail(err)
	}
	fmt.Println(line)
}
