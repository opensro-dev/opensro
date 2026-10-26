# sro-wtfixture — loopback transport fixture

`sro-wtprobe -measure` speaks to a live GameWorld's transport edge. This
fixture stands the same production transport server
(`internal/transport`) up on loopback with one fixture attachment — an
admission verifier that accepts a single documented ticket — so the
probe's WebSocket and WebTransport measurement arms can run against an
isolated server with a recorded admission budget: no live shard, no
authority store, no browser.

Start it (run from `apps/server`):

```powershell
go run ./cmd/operations/sro-wtfixture -certs .state/wtfixture-certs
```

It prints the WebSocket address, the WebTransport address, the
certificate file it generated, and the ticket. Drive it (same module,
once per arm):

```powershell
$env:SRO_WT_PROBE_TICKET = 'loopback-fixture-ticket'
go run ./cmd/operations/sro-wtprobe -url http://127.0.0.1:18087 -transport ws -measure
go run ./cmd/operations/sro-wtprobe -url http://127.0.0.1:18087 -transport wt -wt-addr 127.0.0.1:18088 -ca-cert .state/wtfixture-certs/wt-dev-cert.pem -measure
```

The fixture ticket is reusable; production tickets are Agent-minted and
consumed by the WELCOME. The certificate is the transport's managed
self-signed development pair — the probe pins it through the served
cert-hash and trusts it through `-ca-cert`; there is no skip-verify
mode. The fixture answers HELLO/WELCOME and the probe's ping train; it
enters no world and runs no game logic.

Note the admission budget in the fixture header before quoting a
measurement: the ping train and burst must fit inside the transport's
inbound admission (the probe's defaults do).
