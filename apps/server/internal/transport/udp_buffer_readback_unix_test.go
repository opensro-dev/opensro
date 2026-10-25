//go:build linux || darwin

/*
===========================================================================

udp_buffer_readback_unix_test.go - SO_RCVBUF readback on Unix

===========================================================================
*/

package transport

import (
	"net"
	"syscall"
)

/*
================
udpReceiveBufferSize

Reads the receive buffer size the kernel actually granted. Linux
reports double the usable space (it counts bookkeeping), which only
makes the test floor easier to clear.
================
*/
func udpReceiveBufferSize(conn *net.UDPConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var size int
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		size, sockErr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	}); err != nil {
		return 0, err
	}
	return size, sockErr
}
