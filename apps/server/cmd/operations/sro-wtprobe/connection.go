/*
===========================================================================

connection.go - one authenticated frame client for transport probes

Smoke and measurement runs share certificate validation, HELLO admission
and reliable framing. One writer serializes measurement pings and replies
to server keepalives. A canceled run closes the sockets that its reader
may be waiting on.

===========================================================================
*/

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/quic-go/webtransport-go"
	"opensro.online/server/internal/transport"
)

const probeFrameLengthBytes = 4

/*
================
probeClient

All socket state belongs here. WELCOME is one handshake sample; repeating
a successful run requires another one-use admission ticket.
================
*/
type probeClient struct {
	cfg       probeConfig
	base      *url.URL
	http      *http.Client
	roots     *x509.CertPool
	cert      certHashDoc
	welcome   transport.Welcome
	handshake time.Duration
	ws        *websocket.Conn
	wt        *webtransport.Session
	dialer    *webtransport.Transport
	stream    *webtransport.Stream
	writeMu   sync.Mutex
	closeOnce sync.Once
	done      chan struct{}
}

/*
================
newProbeClient

The operator's additional CA roots apply to HTTPS, WSS and QUIC alike.
The WT leaf pin remains an additional restriction on a verified chain.
================
*/
func newProbeClient(cfg probeConfig) (*probeClient, error) {
	if cfg.Transport == "" {
		cfg.Transport = "wt"
	}
	if cfg.Transport != "wt" && cfg.Transport != "ws" {
		return nil, &stageError{"config", fmt.Errorf("transport must be wt or ws")}
	}
	if cfg.Timeout <= 0 {
		return nil, &stageError{"config", fmt.Errorf("timeout must be positive")}
	}
	cfg.Base = strings.TrimSuffix(cfg.Base, "/")
	base, err := url.Parse(cfg.Base)
	if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, &stageError{"config", fmt.Errorf("base URL must be an HTTP or HTTPS origin or path")}
	}
	if cfg.WTAddr != "" {
		host, port, err := net.SplitHostPort(cfg.WTAddr)
		if err != nil || host == "" || port == "" || strings.ContainsAny(host, "/@?#\\") {
			return nil, &stageError{"config", fmt.Errorf("wt-addr must be a host:port")}
		}
	}
	roots, err := trustPool(cfg.CACert)
	if err != nil {
		return nil, &stageError{"trust", err}
	}
	return &probeClient{
		cfg: cfg, base: base, roots: roots, done: make(chan struct{}),
		http: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				Proxy:           http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{RootCAs: roots},
			},
		},
	}, nil
}

/*
================
open

Dial and consume a ticket only through the server's ordinary WELCOME
transaction. HELLO-to-WELCOME timing excludes certificate discovery and
the transport dial, matching the separately reported server metric.
================
*/
func (client *probeClient) open(ctx context.Context) error {
	stageCtx, cancel := context.WithTimeout(ctx, client.cfg.Timeout)
	defer cancel()
	var err error
	if client.cfg.Transport == "wt" {
		err = client.dialWT(stageCtx)
	} else {
		err = client.dialWS(stageCtx)
	}
	if err != nil {
		return err
	}
	go func() {
		select {
		case <-ctx.Done():
			client.close()
		case <-client.done:
		}
	}()
	deadline, _ := stageCtx.Deadline()
	if err := client.setDeadline(deadline); err != nil {
		return &stageError{"stream", err}
	}
	started := time.Now()
	if err := client.writeFrame(transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(client.cfg.Ticket)}),
	}); err != nil {
		return &stageError{"hello", err}
	}
	for {
		frame, err := client.readFrame()
		if err != nil {
			return &stageError{"welcome", err}
		}
		if frame.Opcode == transport.OpPing {
			if err := client.replyPing(frame); err != nil {
				return &stageError{"welcome", err}
			}
			continue
		}
		if frame.Opcode == transport.OpPong {
			continue
		}
		if frame.Opcode != transport.OpWelcome {
			return &stageError{"welcome", fmt.Errorf("first frame = 0x%04X, want WELCOME", frame.Opcode)}
		}
		client.welcome, err = transport.DecodeWelcome(frame.Payload)
		if err != nil {
			return &stageError{"welcome", err}
		}
		client.handshake = time.Since(started)
		deadline, _ = ctx.Deadline()
		if err := client.setDeadline(deadline); err != nil {
			return &stageError{"stream", err}
		}
		return nil
	}
}

