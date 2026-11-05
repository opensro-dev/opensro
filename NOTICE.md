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
corresponding source available under the same license, and keep the
attribution described under "Additional terms" below.

## Additional terms: attribution (AGPL section 7(b))

The OpenSRO contributors add the following term to the material they add
to this work, as section 7(b) of the license allows ("requiring
preservation of specified reasonable legal notices or author
attributions"):

1. **Keep the in-game credit.** Any version of the work, modified or not,
   that players use keeps the "Powered by OpenSRO" attribution on its
   loading and title screens, visible and readable, linking to
   <https://github.com/opensro-dev/opensro>. A modified version may add its
   own name beside it, for example "MyServer, powered by OpenSRO".
2. **Keep this file.** Copies and modified versions keep this NOTICE.md,
   with this section, and the copyright notices in the source.
3. **Say you changed it.** A modified version states that it is modified
   and where its complete corresponding source is (section 5(a) and
   section 13 of the license already ask for both).

These terms add no restriction beyond section 7(b): you may still use,
change, host and share the work under the license. In plain words: open
your source, and credit the project you built on.

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
