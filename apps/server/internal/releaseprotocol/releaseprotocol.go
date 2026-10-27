/*
===========================================================================

releaseprotocol.go - the one version a browser and a server must agree on

A release protocol names the complete set of browser-facing wire contracts
(the EnterWorld DTO, the character list). This package owns those contract
versions; their encoders read them from here, so a wire change cannot
happen without a new release protocol.

Every browser request declares the protocol it was built for in the
X-OpenSRO-Protocol header. A server refuses any other with 426 Upgrade
Required and the protocol it speaks; the browser then offers the newer
release instead of failing inside a decoder. Release admission publishes a
protocol change only as one coordinated server and client release.

Transport sessions need no second check: their admission tokens are minted
only by requests that passed this one.

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
	Effects    int // the attached-effect stream the transport sends
	Friends    int // the friend request's prompt and consent
	Notices    int // the port-only 0x3667 notice channels
	Movement   int // the 0xB738 movement acknowledgement's source block
}

// Current is the release protocol this build speaks, and the contract
// versions it fixes. Encoders version their payloads from these.
const (
	Current = 11
	// The movement contract: 1 carries the 0xB738 acknowledgement's
	// source block as [f32 x][f32 y][f32 z] on every floor (#1744). The
	// retail i16 x10 source words cap at +-3276.7 units, which the
	// port's one-frame tomb floors (0x8002-0x8006) overflow, so the
	// block was simply omitted there and viewers saw the walker frozen
	// until the next settle. The owner waived byte-parity for this one
	// block (option 5): a protocol-10 client reads the longer block as
	// a malformed ack and drops every walk, which is exactly what the
	// bump exists to reload away from.
	MovementContract = 1
	// The notice contract: 1 carries the job-kill announcement on 0x3667
	// channel 0x80 (#1011) and the seal-drop and +8..+11 announcements on
	// 0x81 (#1019), laid out like channel 7 so a player can hide them; 0x81
	// also carries the player and item it names after its text (#1068). A
	// protocol-9 client decodes the packet but drops both channels
	// (decodeChatBroadcast returns null), so its player never sees them;
	// the bump (F9) sends every open tab the 426 that reloads it onto a
	// client that shows them.
	NoticesContract = 1
	// The friend contract: 1 asks the target first, as the original does
	// (#965/#966): S->C 0x7164 prompts the target, its C->S 0x3012 {jid,
	// serial, accept} answers, and 0xB164/0xB5DB carry the results. A
	// protocol-8 client decodes nothing wrong but ignores the prompt, so
	// every request to it waits out the 30 s window and cancels. The owner
	// made that a protocol change (2026-10-14, F8): the bump sends every
	// open tab the 426 that reloads it onto a client that can answer.
	FriendsContract = 1
	// The attached-effect contract: 1 sends every heal, cure and revival
	// recipient a token-0 0xB419 (the server's attached-effect send). No
	// B6A0 ever ends one, so a client must retire it itself; a protocol-7
	// client keeps every one and ends its session at 2048 rows. It decodes
	// the packet but cannot sustain the stream, which is a protocol change
	// all the same.
	EffectsContract = 1
	// The companion contract: 2 lifts the COS bag capacity bound to its
	// one wire byte (#801), so an item-mall transport's 141 to 177 slots
	// decode; 1 added populated summoners and concurrent COS lifetimes and
	// refused a bag over 140.
	CompanionsContract = 2
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
	2:  {Bootstrap: 2, Roster: 1, References: 1},
	3:  {Bootstrap: 2, Roster: 2, References: 1},
	4:  {Bootstrap: 2, Roster: 2, References: 2},
	5:  {Bootstrap: 2, Roster: 2, References: 2, Companions: 1},
	6:  {Bootstrap: 2, Roster: 2, References: 3, Companions: 1},
	7:  {Bootstrap: 2, Roster: 2, References: 3, Companions: 2},
	8:  {Bootstrap: 2, Roster: 2, References: 3, Companions: 2, Effects: 1},
	9:  {Bootstrap: 2, Roster: 2, References: 3, Companions: 2, Effects: 1, Friends: 1},
	10: {Bootstrap: 2, Roster: 2, References: 3, Companions: 2, Effects: 1, Friends: 1, Notices: 1},
	Current: {Bootstrap: BootstrapContract, Roster: RosterContract, References: ReferencesContract, Companions: CompanionsContract,
		Effects: EffectsContract, Friends: FriendsContract, Notices: NoticesContract, Movement: MovementContract},
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
Require

Refuses a browser request whose declared protocol is not Current. CORS
preflights pass untouched: they carry no declaration and no application
state, and the browser sends the declared request next.
================
*/
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		declared, err := strconv.Atoi(r.Header.Get(Header))
		if err != nil || declared != Current {
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
