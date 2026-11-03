# Live item grants

The GameWorld's existing private player-operations endpoint accepts an
`action: "grant-items"` request. It adds authored items through the running
inventory authority, persists the complete batch and sends normal item
reference and inventory receipts to a connected character. Grants require
neither a service restart nor GM privileges on the recipient.

Installing a server version with this operation, or initially enabling its
operator credential, requires the normal GameWorld rollout. Subsequent grants
use the running service. Never edit the live SQLite inventory directly.

Send `POST /internal/operations/player` to the GameWorld's loopback control
listener, with `Authorization: Bearer <operator-token>`,
`X-SRO-Local-Diagnostics: 1`, and `Content-Type: application/json`:

```json
{
  "id": "unique-request-id-at-least-16-characters",
  "operator": "local-operator",
  "character": "CodexProbe",
  "action": "grant-items",
  "reason": "Inspect Star, Moon and Sun item visuals",
  "items": [
    { "codename": "ITEM_CH_BOW_02_A_RARE", "count": 1 },
    { "codename": "ITEM_CH_BOW_02_B_RARE", "count": 1 },
    { "codename": "ITEM_CH_BOW_02_C_RARE", "count": 1 }
  ]
}
```

The token is read from `operator-token` in the shard authority directory at
startup. Keep it private; browser origins and forwarded requests are refused.
The existing observatory gateway can forward the operation, but its recovery
form remains a rescue form; this change adds no grant UI.

Each batch contains 1–32 rows, each with an authored codename and a count of
1–65,535. Items use their authored stack limits and initial durability;
equipment has +0 and no fabricated magic attributes. Unknown items, deletion
pending on the character, insufficient bag capacity, and an open exchange or
stall refuse the batch without partial inventory changes.

The request ID is consumed by a durable audit intent before mutation. Reusing
it returns HTTP 409, including after a restart. A timeout or persistence error
must be inspected with `GET /internal/operations/player?character=CodexProbe`
and the shard's `operator-audit.jsonl` before submitting a new ID. The response
includes the character's inventory and storage health. This is at-most-once
admission, not a promise that a failed HTTP response means no items were added.
