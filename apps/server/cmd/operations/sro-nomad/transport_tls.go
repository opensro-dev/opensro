package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"opensro.online/server/internal/transport"
)

const managedDevelopmentTLSIdentity = "managed-development"

// resolveTransportCertificate owns the deployment-time WebTransport TLS
// boundary. A production pair is loaded before Nomad mutation, checked
// against every public shard host, and reduced to a non-secret fingerprint
// that makes an in-place certificate renewal create a new job version.
// The loaded pair travels with the result so the public-posture check can
// verify the hosts clients will actually dial; it is nil when nothing was
// loaded.
func resolveTransportCertificate(
	certFile string,
	keyFile string,
	hostNetwork string,
	validate bool,
	shards []shardDeployment,
) (string, string, string, *transport.Certificate, error) {
	certFile = strings.TrimSpace(certFile)
	keyFile = strings.TrimSpace(keyFile)
	if (certFile == "") != (keyFile == "") {
		return "", "", "", nil, fmt.Errorf(
			"-transport-cert-file and -transport-key-file must be configured together",
		)
	}
	if certFile == "" {
		if validate && hostNetwork != "loopback" {
			return "", "", "", nil, fmt.Errorf(
				"-transport-cert-file and -transport-key-file are required for host network %q",
				hostNetwork,
			)
		}
		return "", "", managedDevelopmentTLSIdentity, nil, nil
	}

	certFile = cleanAbsolute(certFile)
	keyFile = cleanAbsolute(keyFile)
	if !validate {
		return slashPath(certFile), slashPath(keyFile), "", nil, nil
	}
	if err := requireRegularFile(certFile); err != nil {
		return "", "", "", nil, err
	}
	if err := requireRegularFile(keyFile); err != nil {
		return "", "", "", nil, err
	}
	certificate, err := transport.LoadCertificate(certFile, keyFile, "")
	if err != nil {
		return "", "", "", nil, fmt.Errorf(
			"WebTransport TLS certificate: %w",
			err,
		)
	}
	if hostNetwork != "loopback" {
		for _, game := range shards {
			endpoint, err := absoluteHTTPURL(game.Definition.TransportURL)
			if err != nil {
				return "", "", "", nil, fmt.Errorf(
					"shard %q transport URL: %w",
					game.Definition.ID,
					err,
				)
			}
			if endpoint.Scheme != "https" {
				return "", "", "", nil, fmt.Errorf(
					"shard %q transport URL must use HTTPS outside loopback",
					game.Definition.ID,
				)
			}
			if err := certificate.Leaf.VerifyHostname(endpoint.Hostname()); err != nil {
				return "", "", "", nil, fmt.Errorf(
					"WebTransport certificate does not cover shard %q host %q: %w",
					game.Definition.ID,
					endpoint.Hostname(),
					err,
				)
			}
		}
	}
	return slashPath(certFile),
		slashPath(keyFile),
		certificate.SHA256Hex(),
		certificate,
		nil
}

/*
================
webTransportPosture

The public WebTransport deployment decision: the switch, the UDP bind,
and the public port the host redirects to that bind (0 when clients
dial the bind port itself).
================
*/
type webTransportPosture struct {
	Public     bool
	WTAddr     string
	PublicPort int
}

