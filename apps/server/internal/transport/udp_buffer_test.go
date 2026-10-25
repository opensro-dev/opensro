/*
===========================================================================

udp_buffer_test.go - the WebTransport socket-buffer sizing tests

The default, the TRANSPORT_UDP_BUFFER_BYTES override, the config
refusal and the socket a started server actually owns.

===========================================================================
*/

package transport

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

/*
================
TestUDPBufferBytesDefaultAndEnvOverride

The default ships at the 25 MB the host proposal pairs with the
host's raised ceilings, and the environment still wins when set.
================
*/
func TestUDPBufferBytesDefaultAndEnvOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	RegisterViperDefaults()
	if got := ConfigFromViper().UDPBufferBytes; got != DefaultUDPBufferBytes {
		t.Fatalf("default udp_buffer_bytes = %d, want %d", got, DefaultUDPBufferBytes)
	}
	t.Setenv("TRANSPORT_UDP_BUFFER_BYTES", "4194304")
	if got, want := ConfigFromViper().UDPBufferBytes, 4194304; got != want {
		t.Fatalf("TRANSPORT_UDP_BUFFER_BYTES=4194304 read back as %d", got)
	}
}

/*
================
TestUDPBufferBytesNegativeRejected

A negative size is a configuration error, not a silent default: the
defaults pass only replaces a zero, so a negative reaches NewServer
and is refused there.
================
*/
func TestUDPBufferBytesNegativeRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WTAddr, cfg.WSAddr = "127.0.0.1:0", "127.0.0.1:0"
	cfg.CertDir = t.TempDir()
	cfg.UDPBufferBytes = -1
	if _, err := NewServer(cfg); err == nil {
		t.Fatal("NewServer accepted UDPBufferBytes=-1")
	}
}

/*
================
TestServerRaisesUDPBuffers

A started server owns a socket whose receive buffer reflects the
request, not the small stock default. The threshold is deliberately
loose: the kernel may clamp (Linux grants at most its rmem_max,
doubled), but even a clamped grant lands far above the 262144 floor
this test pins.
================
*/
func TestServerRaisesUDPBuffers(t *testing.T) {
	cfg := Config{
		WTAddr:         "127.0.0.1:0",
		WSAddr:         "127.0.0.1:0",
		CertDir:        t.TempDir(),
		UDPBufferBytes: 1 << 20,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Start refuses to serve until both admission gates exist; the
	// test identities only need the sockets bound.
	srv.Hub.SetHelloAuth(func([]byte) (AdmissionIdentity, error) {
		return AdmissionIdentity{AccountID: "test-account", ShardID: "test-shard"}, nil
	})
	srv.Hub.SetEnterWorldAuth(func(*Session, EnterWorld) (bool, uint32) {
		return true, 0
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	got, err := udpReceiveBufferSize(srv.udpConn)
	if err != nil {
		t.Fatalf("reading back SO_RCVBUF: %v", err)
	}
	if got < 262144 {
		t.Fatalf("SO_RCVBUF = %d, want at least 262144 after asking for 1 MB", got)
	}
}
