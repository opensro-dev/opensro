package enterworld_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

func TestWorldAdmissionFailureRetiresOnlyAClaimedMembership(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(map[bool]string{false: "projection-fails", true: "membership-refused"}[refuse], func(t *testing.T) {
			srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), GracePeriod: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				srv.Shutdown(ctx)
			}()
			srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
				return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
			})
			authenticated := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
			c := &enterworld.Character{ID: 3, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
			source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {c}}
			retired := make(chan uint64, 1)
			deps := &enterworld.Deps{
				Roster: &enterworld.Roster{}, Characters: source,
				Items:             enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "absent")),
				ResolveDivisionID: enterworld.DevResolveDivisionIDFromCatalog(source),
				AdmitCharacterSession: func(string, string, uint64) error {
					if refuse {
						return errors.New("full")
					}
					return nil
				},
				RetireCharacterSession: func(division, name string, session uint64) { retired <- session },
				EntrySkills:            func(string, string) []enterworld.EntrySkill { return []enterworld.EntrySkill{{ID: 0}} },
			}
			enterworld.RegisterEnterWorld(srv.Hub, deps)
			conn := newFakeConn()
			go srv.Hub.AcceptConn(conn)
			conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test")})}
			waitFor(t, "welcome", func() bool {
				for _, f := range conn.frames() {
					if f.Opcode == transport.OpWelcome {
						return true
					}
				}
				return false
			})
			conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(authenticated.Entry("0", c.Name))}
			waitFor(t, "refusal", func() bool {
				for _, f := range conn.frames() {
					if f.Opcode == transport.OpEnterWorldResult {
						r, e := transport.DecodeEnterWorldResult(f.Payload)
						if e != nil || r.OK {
							t.Fatal("invalid refusal", r, e)
						}
						return true
					}
				}
				return false
			})
			waitFrameQuiescence(t, "admission return", conn)
			if !refuse {
				select {
				case <-retired:
				default:
					t.Fatal("failed projection leaked membership")
				}
			} else {
				select {
				case <-retired:
					t.Fatal("refused claim retired an existing owner")
				default:
				}
			}
			for _, s := range srv.Hub.Sessions() {
				if _, _, bound := s.CharacterBinding(); bound {
					t.Fatal("failed bootstrap bound an actor")
				}
			}
		})
	}
}

/*
================
TestReplacementBootstrapFailureKeepsTheLiveMembership

#3540: a session already in the world with a character gets a replacement
bootstrap (socket resume). When that bootstrap fails, the live scene's
membership, world and effects stay; retiring them dropped a speed scroll
and reseeded the world at base speed with no 0x376F, so the client kept
running at the scroll's speed while the server timed paths at 50.
================
*/
func TestReplacementBootstrapFailureKeepsTheLiveMembership(t *testing.T) {
	srv, err := transport.NewServer(transport.Config{CertDir: t.TempDir(), GracePeriod: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	authenticated := entryauth.NewAuthenticatedSessionFixture(t, srv.Hub)
	c := &enterworld.Character{ID: 3, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {c}}
	retired := make(chan uint64, 2)
	var entries atomic.Int32
	deps := &enterworld.Deps{
		Roster: &enterworld.Roster{}, Characters: source,
		Items:                  enterworld.NewTextdataItems(filepath.Join(t.TempDir(), "absent")),
		ResolveDivisionID:      enterworld.DevResolveDivisionIDFromCatalog(source),
		AdmitCharacterSession:  func(string, string, uint64) error { return nil },
		RetireCharacterSession: func(division, name string, session uint64) { retired <- session },
		// The first entry projects; the replacement's projection fails.
		EntrySkills: func(string, string) []enterworld.EntrySkill {
			if entries.Add(1) == 1 {
				return nil
			}
			return []enterworld.EntrySkill{{ID: 0}}
		},
	}
	enterworld.RegisterEnterWorld(srv.Hub, deps)
	conn := newFakeConn()
	go srv.Hub.AcceptConn(conn)
	conn.inbound <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte("test")})}
	waitFor(t, "welcome", func() bool {
		for _, f := range conn.frames() {
			if f.Opcode == transport.OpWelcome {
				return true
			}
		}
		return false
	})
	results := func() (ok, refused int) {
		for _, f := range conn.frames() {
			if f.Opcode != transport.OpEnterWorldResult {
				continue
			}
			if r, e := transport.DecodeEnterWorldResult(f.Payload); e == nil && r.OK {
				ok++
			} else {
				refused++
			}
		}
		return ok, refused
	}
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(authenticated.Entry("0", c.Name))}
	waitFor(t, "first entry", func() bool { ok, _ := results(); return ok == 1 })
	waitFrameQuiescence(t, "first entry", conn)
	conn.inbound <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(authenticated.Entry("0", c.Name))}
	waitFor(t, "replacement refusal", func() bool { _, refused := results(); return refused == 1 })
	waitFrameQuiescence(t, "replacement refusal", conn)
	select {
	case session := <-retired:
		t.Fatalf("a failed replacement bootstrap retired the live membership of session %d", session)
	default:
	}
}
