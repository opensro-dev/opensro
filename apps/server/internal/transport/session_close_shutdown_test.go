/*
===========================================================================

session_close_shutdown_test.go - shutdown owns hooks across session retirement

The retirement log is a barrier after Done and registry removal, before hook
execution. Shutdown must join both that gap and the hooks that follow it.

===========================================================================
*/
package transport

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/testsupport/wait"
)

/*
================
retirementLogBarrier
================
*/
type retirementLogBarrier struct {
	session uint64
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

/*
================
Levels
================
*/
func (*retirementLogBarrier) Levels() []log.Level { return []log.Level{log.InfoLevel} }

/*
================
Fire

Pause only the selected session's final retirement event, outside transport locks.
================
*/
func (b *retirementLogBarrier) Fire(entry *log.Entry) error {
	if entry.Message == "transport: session closed" && entry.Data["session"] == b.session {
		b.once.Do(func() {
			close(b.entered)
			<-b.release
		})
	}
	return nil
}

/*
================
TestShutdownJoinsRetiredSessionHooks

Both inline and send-triggered retirement publish Done before their hooks start.
An expired drain context must not release the store owner while those hooks run.
================
*/
func TestShutdownJoinsRetiredSessionHooks(t *testing.T) {
	const timeout = 5 * time.Second
	const blockedWindow = 50 * time.Millisecond
	for _, fromSend := range []bool{false, true} {
		name := "synchronous"
		if fromSend {
			name = "send"
		}
		t.Run(name, func(t *testing.T) {
			cfg := testCfg()
			cfg.OutboundQueue = 1
			hub := newHub(cfg)
			session, err := hub.createSession()
			if err != nil {
				t.Fatal(err)
			}
			barrier := &retirementLogBarrier{session: session.ID, entered: make(chan struct{}), release: make(chan struct{})}
			logger := log.StandardLogger()
			previousHooks := logger.ReplaceHooks(make(log.LevelHooks))
			previousLevel := logger.GetLevel()
			logger.SetLevel(log.InfoLevel)
			logger.AddHook(barrier)
			t.Cleanup(func() {
				logger.ReplaceHooks(previousHooks)
				logger.SetLevel(previousLevel)
			})
			var releaseRetirement, releaseHook sync.Once
			hookEntered, hookRelease, hookDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			hub.OnSessionClose(func(*Session, error) {
				calls.Add(1)
				close(hookEntered)
				<-hookRelease
				close(hookDone)
			})
			closeDone := make(chan struct{})
			go func() {
				defer close(closeDone)
				if fromSend {
					_ = session.SendBatch([]Frame{{Opcode: 0x3417}, {Opcode: 0x3417}})
				} else {
					hub.closeSession(session, nil)
				}
			}()
			shutdownDone := make(chan struct{})
			t.Cleanup(func() {
				releaseRetirement.Do(func() { close(barrier.release) })
				releaseHook.Do(func() { close(hookRelease) })
				wait.Eventually(t, timeout, "close and hook completion", func() bool {
					select {
					case <-closeDone:
					default:
						return false
					}
					select {
					case <-hookDone:
						return true
					default:
						return false
					}
				})
			})
			wait.Eventually(t, timeout, "retirement barrier", func() bool {
				select {
				case <-barrier.entered:
					return true
				default:
					return false
				}
			})
			select {
			case <-session.Done():
			default:
				t.Fatal("retirement did not publish Done")
			}
			hub.mu.RLock()
			registered := len(hub.sessions)
			hub.mu.RUnlock()
			if registered != 0 {
				t.Fatal("retired session remains registered")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			go func() {
				hub.shutdown(ctx)
				close(shutdownDone)
			}()
			t.Cleanup(func() {
				releaseRetirement.Do(func() { close(barrier.release) })
				releaseHook.Do(func() { close(hookRelease) })
				wait.Eventually(t, timeout, "shutdown completion", func() bool {
					select {
					case <-shutdownDone:
						return true
					default:
						return false
					}
				})
			})
			wait.Eventually(t, timeout, "shutdown closes admission", func() bool {
				hub.mu.RLock()
				defer hub.mu.RUnlock()
				return hub.closed
			})
			wait.Consistently(t, blockedWindow, "shutdown waits before hook dispatch", func() bool {
				select {
				case <-shutdownDone:
					return false
				default:
					return true
				}
			})
			releaseRetirement.Do(func() { close(barrier.release) })
			wait.Eventually(t, timeout, "hook entry", func() bool {
				select {
				case <-hookEntered:
					return true
				default:
					return false
				}
			})
			hub.closeSession(session, nil)
			hub.closeSessionFromSend(session, nil)
			wait.Consistently(t, blockedWindow, "shutdown waits for hook completion", func() bool {
				select {
				case <-shutdownDone:
					return false
				default:
					return true
				}
			})
			releaseHook.Do(func() { close(hookRelease) })
			wait.Eventually(t, timeout, "shutdown completes after hooks", func() bool {
				select {
				case <-shutdownDone:
					return true
				default:
					return false
				}
			})
			if calls.Load() != 1 {
				t.Fatalf("close hooks ran %d times", calls.Load())
			}
		})
	}
}