/*
================
validateWebTransportPosture

The public WebTransport posture as one deployment decision, checked
before Nomad mutation so a half-posture fails in both directions: on
without its pair, bind or advertised route is refused, and a public
bind named while the switch is off is refused too. The GameWorld
refuses the same shapes at boot (transport.NewServer); this is the
earlier, louder half of that line, where the operator can still act.
certificate is the loaded pair from resolveTransportCertificate, nil
when none was loaded.

The advertised URL is the transport base clients append every
transport path to (advertisedTransportBase). The advertised port must
be the port clients reach: the bind port, or
PublicPort when the host redirects a public port to the bind (#1447:
the GameWorld runs unprivileged and cannot bind UDP 443, so the host
redirects public 443 to a high bind port). A redirected bind must be
an unspecified host: the redirect delivers to the interface address.
================
*/
func validateWebTransportPosture(certificate *transport.Certificate, posture webTransportPosture, shards []shardDeployment) error {
	wtAddr := strings.TrimSpace(posture.WTAddr)
	if !posture.Public {
		if wtAddr != "" {
			return fmt.Errorf(
				"-transport-wt-addr %q names a public UDP bind, but SRO_WEBTRANSPORT_PUBLIC is off; set the switch or drop the address",
				wtAddr,
			)
		}
		if posture.PublicPort != 0 {
			return fmt.Errorf(
				"-transport-wt-public-port %d names a public port, but SRO_WEBTRANSPORT_PUBLIC is off; set the switch or drop the port",
				posture.PublicPort,
			)
		}
		return nil
	}
	if certificate == nil {
		return fmt.Errorf(
			"SRO_WEBTRANSPORT_PUBLIC is on, but no production TLS pair is configured; " +
				"the public endpoint never serves the managed development certificate",
		)
	}
	if wtAddr == "" {
		return fmt.Errorf(
			"-transport-wt-addr is required with SRO_WEBTRANSPORT_PUBLIC: the public UDP bind is an operator value, never derived",
		)
	}
	host, portText, err := net.SplitHostPort(wtAddr)
	if err != nil || host == "" {
		return fmt.Errorf("-transport-wt-addr %q must be a host:port", wtAddr)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("-transport-wt-addr %q has an invalid port", wtAddr)
	}
	reached := port
	if posture.PublicPort != 0 {
		if posture.PublicPort < 1 || posture.PublicPort > 65535 {
			return fmt.Errorf("-transport-wt-public-port %d is not a port", posture.PublicPort)
		}
		// The host's redirect delivers to the receiving interface's own
		// address, so only a wildcard bind is sure to receive it; loopback
		// or another address would pass every check and never see a packet.
		if posture.PublicPort != port {
			if ip := net.ParseIP(host); ip == nil || !ip.IsUnspecified() {
				return fmt.Errorf(
					"-transport-wt-addr %q must bind an unspecified host ([::] or 0.0.0.0) when -transport-wt-public-port %d is redirected to it",
					wtAddr,
					posture.PublicPort,
				)
			}
		}
		reached = posture.PublicPort
	}
	for _, game := range shards {
		advertised := game.Definition.AdvertisedTransportURL()
		endpoint, err := absoluteHTTPURL(advertised)
		if err != nil || endpoint.Scheme != "https" {
			return fmt.Errorf(
				"shard %q advertised transport URL %q must be an absolute HTTPS URL for public WebTransport",
				game.Definition.ID,
				advertised,
			)
		}
		if !advertisedTransportBase(endpoint.Path) {
			return fmt.Errorf(
				"shard %q advertised transport URL %q must be the transport base (https://host or https://host/route); "+
					"clients append %s and the other transport paths themselves",
				game.Definition.ID,
				advertised,
				transport.PathWS,
			)
		}
		advertisedPort := 443
		if text := endpoint.Port(); text != "" {
			if advertisedPort, err = strconv.Atoi(text); err != nil {
				return fmt.Errorf(
					"shard %q advertised transport URL %q has an invalid port",
					game.Definition.ID,
					advertised,
				)
			}
		}
		if advertisedPort != reached {
			return fmt.Errorf(
				"shard %q advertises WebTransport on port %d, but clients reach port %d (-transport-wt-addr binds %d, -transport-wt-public-port %d)",
				game.Definition.ID,
				advertisedPort,
				reached,
				port,
				posture.PublicPort,
			)
		}
		if err := certificate.Leaf.VerifyHostname(endpoint.Hostname()); err != nil {
			return fmt.Errorf(
				"WebTransport certificate does not cover shard %q advertised host %q: %w",
				game.Definition.ID,
				endpoint.Hostname(),
				err,
			)
		}
	}
	return nil
}

/*
================
advertisedTransportBase

Whether an advertised URL path is the transport base: the origin, or an
origin plus a route that names no transport path. Every consumer appends
the socket, WebTransport, cert-hash and references paths to this base
(the browser client, sro-wtprobe), so the advertised URL is never the
socket itself.

Transition (#1447 follow-up): a base that already ends with the socket
path is still accepted for one release, so a host catalogue written in
the old socket form keeps deploying; clients from #1886 on normalise it.
Remove the tolerance once no host catalogue holds the socket form.
================
*/
func advertisedTransportBase(path string) bool {
	path = strings.TrimSuffix(path, "/")
	if strings.HasSuffix(path, transport.PathWS) {
		return true
	}
	return !strings.Contains(path+"/", "/transport/")
}
