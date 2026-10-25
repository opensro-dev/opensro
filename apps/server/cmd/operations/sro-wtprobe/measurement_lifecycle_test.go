/*
===========================================================================

measurement_lifecycle_test.go - reader failures survive the shutdown boundary

Publication can lag decoding. These required stop dependencies place an
error on either side of the stop request without sleeps or source anchors.

===========================================================================
*/

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"

	"github.com/gorilla/websocket"
)

/*
================
probeHiddenTimeout

Gorilla exposes timeouts through net.Error without unwrapping the original
deadline error. Model that observable contract rather than its source text.
================
*/
type probeHiddenTimeout struct{}

/*
================
Error
================
*/
func (*probeHiddenTimeout) Error() string { return "probe I/O deadline" }

/*
================
Timeout
================
*/
func (*probeHiddenTimeout) Timeout() bool { return true }

/*
================
Temporary
================
*/
func (*probeHiddenTimeout) Temporary() bool { return false }

/*
================
TestMeasurementFinalizationPreservesReaderFailures
================
*/
func TestMeasurementFinalizationPreservesReaderFailures(t *testing.T) {
	protocolFailure := errors.New("decoded invalid probe response")
	for _, test := range []struct {
		name              string
		err               error
		expectedAfterStop bool
	}{
		{"protocol", protocolFailure, true},
		{"peer_eof", io.EOF, true},
		{"local_close", net.ErrClosed, false},
		{"local_deadline", os.ErrDeadlineExceeded, false},
		{"hidden_deadline", &probeHiddenTimeout{}, false},
		{"remote_normal_close", &websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "peer closed"}, true},
	} {
		for _, delayed := range []bool{false, true} {
			name := test.name + "/before_stop"
			if delayed {
				name = test.name + "/published_after_stop"
			}
			t.Run(name, func(t *testing.T) {
				recorder := newProbeRecorder()
				stopped := make(chan struct{})
				if delayed {
					go func() {
						<-stopped
						recorder.completeRead(test.err)
					}()
				} else {
					recorder.completeRead(test.err)
				}
				finished := make(chan error, 1)
				go func() {
					finished <- recorder.finish(func() error {
						close(stopped)
						return nil
					})
				}()
				ctx, cancel := context.WithTimeout(context.Background(), measurementTestTimeout)
				defer cancel()
				select {
				case err := <-finished:
					wantFailure := !delayed || test.expectedAfterStop
					if errors.Is(err, test.err) != wantFailure {
						t.Fatalf("reader failure lost or invented: delayed=%v error=%v", delayed, err)
					}
				case <-ctx.Done():
					t.Fatal("reader finalization failed to join")
				}
			})
		}
	}
}
