# Fortress tax collection

Issue #481 adds the missing server handler for `0x71E1` action 2. The client
already encodes this request and decodes its response. Merchant tax accumulation
(#480) and the tax-management window (#484) are separate work.

## Native contract

The owner's v1.188 GameServer has SHA-256
`bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`;
the v1.150 client has SHA-256
`375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a`.
Addresses below are preferred virtual addresses, image base `0x400000`.

| Decision | Instruction evidence | Port |
| --- | --- | --- |
| Signed request amount | Server `62F808–62F833`: negative high word floors to zero; upper comparison is `INT64_MAX` | Decode an `int64`; floor negative values at zero |
| Refusal order | `62F83B–62F8C8`: period, fortress lookup, holder guild, master, queue failure | Low-byte codes 8, 3, 6, 7, 2 |
| Current holder | `61D291–61D2B4`: nonzero temporary guild at `+0x34`, otherwise occupying guild at `+0x1C` | Check temporary holder before permanent holder |
| Durable operation | `621379–621387`: queue tax collection with the amount and fortress identity | One transaction for character and fortress records |
| Successful completion | `623481–6234A4`: subtract the query amount from tax; `6234D5–6234F4`: credit character | Publish both in-memory balances only after commit |
| Success reply | `623509–623544`: action 2, result 1, eight-byte amount; client `754A40` case 2 reads signed 64-bit amount | `0xB1E1 [2, 1, i64 collected]`, following the gold refresh |
| Gold notification | PC vtable `AF59FC + 16C` resolves to `4E4B60`; `4E4BD7–4E4BE1` forwards the final argument to `4E3FB0`; `4E4016–4E401D` writes it after the balance | Nonzero credit refreshes gold with `Notify=false`; zero credit sends only tax success |
| Refusal reply | Client `754A40` reads one byte after result 2 and selects notice category `0x1E` | `0xB1E1 [2, 2, u8 code]`; do not copy the newer server's two-byte error |

The shared manager path still checks selected NPC, manager service and distance
before dispatch. Production wiring already restores the fortress authority from
the store door; that door now implements the tax transaction as well.

## SQL boundary and persistence

The executable queues `_SiegeFortressTaxCollect`. The procedure recovered from
the owner's donor backup refuses requests larger than the treasury with status
`-1002`, before starting its transaction. Otherwise it debits the full request
and adds the same amount to the character's `BIGINT` balance. Native query
execution `457530` case 2 checks the returned status; it does not replace the
request amount. Failure dispatch `626180` case 2 maps the failure to error 2.
Accordingly, the port refuses over-amount requests without changing either
balance. Negative requests become successful zero withdrawals after the normal
authorization checks. The explicit overflow check preserves the signed balance
range without relying on a failing SQL arithmetic expression.

The SQL source is donor evidence, not proof of the deployed retail database.
Backup SHA-256 is
`9b9179e598f303f3293df1771ed7a9339a5e9c9de0dd2401884db2817d5dd9a4`;
the 718-byte extraction at byte offset 14,474,607 hashes to
`3123aac04528cfdf79b04162b61aebc02bfa44b0d6af63a06ba7c0be57cadd2a`.
The original spelling, including a `ROMLBACK` typo in its failure branch, is
preserved in the research record; the procedure was not executed. The port uses
an ordinary Go SQL transaction and rolls back on either write failure.

Raw instruction bytes, the SQL extraction and helper-label port statuses are
saved under the external research directory
`investigations/fortress-tax-481-sidecar/`. Every saved instruction was compared
with the owner's corresponding executable, with zero mismatches. No research
file is a product runtime dependency.

The fortress lock spans period admission and the store transaction. The store
lock covers current guild membership and the character balance. Copies are
written first; neither live balance changes if either write or commit fails.
The ordinary tax query reads this same authority balance, so a successful
withdrawal is immediately reflected in the next query.

## Validation scope

The store tests cover signed limits, partial and over-treasury requests, refusal
precedence, temporary holders, concurrent withdrawals, and reopening persisted
state. A SQL trigger verifies that the character credit occurred inside the
transaction before aborting the treasury write; neither balance may escape that
rollback. Runtime tests exercise the actual manager packet path and wire replies.

This is a bounded server port, not a claim that the fortress UI and commerce
tax accumulation are complete. No production deployment or original-client
live-session comparison is included.
