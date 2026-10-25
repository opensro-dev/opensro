/*
===========================================================================

connection_interrupt_test.go - interrupt reads before starting TLS teardown

Closing a TLS connection sends close_notify, which can make a reverse
proxy send a WebSocket close before our reader has joined. The stop door
sets a local deadline; the ordinary close remains a later lifecycle step.

===========================================================================
*/

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

/*
================
probeTrackedConn

Observe lifecycle operations at the actual TCP boundary, including calls
delegated by a TLS connection. No implementation text is inspected.
================
*/
type probeTrackedConn struct {
	net.Conn
	closed   atomic.Bool
	deadline atomic.Bool
}

/*
================
Close
================
*/
func (conn *probeTrackedConn) Close() error {
	conn.closed.Store(true)
	return conn.Conn.Close()
}

/*
================
SetDeadline
================
*/
func (conn *probeTrackedConn) SetDeadline(deadline time.Time) error {
	conn.deadline.Store(true)
	return conn.Conn.SetDeadline(deadline)
}

/*
================
TestProbeInterruptDefersCloseUntilReaderJoined
================
*/
func TestProbeInterruptDefersCloseUntilReaderJoined(t *testing.T) {
	for _, secure := range []bool{false, true} {
		kind := "ws"
		if secure {
			kind = "wss"
		}
		t.Run(kind, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				upgrader := websocket.Upgrader{}
				peer, err := upgrader.Upgrade(w, request, nil)
				if err != nil {
					t.Errorf("fixture upgrade: %v", err)
					return
				}
				defer peer.Close()
				for {
					if _, _, err := peer.ReadMessage(); err != nil {
						return
					}
				}
			})
			var server *httptest.Server
			if secure {
				server = httptest.NewTLSServer(handler)
			} else {
				server = httptest.NewServer(handler)
			}
			t.Cleanup(server.Close)
			var tracked *probeTrackedConn
			dialer := websocket.Dialer{
				NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					var tcp net.Dialer
					conn, err := tcp.DialContext(ctx, network, address)
					if err != nil {
						return nil, err
					}
					tracked = &probeTrackedConn{Conn: conn}
					return tracked, nil
				},
			}
			if secure {
				roots := x509.NewCertPool()
				roots.AddCert(server.Certificate())
				dialer.TLSClientConfig = &tls.Config{RootCAs: roots}
			}
			ctx, cancel := context.WithTimeout(context.Background(), measurementTestTimeout)
			defer cancel()
			address := strings.Replace(server.URL, "http", "ws", 1)
			socket, response, err := dialer.DialContext(ctx, address, nil)
			if err != nil {
				if response != nil {
					_ = response.Body.Close()
				}
				t.Fatal(err)
			}
			client, err := newProbeClient(probeConfig{Base: server.URL, Transport: "ws", Timeout: measurementTestTimeout})
			if err != nil {
				_ = socket.Close()
				t.Fatal(err)
			}
			client.ws = socket
			t.Cleanup(client.close)
			tracked.deadline.Store(false)
			tracked.closed.Store(false)
			if err := client.interrupt(); err != nil {
				t.Fatal(err)
			}
			if tracked.closed.Load() || !tracked.deadline.Load() {
				t.Fatal("read interruption closed the connection before the reader could join")
			}
			_, err = client.readFrame()
			var timeout net.Error
			if !errors.As(err, &timeout) || !timeout.Timeout() {
				t.Fatalf("interrupted read did not report a deadline: %v", err)
			}
			client.close()
			if !tracked.closed.Load() {
				t.Fatal("final close left the socket open")
			}
		})
	}
}
