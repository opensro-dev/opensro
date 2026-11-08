/*
===========================================================================

prebootstrap_receipt_hub_test.go - nothing precedes the bootstrap

===========================================================================
*/
package enterworld_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

/*
================
TestReceiptDuringBuildLandsBehindTheBootstrap

Drives a private receipt (a vitals refresh, the #2872 frame) into the
entering session while its bootstrap is still being built: the entry
claim runs inside Build, which is where a producer resolving the
just-claimed session delivers. The client applies the wire in order,
so the receipt must land behind the EnterWorld answer; ahead of it
the client refuses it ("Native packet 0x33a6 before EnterWorld").
================
*/
func TestReceiptDuringBuildLandsBehindTheBootstrap(t *testing.T) {
	t.Parallel()
	character := &enterworld.Character{ID: 11, Name: "EarlyReceipt", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	deps := &enterworld.Deps{
		Roster:            &enterworld.Roster{},
		Characters:        source,
		Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "missing-textdata")),
		ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
		PlayerBaseStats: func(*enterworld.Character) (wire.BaseStats, error) {
			return wire.BaseStats{}, nil
		},
	}
	srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), GracePeriod: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	authenticatedSession := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	deps.AdmitCharacterSession = func(divisionID, characterName string, sessionID uint64) error {
		// The claim runs inside Build: a producer that resolves the
		// just-claimed session delivers its receipt now, while the
		// bootstrap is still being built.
		sess, ok := srv.Hub.Session(sessionID)
		if !ok {
			return nil
		}
		revision, valid := sess.SceneReceiptRevision()
		if !valid {
			return nil
		}
		return sess.SendSceneReceiptBatch(revision, []transport.Frame{
			{Opcode: enterworld.OpcodeVitalsUpdate, Payload: []byte{1, 2, 3}},
		})
	}
	enterworld.Register(srv.Hub, deps)
	conn := newFakeConn()
	go srv.Hub.AcceptConn(conn)
	conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test-admission")})}
	waitFor(t, "WELCOME", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode == transport.OpWelcome {
				return true
			}
		}
		return false
	})
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(
		authenticatedSession.Entry("0", character.Name),
	)}
	waitFor(t, "the receipt behind the bootstrap", func() bool {
		for _, frame := range conn.frames() {
			if frame.Opcode == enterworld.OpcodeVitalsUpdate {
				return true
			}
		}
		return false
	})
	answerAt, receiptAt := -1, -1
	for i, frame := range conn.frames() {
		if frame.Opcode == transport.OpEnterWorldResult && answerAt < 0 {
			answerAt = i
		}
		if frame.Opcode == enterworld.OpcodeVitalsUpdate && receiptAt < 0 {
			receiptAt = i
		}
	}
	if answerAt < 0 || receiptAt < 0 {
		t.Fatalf("answer at %d, receipt at %d in %v", answerAt, receiptAt, conn.frames())
	}
	if receiptAt < answerAt {
		t.Fatalf("receipt 0x33a6 reached the client at %d, ahead of the EnterWorld answer at %d", receiptAt, answerAt)
	}
}
