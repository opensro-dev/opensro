/*
===========================================================================

releaseprotocol.go - the one version a browser and a server must agree on

A release protocol names the complete set of browser-facing wire contracts
(the EnterWorld DTO, the character list). This package owns those contract
versions; their encoders read them from here, so a wire change cannot
happen without a new release protocol.

Every browser request declares the protocol it was built for in the
X-OpenSRO-Protocol header. A server serves Oldest through Current and
refuses any other with 426 Upgrade Required and the protocol it speaks; the
browser then offers the newer release instead of failing inside a decoder.
Release admission publishes a protocol change only as one coordinated
server and client release, or as a server that still serves the live
browser's protocol (Oldest) followed by the browser.

Transport sessions need no second check: their admission tokens are minted
only by requests that passed this one, and the EnterWorld token carries the
declared protocol into the session (auth.MintProtocol), so the encoders that
differ between protocols pick the session's contract.

This package imports nothing from the module, so every owner can use it.

===========================================================================
*/
package releaseprotocol

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Header carries the release protocol a browser was built for.
const Header = "X-OpenSRO-Protocol"

// Contracts are the browser-facing wire contracts one protocol fixes.
/*
================
Contracts
================
*/
type Contracts struct {
	Companions int // native populated summoner items and concurrent COS lifetimes
	Bootstrap  int // the EnterWorld DTO
	Roster     int // the character list
	References int // the public reference file beside the transport
}

// Current is the newest release protocol this build speaks, and the contract
// versions it fixes. Encoders version their payloads from these.
//
// Oldest is the oldest protocol it still serves. Protocol 6 changed only the
// reference file (contract 3), so a server that also publishes the contract-2
// file and the login monster rows serves protocol-5 tabs too. That lets a
// server carrying a store upgrade ship alone while protocol-5 tabs are live
// (a coordinated pair may not change the store). Raise Oldest once every
// browser runs 6.
const (
	Oldest             = 5
	Current            = 6
	CompanionsContract = 1
	BootstrapContract  = 2 // the EnterWorld DTO
	RosterContract     = 2 // the character list: worn items as (RefItemID, plus)
	// The reference file: 3 adds every creatable monster's row
	// (refObjSnapshot, #369); 2 published the static item rows a login used
	// to repeat (refItemSnapshot); 1 held skills and item commands only.
	ReferencesContract = 3
)

// history names every protocol a release has shipped. A browser and a
// server agree only when they speak the same one.
var history = map[int]Contracts{
	2:       {Bootstrap: 2, Roster: 1, References: 1},
	3:       {Bootstrap: 2, Roster: 2, References: 1},
	4:       {Bootstrap: 2, Roster: 2, References: 2},
	5:       {Bootstrap: 2, Roster: 2, References: 2, Companions: 1},
	Current: {Bootstrap: BootstrapContract, Roster: RosterContract, References: ReferencesContract, Companions: CompanionsContract},
}

/*
================
ContractsOf

The contracts a released protocol fixes.
================
*/
func ContractsOf(protocol int) (Contracts, bool) {
	contracts, ok := history[protocol]
	return contracts, ok
}

/*
================
Supported

Whether this build serves a protocol: Oldest through Current, each one a
release shipped (history).
================
*/
func Supported(protocol int) bool {
	_, shipped := history[protocol]
	return shipped && protocol >= Oldest && protocol <= Current
}

/*
================
Declared

The protocol a request declares, when it is one this build serves.
================
*/
func Declared(r *http.Request) (int, bool) {
	declared, err := strconv.Atoi(r.Header.Get(Header))
	if err != nil || !Supported(declared) {
		return 0, false
	}
	return declared, true
}

/*
================
Require

Refuses a browser request whose declared protocol this build does not
serve. CORS preflights pass untouched: they carry no declaration and no
application state, and the browser sends the declared request next.
================
*/
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := Declared(r); !ok {
			refuse(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

/*
================
refuse

426 with a body naming the protocol this server speaks.
================
*/
func refuse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUpgradeRequired)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": "client-outdated", "protocol": Current})
}
