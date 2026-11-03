# Notices

## Unofficial project

OpenSRO is an independent, unofficial compatibility project. It is not
endorsed, sponsored or operated by the publishers or other rights holders of
Silkroad Online. Product names and trademarks belong to their owners.

## License

OpenSRO is free software under the GNU Affero General Public License, version 3
or (at your option) any later version: [LICENSE](LICENSE)
(`AGPL-3.0-or-later`). You may use, study, modify and share it, including in
forks. Anyone who distributes a modified version, or lets players use one over
a network (for example by hosting a server), must make the complete
corresponding source available under the same license.

## Origin

`apps/server` began as a fork of
[ferdoran/go-sro-gateway-server](https://github.com/ferdoran/go-sro-gateway-server)
by Roland Müller, released under the DON'T BE A DICK PUBLIC LICENSE 1.1, which
permits modified works under other terms. Its original license text is kept in
[apps/server/LICENSE-UPSTREAM.md](apps/server/LICENSE-UPSTREAM.md).

Thanks to
[DaxterSoul](https://www.elitepvpers.com/forum/members/1084164-daxtersoul.html)
for sharing knowledge of the game's packet and file structures.

## Game assets

Source licenses cover only work the contributors are entitled to license. They
grant no rights to third-party game clients, servers, artwork, audio, text,
databases or trademarks.

This repository does not contain a retail client or server, extracted media,
account databases, secrets or private keys. The asset pipeline reads a client
the operator supplies. OpenSRO grants no rights to that client, and each
operator is responsible for having the right to use it. Tests include small
protocol fixtures and derived reference values used to check compatibility; do
not add complete retail files or material that cannot be redistributed.

## Dependencies

Go dependencies are listed in [apps/server/go.mod](apps/server/go.mod) and
JavaScript dependencies in [pnpm-lock.yaml](pnpm-lock.yaml). None are vendored.
Each remains under its own license; binary distributors must carry forward any
notices those licenses require.
