/*
===========================================================================

bot.go - one load bot: sign in, enter the world, stay and play

Every stage is the production path and every failure keeps its stage
name, so the report can name why a bot never entered instead of
shrinking the denominator. A bot that enters holds its session for the
step, answers keepalives, acknowledges its stream position, and counts
what the server sends it.

===========================================================================
*/

package main

import (
	"context"
	"fmt"
	"time"

	"opensro.online/server/internal/transport"
)

// opGameReady ends the client-side loading state server-side; action
// handlers refuse requests until it arrives. It is the enterworld
// package's OpcodeGameReady, mirrored here so the load tool stays a
// transport-level client.
const opGameReady uint16 = 0x3012

// botAccount is one seeded load account and the character it enters with
// (empty: the account's first character).
type botAccount struct {
	ID        string `json:"id"`
	Password  string `json:"password"`
	Character string `json:"character"`
}

// botResult is one bot's row in the step report. FailedStage is empty
// only for a bot that entered and held.
type botResult struct {
	Account     string    `json:"account"`
	Character   string    `json:"character"`
	FailedStage string    `json:"failedStage,omitempty"`
	Error       string    `json:"error,omitempty"`
	EnterMillis int64     `json:"enterMillis,omitempty"`
	FramesIn    uint64    `json:"framesIn"`
	BytesIn     uint64    `json:"bytesIn"`
	HeldSeconds float64   `json:"heldSeconds,omitempty"`
	EnteredAt   time.Time `json:"enteredAt,omitempty"`
}

/*
================
runBot

Sign in, take both tickets, connect, enter the world, then hold until
the step context ends. The returned row always names the bot; a failure
names its stage.
================
*/
func runBot(ctx context.Context, cfg runConfig, acct botAccount) botResult {
	row := botResult{Account: acct.ID, Character: acct.Character}
	fail := func(stage string, err error) botResult {
		row.FailedStage = stage
		row.Error = err.Error()
		return row
	}
	started := time.Now()

	agent, err := newAgentClient(cfg.agentURL, cfg.serverID, cfg.division, cfg.timeout)
	if err != nil {
		return fail("agent-client", err)
	}
	if err := agent.login(acct.ID, acct.Password); err != nil {
		return fail("login", err)
	}
	if row.Character == "" {
		names, err := agent.characterNames()
		if err != nil {
			return fail("character-list", err)
		}
		if len(names) == 0 {
			return fail("character-list", fmt.Errorf("account %s has no characters on division %s", acct.ID, cfg.division))
		}
		row.Character = names[0]
	}
	ticket, err := agent.transportToken()
	if err != nil {
		return fail("transport-token", err)
	}
	conn, _, err := dialGame(ctx, cfg.gameURL, ticket, cfg.timeout)
	if err != nil {
		return fail("connect", err)
	}
	defer conn.close()

	bind, err := agent.enterWorldToken(row.Character)
	if err != nil {
		return fail("enterworld-token", err)
	}
	if err := conn.writeFrame(transport.Frame{
		Opcode: transport.OpEnterWorld,
		Payload: transport.EncodeEnterWorld(transport.EnterWorld{
			Division:  cfg.division,
			CharName:  row.Character,
			AuthToken: []byte(bind),
		}),
	}); err != nil {
		return fail("enterworld-send", err)
	}

	go conn.readLoop()
	go conn.ackLoop(ctx)

	// The enter-world answer arrives in order among the bootstrap
	// frames; everything before it is world state the bot counts.
	entered := false
	for !entered {
		select {
		case <-ctx.Done():
			return fail("enterworld", ctx.Err())
		case frame, ok := <-conn.frames:
			if !ok {
				return fail("enterworld", fmt.Errorf("connection closed before the enter-world answer"))
			}
			row.FramesIn++
			if frame.Opcode == transport.OpPing {
				_ = conn.writeFrame(transport.Frame{Opcode: transport.OpPong, Payload: frame.Payload})
				continue
			}
			if frame.Opcode != transport.OpEnterWorldResult {
				continue
			}
			result, err := transport.DecodeEnterWorldResult(frame.Payload)
			if err != nil {
				return fail("enterworld", err)
			}
			if !result.OK {
				return fail("enterworld", fmt.Errorf("refused with native error code %d", result.NativeErrorCode))
			}
			entered = true
		}
	}
	if err := conn.writeFrame(transport.Frame{Opcode: opGameReady}); err != nil {
		return fail("game-ready", err)
	}
	row.EnteredAt = time.Now()
	row.EnterMillis = time.Since(started).Milliseconds()

	// Hold: drain and count until the step ends or the socket dies.
	holdStarted := time.Now()
	for {
		select {
		case <-ctx.Done():
			row.HeldSeconds = time.Since(holdStarted).Seconds()
			row.BytesIn = conn.bytesIn
			return row
		case frame, ok := <-conn.frames:
			if !ok {
				row.HeldSeconds = time.Since(holdStarted).Seconds()
				row.BytesIn = conn.bytesIn
				return fail("hold", fmt.Errorf("connection closed after %.1fs in world", row.HeldSeconds))
			}
			row.FramesIn++
			if frame.Opcode == transport.OpPing {
				_ = conn.writeFrame(transport.Frame{Opcode: transport.OpPong, Payload: frame.Payload})
			}
		}
	}
}
