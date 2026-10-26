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
validateWebTransportPosture

The public WebTransport posture as one deployment decision, checked
before Nomad mutation so a half-posture fails in both directions: on
without its pair, bind or advertised route is refused, and a public
bind named while the switch is off is refused too. The GameWorld
refuses the same shapes at boot (transport.NewServer); this is the
earlier, louder half of that line, where the operator can still act.
certificate is the loaded pair from resolveTransportCertificate, nil
when none was loaded.
================
*/
func validateWebTransportPosture(certificate *transport.Certificate, public bool, wtAddr string, shards []shardDeployment) error {
	wtAddr = strings.TrimSpace(wtAddr)
	if !public {
		if wtAddr != "" {
			return fmt.Errorf(
				"-transport-wt-addr %q names a public UDP bind, but SRO_WEBTRANSPORT_PUBLIC is off; set the switch or drop the address",
				wtAddr,
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
		if !strings.HasSuffix(endpoint.Path, transport.PathWS) {
			return fmt.Errorf(
				"shard %q advertised transport URL %q must end %s, the route clients map to WebTransport",
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
		if advertisedPort != port {
			return fmt.Errorf(
				"shard %q advertises WebTransport on port %d, but -transport-wt-addr binds port %d",
				game.Definition.ID,
				advertisedPort,
				port,
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
