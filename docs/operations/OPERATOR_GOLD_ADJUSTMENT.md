# Audited gold adjustment

The GameWorld's existing private player-operations endpoint accepts an
`action: "adjust-gold"` request (#3309). It applies one signed gold
correction to a character through the character's own mutation door, so
the economy ledger records it like every other gold change (reason
`operator_adjust`: a credit mints, a debit burns). The character may be
live or offline; a connected session is pushed its new balance. There is
no way to correct gold by editing the live store instead - the running
GameWorld would overwrite or contradict the edit.

Send `POST /internal/operations/player` to the GameWorld's loopback control
listener, with `Authorization: Bearer <operator-token>`,
`X-SRO-Local-Diagnostics: 1`, and `Content-Type: application/json`:

```json
{
  "id": "unique-request-id-at-least-16-characters",
  "operator": "local-operator",
  "character": "CodexProbe",
  "action": "adjust-gold",
  "reason": "Exploit correction: duplicated gold removed",
  "gold": -1500
}
```

The token is read from `operator-token` in the shard authority directory at
startup. Keep it private; browser origins and forwarded requests are refused.
The reason is required (5-500 characters), as for every player operation.

The amount is signed and must not be zero. A debit that would take the
balance below zero is refused whole - never clamped to a partial debit -
and the refusal reports the amount that could have applied (the whole
balance, down to exactly zero). A credit that would overflow the balance
refuses the same way. Unknown and deletion-pending characters refuse.

Every accepted adjustment writes two records: the `economy_ledger` row its
door commit produces (the signed change and the balance it left), and the
`operator-audit.jsonl` completion line, whose result names the operator,
the requested and applied amount, the balances before and after, and the
reason text. The response is the character read plus that same
`goldAdjustment` object.

The request ID is consumed by a durable audit intent before mutation.
Reusing it returns HTTP 409, including after a restart. A timeout or
persistence error must be inspected with
`GET /internal/operations/player?character=CodexProbe` and the shard's
`operator-audit.jsonl` before submitting a new ID. This is at-most-once
admission, not a promise that a failed HTTP response means no gold moved.
