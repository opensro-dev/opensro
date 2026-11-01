# Persistence

- **gate** A new saved field on a stored record gets its own store schema step: a version bump and an offline upgrade.
  `TestRecordFieldsBelongToTheCurrentSchema` fails without one.
- **review** Never take a store door inside another door. Nested locks deadlock.
- **review** Measure before fixing an audit finding. A path that "may write" is not proof that it writes.
- **review** A change that skips writing a section must keep the dirty marks complete: check every direct writer, or
  a skipped section loses a write.
- **review** A hashed fixture or source pins exact bytes. When line endings or imports change it, prove the change is
  mechanical before updating the hash.
- **review** Tests that need game data call the game-data check first, because CI has no game data.
