/*
===========================================================================

notice.go - send an operator-signed notice before a fleet restart

Only the local operator with the private identity can mint the request. The
existing control listeners remain private; HTTP redirects are never followed.

===========================================================================
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/security/auth"
)

const (
	noticeRequestTimeout = 10 * time.Second
	noticeResponseLimit  = 4096
)

/*
================
runNotice
================
*/
func runNotice(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("notice", flag.ContinueOnError)
	stateDir := flags.String("state-dir", filepath.Join(".state", "cluster"), "private operator identity directory")
	catalogPath := flags.String("catalog", filepath.Join("config", "shards.json"), "shard catalog")
	message := flags.String("message", "", "notice text (at most 100 UTF-16 units)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("notice does not accept positional arguments")
	}
	if err := auth.ValidateNoticeText(*message); err != nil {
		return err
	}
	catalog, err := shard.Load(*catalogPath)
	if err != nil {
		return err
	}
	signer, err := auth.NewAgentSessionSigner(filepath.Join(*stateDir, auth.AgentSessionPrivateKeyRingFile))
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout:       noticeRequestTimeout,
		Transport:     &http.Transport{},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	for _, definition := range catalog.Definitions() {
		if !definition.Enabled {
			continue
		}
		endpoint, err := noticeEndpoint(definition.ControlURL)
		if err != nil {
			return fmt.Errorf("shard %s: %w", definition.ID, err)
		}
		token, err := signer.MintNotice(definition.ID, *message, time.Now())
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(token))
		if err != nil {
			return err
		}
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
		request.Header.Set("Content-Type", "text/plain")
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("notice to %s: %w", definition.ID, err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, noticeResponseLimit))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("notice to %s: HTTP %d", definition.ID, response.StatusCode)
		}
		fmt.Printf("Notice accepted by shard %s\n", definition.ID)
	}
	return nil
}

/*
================
noticeEndpoint
================
*/
func noticeEndpoint(controlURL string) (string, error) {
	endpoint, err := url.Parse(controlURL)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", fmt.Errorf("notice requires a loopback HTTP control URL")
	}
	address := net.ParseIP(endpoint.Hostname())
	if address == nil || !address.IsLoopback() || (endpoint.Path != "" && endpoint.Path != "/") {
		return "", fmt.Errorf("notice requires a loopback HTTP control URL")
	}
	endpoint.Path = agentapi.NoticePath
	return endpoint.String(), nil
}
