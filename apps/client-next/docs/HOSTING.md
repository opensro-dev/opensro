# Hosting and session restoration

The browser defaults to same-origin `/api`. Route `/api/` to Agent, stripping
the `/api` prefix. This keeps authentication cookies first-party on localhost
and on the deployed domain. The browser does not need to know Agent's internal
hostname or port, and moving Agent requires only a reverse-proxy configuration
change, not a client rebuild.

For example, an HTTPS nginx virtual host serving the built client can include:

```nginx
location /api/ {
    proxy_pass http://agent:8787/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Configure Agent's exact allowed origins to include the public client origin.
The TLS edge must overwrite `X-Forwarded-Proto` and Agent must not be directly
exposed through an untrusted HTTP forwarding path. Cookies are host-only,
HttpOnly, SameSite=Strict, and Secure for HTTPS. Explicit loopback HTTP uses
the development cookie name. Proxy API responses without caching.

Development and Vite preview already proxy `/api`. Set `SRO_AGENT_PROXY_TARGET`
in the environment or an untracked Vite `.env.local` file to change the upstream
(default `http://127.0.0.1:8787`), then restart the existing client server.
This value is server-side configuration and is not bundled into browser code.

If a separate public API origin is required, set `VITE_AGENT_API_BASE` before
building (for example `https://api.game.example`). Both sites must use HTTPS
and the same registrable domain for Strict-cookie restoration. Agent must
allow the exact client origin with credentials. Do not point login at an
unrelated domain and weaken cookie policy to compensate. Production ignores
the development-only `?apiBase=` diagnostic override.

## Game transport

Agent's shard directory advertises each shard's transport as a URL reference,
which the client resolves against the API URL like any HTTP link. The shard
catalog chooses one of two shapes per shard:

- `"publicTransportUrl": "/shards/<id>"` (an edge route). The edge serving the
  client proxies `/shards/<id>/transport/` (the WebSocket and its immutable
  references) to that shard's `transportUrl`. The browser never learns
  GameWorld addresses, and every client origin works unchanged: localhost, a
  LAN address, HTTPS, or the deployed domain. The development catalog uses it.
- An absolute `publicTransportUrl`, or none (then `transportUrl`). The client
  dials that host directly, and the host owns its TLS and allowed-origin
  policy. Only this shape can carry WebTransport, which a TCP proxy cannot.

An edge route needs one WebSocket-capable location per shard, for example:

```nginx
location /shards/global-official/transport/ {
    proxy_pass http://gameworld-global-official:8788/transport/;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

Configure GameWorld's allowed origins to include the public client origin, as
for Agent. Server-local tools that bypass the edge dial the catalog's
`transportUrl` (`scripts/lib/probeSession.mjs`).

## Development edge and HTTPS

`pnpm dev` and `vite preview` are this edge: `/api` reaches Agent and every
catalog edge route reaches its shard (`SRO_SHARD_CATALOG`, default
`../server/config/shards.json`). A request whose `Origin` is the edge's own
page is forwarded without `Origin`, so Agent's and GameWorld's loopback-only
development policies admit whatever host serves the client. Other origins are
forwarded unchanged and still meet those allowlists.

WebGPU needs a secure context, which plain HTTP provides only on localhost. To
play from another device, run `pnpm dev:https` and open
`https://<this machine's LAN address>:5180/`. It listens on every interface
with a generated self-signed certificate; accept the warning once per device.
For warning-free access, create a pair with mkcert covering the addresses you
use, install its CA on those devices, and set `SRO_DEV_TLS_CERT` and
`SRO_DEV_TLS_KEY` in `.env.local`.

A tunnel such as Cloudflare Tunnel or ngrok can also publish the edge: it
terminates TLS, so the page is a secure context, and forwards plain HTTP.
List its public host names in `SRO_DEV_TUNNEL_HOSTS` (comma-separated, no
scheme or port). They pass Vite's host check, and for those hosts only the
edge takes the page's scheme from `X-Forwarded-Proto`, so its own HTTPS pages
are still recognised and relayed without `Origin`. Requests to any other host
ignore the header. Serve the tunnel from `vite preview` of a built client
rather than `pnpm dev`, whose per-module requests quickly exhaust tunnel rate
limits. Anyone who reaches the tunnel reaches the development cluster.

## Session restoration

Login restoration retains the existing signed session's twelve-hour expiry;
refresh does not renew it. A successful EnterWorld ticket request remembers a
character hint in a second HttpOnly cookie, only when the browser cookie matches
the bearer. The hint grants no authority. On refresh the session owner validates
the authoritative roster and requests new one-use transport/EnterWorld tickets.
Missing or deleted characters return to selection; rejected admission returns
to the authenticated roster. Login/logout clear the old hint. Browser teardown
does not log out. No passwords or authentication tokens go into local storage.
