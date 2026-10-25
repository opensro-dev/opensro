# Transport smoke and measurement probe

Run this command from the repository root. The default is the existing
WebTransport certificate-discovery and HELLO/WELCOME smoke:

```sh
go -C apps/server run ./cmd/operations/sro-wtprobe -url https://play.example.net
```

Mint a fresh admission ticket for a designated probe account through the
Agent and put it in `SRO_WT_PROBE_TICKET`, or name its environment variable
with `-ticket-env`. A successful WELCOME consumes the ticket. Each arm
needs another ticket; the command opens one connection per invocation.
Concurrent load sessions need distinct accounts because ordinary admission
replaces an account's previous session.

`-transport ws` selects WebSocket. `-wt-addr host:port` selects a different
UDP endpoint for WT. `-ca-cert bundle.pem` adds development roots to HTTPS,
WSS and QUIC verification. WT additionally requires the UDP certificate to
match the leaf hash advertised by `/transport/cert-hash`.

## One measurement arm

```sh
mkdir -p .state/transport-probe
go -C apps/server run ./cmd/operations/sro-wtprobe \
  -url https://play.example.net -transport ws -measure \
  > .state/transport-probe/ws-01.json
```

For the WT arm, supply a new ticket and use `-transport wt` with a different
output filename. The default schedules are:

| Schedule | Samples | Send interval |
| --- | ---: | ---: |
| Ping train | 1,200 | 100 ms |
| Ordered burst | 200 | 50 ms |

`-samples`, `-ping-interval`, `-burst` and `-burst-interval` change those
schedules. Sending follows its own clock while a reader collects replies
and answers server keepalives. `-timeout` is the smoke budget, or the
measurement handshake, HTTP and response-drain timeout. The command derives
an overall measurement deadline from both schedules and those stage budgets.

The paced burst fits the server's current default admission budget of 25
frames/s with a 75-frame bucket. `-burst-interval 0` explicitly requests an
unpaced arm. Use that with an isolated measurement fixture whose configured
admission budget is recorded alongside the result. An immediate 200-frame
burst exceeds the default bucket; the report keeps both missing replies and
the `rate_limited_frames` delta and marks that arm invalid.

## Reading the JSON

The report contains public settings, raw sample offsets and one
HELLO-to-WELCOME observation. `handshake_samples` is one after admission;
multiple independent arms are needed to build a handshake distribution.

- Fields ending in `_ns` are nanoseconds. Divide by 1,000,000 for milliseconds.
- RTT percentiles use nearest rank, `ceil(percentile * unique_reply_count)`.
  Duplicate responses contribute no extra RTT sample. `received: 0` means
  no latency sample exists, even though numeric percentile fields read zero.
- Jitter is the mean absolute difference between successive RTTs divided
  by two, in unique arrival order: the one-way-equivalent change in spacing.
- Receipt stalls are gaps strictly greater than 250 ms. The first gap starts
  at the phase's start; later gaps are between unique receipts. Intentional
  pauses between phases do not become stalls. A missing final reply stays
  missing rather than creating an inferred arrival or stall duration.
- Each phase reports requested, registered, sent, received, missing and
  not-sent counts. Duplicate and out-of-order replies remain separate.
  `reordered_replies` also catches replies crossing phase boundaries.
- `metrics.before` and `metrics.after` retain the endpoint observations.
  `counter_deltas` contains monotonic counters only. A decrease or invalid
  histogram fails the comparison and preserves both snapshots.
- Queue high-water marks, current gauges and runtime percentiles stay as
  snapshots. Runtime scheduler/GC percentiles describe the process lifetime.
  `tick_window` uses the transport owner's bucket calculation; its open last
  bucket uses the later lifetime tick maximum. These scopes are explicit in
  the report and are not exact sample maxima for the measured interval.

Missing, duplicate, reordered or unmatched replies, transport failures,
increased server rate-limit/write-error/empty-frame/slow-consumer counters,
counter resets, an absent selected-transport handshake in the metrics window,
and incomplete metric captures make `valid` false. An
incomplete second scrape retains `metrics_before_if_incomplete` instead of
presenting a zero-filled second observation. Tickets never enter the JSON.

Exit status is 0 for a successful smoke or valid measurement. Flag parsing,
missing required inputs and invalid measurement schedules exit 2; remaining
configuration, connection and arm failures exit 1. A measurement arm writes
its JSON even on failure; diagnostics go to stderr.

## Evidence scope

This is the protocol-probe foundation of #1450. Its loopback tests verify
socket admission, framing, scheduling and error accounting. For a transport
default decision, combine the raw arms with actual browser runs, Linux
netem, remote-play traces, process CPU/load evidence and the fallback drill
specified in #1450. Record the server build, origin, configured budgets and
fixture for every arm, and use the repository's controlled comparison order.
`valid` describes collection correctness; the switching thresholds are a
separate decision based on the complete evidence pack.
