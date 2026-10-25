//go:build windows

/*
===========================================================================

udp_buffer_readback_windows_test.go - SO_RCVBUF readback on Windows

===========================================================================
*/

package transport

import (
	"net"

	"golang.org/x/sys/windows"
)

/*
================
udpReceiveBufferSize

Reads the receive buffer size the kernel actually granted.
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
		size, sockErr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_RCVBUF)
	}); err != nil {
		return 0, err
	}
	return size, sockErr
}
