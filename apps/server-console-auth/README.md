# Console sign-in

Staff sign in to the operator console (`apps/server-observatory`) with
Discord. This service answers Caddy's `forward_auth` on loopback. It tells
Caddy who the operator is and which console role they hold (operator or
approver). Caddy passes that identity to the console, so every audited
console action names a person. Node 22+, no install or build step, no
dependencies.

```sh
SRO_CONSOLE_AUTH_CONFIG=/etc/opensro-console-auth/config.json npm start
npm test
```

## Config

```json
{
  "origin": "https://console.example",
  "discord": { "clientId": "…", "guildId": "…", "clientSecretFile": "/path/to/secret" },
  "roles": { "staff": "…", "operator": "…", "approver": "…" },
  "approverIds": [ "…" ],
  "limits": { "sessionMaxMs": 43200000, "idleMs": 7200000 }
}
```

The ids are Discord ids. The redirect URI is always `<origin>/auth/callback`.
`limits` is optional; `server/auth.mjs` `DEFAULT_LIMITS` lists the keys.

## Rules

- **Operators and up only.** Staff plus Operator gives operator. Staff plus
  Approver plus a user id in `approverIds` (the owner) gives approver, because
  a Discord role alone can be self-granted by anyone who manages roles.
  Anything else, Staff alone included, is no access.
- **Sessions live in memory.** They end at their maximum age, after an idle
  spell, at sign-out, or when the role re-check fails for any reason. A
  restart signs everyone out, and no token is written to disk.
- **The log** is one JSON line per sign-in event. It never contains a token,
  code, state or cookie.
- **The service never sees the console's edge secret.** Caddy deletes client
  copies of the identity headers and of the edge secret before asking this
  service, routes `/auth/*` only here, and adds the edge secret itself.
