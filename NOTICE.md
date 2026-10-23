# Notices

## Unofficial project

OpenSRO is an independent, unofficial, open-source software project. It is
not affiliated with, endorsed by or connected to WeMade Max, Joymax or any
other publisher or operator of the games it is compatible with. All
trademarks belong to their owners and are used here only to describe
compatibility.

The project publishes source code only. It does not host, distribute or sell
game files, artwork, audio, game data or accounts. It does not operate a game
service, and it takes no payment of any kind.

## Rights holders

If you hold rights in anything this project publishes and want it changed or
removed, email **info@opensro.online**. We aim to reply within 48 hours. When
a request identifies material, we remove it first and discuss afterwards.

To help us act quickly, please include who you are (or who you represent),
what material you are concerned about (a link or file path), and what you
would like us to do. GitHub's own DMCA process also applies to the repository.

## Servers run by others

OpenSRO does not operate, endorse, support or partner with any game server,
public or private, commercial or free. Anyone who runs a server with this code
does so on their own responsibility, including obtaining any rights or
permissions it needs. We do not help run public servers, and we do not accept
money, donations or "credits" from server operators.

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

Thanks to DaxterSoul for sharing knowledge of the game's packet and file
structures.

## Game assets

Source licenses cover only work the contributors are entitled to license. They
grant no rights to third-party game clients, servers, artwork, audio, text,
databases or trademarks.

This repository does not contain a retail client or server, extracted media,
game data tables, retail text, account databases, secrets or private keys.
Anyone running the asset pipeline supplies their own game files, and must be
permitted to use them. Tests include small protocol fixtures and synthetic
values used to check compatibility; do not add retail files, data extracted
from them, or material that cannot be redistributed.

## Dependencies

Go dependencies are listed in [apps/server/go.mod](apps/server/go.mod) and
JavaScript dependencies in [pnpm-lock.yaml](pnpm-lock.yaml). None are vendored.
Each remains under its own license; binary distributors must carry forward any
notices those licenses require.
