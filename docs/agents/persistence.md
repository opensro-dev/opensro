# Persistence

- **review** A new saved character field gets its own store schema step: a version bump and an offline upgrade.
- **review** Never take a store door inside another door. Nested locks deadlock.
- **review** Measure before fixing an audit finding. A path that "may write" is not proof that it writes.
- **review** A change that skips writing a section must keep the dirty marks complete: check every direct writer, or
  a skipped section loses a write.
- **review** A hashed fixture or source pins exact bytes. When line endings or imports change it, prove the change is
  mechanical before updating the hash.
- **review** Tests that need game data call the licensed-data check first, because CI has no game data.
