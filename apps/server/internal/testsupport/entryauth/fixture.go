// Package entryauth owns explicit authenticated EnterWorld fixtures for server
// tests. It exercises the real token codec and verifier while keeping test
// security assumptions out of production composition roots.
package entryauth

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/transport"
)

const authenticatedFixtureLifetime = time.Minute

var authenticatedFixtureSecret = []byte("sro-enterworld-test-fixture-secret-v1")

// SessionFixture installs a real token verifier and mints identity-bound entry
// payloads for one test server. It is deliberately not an allow-all verifier.
type SessionFixture struct {
	t testing.TB
}

// NewAuthenticatedSessionFixture installs the explicit authenticated-entry
// contract required by a transport test server.
func NewAuthenticatedSessionFixture(t testing.TB, hub *transport.Hub) *SessionFixture {
	t.Helper()
	fixture := &SessionFixture{t: t}
	hub.SetEnterWorldAuth(func(_ *transport.Session, entry transport.EnterWorld) (bool, uint32) {
		err := auth.Verify(
			authenticatedFixtureSecret,
			string(entry.AuthToken),
			entry.Division,
			entry.CharName,
			time.Now(),
		)
		if err != nil {
			return false, auth.DenyCodeUnauthorized
		}
		return true, 0
	})
	return fixture
}

/*
================
UniqueAccountHelloAuth

A hello-auth handler that seats every connection under its own
account ("test-account-1", "test-account-2", ...). A wire fixture's
connections are distinct players, and world entry refuses a second
character of one account, so a fixture that means several players
must not hand every connection the same account. A fixture that is
about one account installs its own constant identity instead.
================
*/
func UniqueAccountHelloAuth() func([]byte) (transport.AdmissionIdentity, error) {
	var next atomic.Int64
	return func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{
			AccountID: fmt.Sprintf("test-account-%d", next.Add(1)),
			ShardID:   "global-official",
		}, nil
	}
}

/*
================
AdmissionTokenHelloAuth

A hello-auth handler that takes the account from the admission token
presented: the token is the player's credential, so a connection
re-presenting a player's token (a transport resume) lands on the same
account, and distinct players are distinct tokens. Suites whose
handshakes mint one token per player use this; suites that present a
shared token use UniqueAccountHelloAuth instead.
================
*/
func AdmissionTokenHelloAuth() func([]byte) (transport.AdmissionIdentity, error) {
	return func(token []byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: string(token), ShardID: "global-official"}, nil
	}
}

// Entry returns a token-bearing payload bound to the requested division and
// character. The transport verifier still makes the admission decision.
func (fixture *SessionFixture) Entry(divisionID, characterName string) transport.EnterWorld {
	fixture.t.Helper()
	return newAuthenticatedEntry(fixture.t, divisionID, characterName)
}

// RejectedEntry returns a well-formed token bound to a different character, so
// authentication rejection tests exercise the real forged-identity path.
func (fixture *SessionFixture) RejectedEntry(divisionID, characterName string) transport.EnterWorld {
	fixture.t.Helper()
	entry := newAuthenticatedEntry(fixture.t, divisionID, characterName+"-token-owner")
	entry.CharName = characterName
	return entry
}

// NewAuthenticatedEntryFixture creates a real post-launcher entry payload for
// direct handler tests and wire helpers whose server fixture is installed at a
// broader package-owned setup boundary.
func NewAuthenticatedEntryFixture(t testing.TB, divisionID, characterName string) transport.EnterWorld {
	t.Helper()
	return newAuthenticatedEntry(t, divisionID, characterName)
}

// NewPostAuthHandlerEntryFixture is only for direct game-handler unit tests
// that intentionally exercise malformed semantic fields after the transport
// authentication boundary. It cannot be used as a live-session credential.
func NewPostAuthHandlerEntryFixture(divisionID, characterName string) transport.EnterWorld {
	return transport.EnterWorld{
		Division:  divisionID,
		CharName:  characterName,
		AuthToken: []byte("post-auth-handler-boundary-fixture"),
	}
}

func newAuthenticatedEntry(t testing.TB, divisionID, characterName string) transport.EnterWorld {
	t.Helper()
	token, err := auth.Mint(
		authenticatedFixtureSecret,
		divisionID,
		characterName,
		time.Now().Add(authenticatedFixtureLifetime),
	)
	if err != nil {
		t.Fatalf("mint EnterWorld test fixture for %s/%s: %v", divisionID, characterName, err)
	}
	return transport.EnterWorld{
		Division:  divisionID,
		CharName:  characterName,
		AuthToken: []byte(token),
	}
}
