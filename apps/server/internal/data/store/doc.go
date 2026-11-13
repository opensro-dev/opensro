// Package store owns durable gameplay authority: characters, inventory,
// position, social records, mail, guilds, training camps, ground items, and
// allocation watermarks.
//
// # Persistence model
//
// Authority lives in one SQLite database using WAL mode. The process keeps the
// live object graph in memory and advances its durable shadow through explicit
// mutation doors. Each accepted gameplay operation is one transaction: a
// multi-character friend operation or a character-plus-ground item move is
// wholly committed or wholly absent after a crash.
//
// Scoped doors persist only the records they declare. The general Mutate door
// conservatively marks the complete world dirty, which is slower but prevents
// an unclassified mutation from being lost. Ground snapshots persist only when
// their registry revision changes.
//
// # Ownership and locking
//
// Store owns database lifecycle, transaction lifecycle, dirty tracking, and
// the canonical persisted object pointers. Gameplay owns behavior and enters
// through narrow domain interfaces.
//
// The lock order is:
//
//	lane.mu (action or movement) -> store.mu -> registry-local mutex
//
// A mutation closure must not call Store accessors: Store's mutex is not
// reentrant. Store never calls transport, session, hub, or handler APIs.
//
// # Startup and failure
//
// Production startup accepts CurrentVersion and CurrentLayoutVersion, and
// the StoreReadHeadroom newer versions of each (0 today: exactly the
// current ones). A newer store's stamps are never lowered. Incompatible
// pre-release stores are replaced through explicit initialization; startup never transforms data in place. Live
// record JSON is decoded strictly, then independently stored records are
// validated as one authority graph before any pointer becomes visible.
//
// Corrupt databases are quarantined with their WAL sidecars and recovered from
// one checkpointed backup generation when possible. An incompatible schema is
// healthy but unusable by this binary, so it stays in place and startup
// refuses without falling back to an older generation.
//
// Runtime write failures are fail-open and loud: the in-memory operation
// remains applied, health degrades, and dirty markers make the next successful
// commit heal the durable state.
package store
