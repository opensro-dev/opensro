# Release plan draft — the WebTransport release (2026-10-26 draft)

The next release is cut as soon as #1822 (WebTransport default on) merges
and the staging rehearsal passes (owner decision 2026-10-27). It switches
on #1447's public endpoint. #1820's open parts B-D do not block it.

## Switch-on: public WebTransport (#1447, #1451)

Order matters: host state first (the deploy refuses without it), posture
second, proof from outside last. All commands on the production host as
root, from the installed release controls, unless named otherwise.

1. **Preconditions.** #1822 merged (the client's default is on; without
   it this whole block is inert) and the staging rehearsal green with
   the posture on. The rehearsal runs the same host check (step 3)
   against the staging host before its deploy, exactly as production
   will.
2. **Host redirect.** The GameWorld runs unprivileged and cannot bind
   UDP 443, so the host redirects public UDP 443 to its high bind:

   ```
   python3 quic_host.py apply --port 18443
   ```

   Installs the QUIC socket-buffer sysctls, the nftables redirect
   (public UDP 443 -> 18443, IPv4 and IPv6), the boot unit that reloads
   it, and the firewall opening on 18443/udp. Idempotent; run once in
   this window. (On a host where a previous window already applied it,
   this is a no-op re-assert.)
3. **Host check.**

   ```
   python3 quic_host.py check --port 18443
   ```

   Must exit 0 and print `ready: public UDP 443 reaches port 18443`.
   Any `not ready:` line names the gap (live redirect, boot unit,
   firewall, buffers). If this fails, stop here and take the failure
   branch below — the deploy itself would refuse anyway
   (`deploy.py` re-runs this check before any token is issued).
4. **Posture in the release config** (`/etc/opensro-release/config.json`
   — never `shard-settings.json`; the deployer derives the switch from
   this file alone). All four keys, or none (a half-posture refuses the
   release before anything runs):

   ```json
   "webtransport_public": "on",
   "transport_wt_addr": "[::]:18443",
   "transport_cert_file": "<the public origin's TLS chain on this host>",
   "transport_key_file": "<its private key>"
   ```

   `[::]` is load-bearing: the redirect delivers to the receiving
   interface's address on both families, and a narrower bind drops
   players silently onto the WebSocket fallback. The GameWorld also
   refuses a non-loopback bind without the TLS pair and the public
   switch (`SRO_WEBTRANSPORT_PUBLIC`, set by the deployer from
   `webtransport_public`), so a posture mistake stops the deploy
   instead of exposing a wrong listener.
5. **Deploy** the release as usual. The deployer re-checks the host
   (step 3), then runs `sro-nomad` with `-transport-wt-addr [::]:18443`,
   the TLS pair and `-transport-wt-public-port 443`, so the advertised
   port still matches what clients dial.
6. **Proof from outside the host** (a loopback or on-host probe bypasses
   the redirect and proves only the listener; if one must run locally,
   it needs `-wt-addr` at the bind and does not count here):
   - `sro-wtprobe -health -url https://play.opensro.online`
   - With a fresh ticket: `sro-wtprobe -measure -transport wt` and
     `-transport ws` against `https://play.opensro.online`. The client
     derives its WebTransport URL from the socket's authority
     (`https://play.opensro.online/shards/global-official/transport/wt`,
     UDP 443), so a green outside WT run proves page origin -> UDP 443
     -> redirect -> `[::]:18443` end to end (the #1450 drill's topology
     finding; behind any other origin the attempt is refused pre-QUIC
     and the client silently stays on the socket).
   - Corroborate server-side: the probe's session is in the gameworld
     journal with `transport=webtransport` and no fallback. As real
     players arrive, their sessions show the same; a session on
     `websocket` with `fallback:"refused"` from a current client build
     is the signature of this path being broken, not of a client fault.
7. **Failure branch (any step).** Leave `webtransport_public` off (or
   remove the four keys) and ship WS-only. Players lose nothing: a
   client whose WebTransport attempt fails falls back to the socket,
   at most one 3 s establish deadline once per session (the #1450
   drill: the failure is remembered for the session's life and never
   retried until a fresh page). `python3 quic_host.py remove --port
   18443` undoes the host side; the sysctl buffers may stay (inert
   without a listener).

## GameWorld CPU reservation (#1581)

The same deploy raises the GameWorld's Nomad CPU reservation from the
deployed 2000 MHz to 6000 MHz: the job spec's `variable "cpu"` default
on main is already 6000 (raised since the 2026-10-19 deploy), so the
deploy lands it with no flag (`-gameworld-cpu` remains an operator
override).

Fit on the production host (4 vCPU, ~15,600 MHz of Nomad compute): the
fleet's reservations become gameworld 6000 + agent 500 + website 250 +
umami 200 + discord-bot 100 = 7,050 MHz, under half the node, so the
job places with headroom. Under full contention the GameWorld's cgroup
share is 6000/7050, about 85% (today 2000/3050, about 66%), and 12:1
against the largest other job. Weights cost nothing while the host is
idle.

Verify after the deploy:

```
nomad job inspect -namespace sro sro-gameworld-global-official   # resources: cpu = 6000
```

and record the tick p99 / scheduler p99 before and after in the release
notes (the numbers #1581's remaining steps compare).