/*
================
dialWT
================
*/
func (client *probeClient) dialWT(ctx context.Context) error {
	doc, err := fetchCertHash(ctx, client.http, client.cfg.Base)
	if err != nil {
		return &stageError{"cert-hash", err}
	}
	client.cert = doc
	hostport := client.cfg.WTAddr
	if hostport == "" {
		hostport = net.JoinHostPort(client.base.Hostname(), "443")
	}
	serverName := client.base.Hostname()
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		serverName = host
	}
	client.dialer = &webtransport.Transport{TLSClientConfig: probeTLS(serverName, doc.SHA256Hex, client.roots)}
	address := "https://" + hostport + transport.PathWT
	response, session, err := client.dialer.Dial(ctx, address, nil)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return &stageError{"dial", err}
	}
	client.wt = session
	client.stream, err = session.OpenStreamSync(ctx)
	if err != nil {
		return &stageError{"stream", err}
	}
	return nil
}

/*
================
dialWS
================
*/
func (client *probeClient) dialWS(ctx context.Context) error {
	address := *client.base
	address.Path = strings.TrimSuffix(address.Path, "/") + transport.PathWS
	address.RawPath = ""
	address.Scheme = "ws"
	if client.base.Scheme == "https" {
		address.Scheme = "wss"
	}
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: client.cfg.Timeout,
		TLSClientConfig:  &tls.Config{RootCAs: client.roots},
	}
	connection, response, err := dialer.DialContext(ctx, address.String(), nil)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return &stageError{"dial", err}
	}
	connection.SetReadLimit(int64(transport.MaxFrameBytes + probeFrameLengthBytes))
	client.ws = connection
	return nil
}

/*
================
setDeadline
================
*/
func (client *probeClient) setDeadline(deadline time.Time) error {
	if client.ws != nil {
		if err := client.ws.SetReadDeadline(deadline); err != nil {
			return err
		}
		return client.ws.SetWriteDeadline(deadline)
	}
	return client.stream.SetDeadline(deadline)
}

/*
================
readFrame
================
*/
func (client *probeClient) readFrame() (transport.Frame, error) {
	if client.ws == nil {
		return transport.ReadStreamFrame(client.stream)
	}
	kind, body, err := client.ws.ReadMessage()
	if err != nil {
		return transport.Frame{}, err
	}
	if kind != websocket.BinaryMessage {
		return transport.Frame{}, fmt.Errorf("WebSocket message type %d is not binary", kind)
	}
	return transport.DecodeFrame(body)
}

/*
================
writeFrame
================
*/
func (client *probeClient) writeFrame(frame transport.Frame) error {
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	if client.ws != nil {
		return client.ws.WriteMessage(websocket.BinaryMessage, frame.Encode())
	}
	return transport.WriteStreamFrame(client.stream, frame)
}

/*
================
replyPing

A long probe participates in keepalive instead of mistaking it for a
measurement reply. The server's advertised echo bound applies both ways.
================
*/
func (client *probeClient) replyPing(frame transport.Frame) error {
	if len(frame.Payload) > transport.MaxPingPayloadBytes {
		return fmt.Errorf("keepalive payload exceeds %d bytes", transport.MaxPingPayloadBytes)
	}
	return client.writeFrame(transport.Frame{Opcode: transport.OpPong, Payload: frame.Payload})
}

/*
================
interrupt

Interrupt pending I/O without starting peer-visible TLS shutdown. Closing
a TLS socket here can make a proxy send a normal WebSocket close before
our reader joins. The ordinary close belongs after that join.
================
*/
func (client *probeClient) interrupt() error {
	var err error
	if client.ws != nil {
		err = client.ws.UnderlyingConn().SetDeadline(time.Now())
	} else {
		err = client.stream.SetDeadline(time.Now())
	}
	if err != nil {
		client.close()
		return err
	}
	return nil
}

/*
================
close
================
*/
func (client *probeClient) close() {
	client.closeOnce.Do(func() {
		close(client.done)
		if client.ws != nil {
			_ = client.ws.Close()
		}
		if client.wt != nil {
			_ = client.wt.CloseWithError(0, "probe complete")
		}
		if client.dialer != nil {
			_ = client.dialer.Close()
		}
		client.http.CloseIdleConnections()
	})
}
