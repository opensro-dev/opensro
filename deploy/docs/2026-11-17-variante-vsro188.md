# Deploy da variante VSRO 1.188 + v1.696 (fork)

Data: 2026-11-17. Tudo aqui é port-only, não nativo: a base continua o v1.150 e o conteúdo novo
entra por flags (`docs/features/CAP110.md`, `docs/architecture/ASSET_PIPELINE.md`).

## Fontes

Ficam fora do repositório, em `~/Projetos/sro-clients/`. Os hashes batem com os pins do repo.

| Pasta | O que é | Pin |
| --- | --- | --- |
| `VSRO_Client/` | cliente VSRO 1.188 (`Data`, `Map`, `Media`, `Music`, `Particles`) | `scripts/vsro_archive_pins.json` |
| `sro_1696/` | cliente v1.696 | `scripts/v1696_archive_pins.json` |
| `dbs/SILKROAD_R_SHARD202507231313.bak` | backup do shard ISRO-R (`89c6835f...`) | `POPULATION_SOURCE_SHA256` nos imports |

O cliente v1.150 fica em `~/Games/Silkroad-v1.150`. A variante é montada em
`~/Games/Silkroad-v1.188-variant`, com seu próprio `.generated` e `game-data/`. O `.generated`
compartilhado do checkout não é tocado.

O `.bak` ainda não serve para nada sozinho: os imports leem exports (`SRO_*_DATABASE`) gerados por
scripts de `research/`, que o clone público não tem.

## Flags

Build (variante e assets):

```
SRO_ALEXANDRIA=1 SRO_CAP110_ITEMS=1 SRO_CAP110_SKILLS=1 SRO_CAP110_MONSTERS=1 SRO_HONOR_GOODS=1
SRO_FORT_CONSTANTINOPLE=1 SRO_STYRIA_CLASH=1 SRO_SKYTEMPLE=1
```

Servidor (`shard.env` no host; o deploy não mexe nesse arquivo):

```
SRO_ALEXANDRIA=1
SRO_CAP110_MONSTERS=1
SRO_LEVEL_CAP=110
SRO_CAP110_SET_BONUSES=1
SRO_FORT_CONSTANTINOPLE=1
SRO_STYRIA_CLASH=1
SRO_SKYTEMPLE=1
```

## Passos

Numa worktree, com `SRO_PYTHON` apontando para o `.venv` do checkout principal e as flags de build
exportadas:

```sh
export SRO_VSRO_ROOT=~/Projetos/sro-clients/VSRO_Client SRO_V1696_ROOT=~/Projetos/sro-clients/sro_1696

# 1. Alexandria extraída do VSRO para o extracted do v1.150 (pasta própria, não mexe no nativo)
SRO_GAME_ROOT=~/Games/Silkroad-v1.150 python scripts/import_vsro_alexandria.py

# 2. A variante (nativo por hard link + camadas)
SRO_GAME_ROOT=~/Games/Silkroad-v1.150 python scripts/build_alexandria_variant.py \
    --variant-root ~/Games/Silkroad-v1.188-variant --force

# 3. Assets e dados do servidor, dentro da variante
export SRO_GAME_ROOT=~/Games/Silkroad-v1.188-variant
export SRO_GENERATED_ROOT=$SRO_GAME_ROOT/.generated
export SRO_SERVER_GAME_DATA_ROOT=$SRO_GAME_ROOT/game-data/server
pnpm assets build full
pnpm task build server-game-data

# 4. O servidor aceita os dados? (com as flags de servidor exportadas)
(cd apps/server && go run ./cmd/tools/sro-game-data-check)

# 5. Imagens, smoke e push; depois shard.env e rollout
IMAGE_TAG=<commit>-vsro188-v1696 deploy/publish.sh
IMAGE_TAG=<commit>-vsro188-v1696 SKIP_PUBLISH=1 deploy/deploy.sh
```

`SRO_SERVER_GAME_DATA_ROOT` precisa terminar em `.../game-data/server`: o `build-images.sh` procura
`server.srogz` ao lado dessa pasta.

## Problemas encontrados

- **Disco da VM do Docker cheio.** O smoke falhou com `no space left on device` ao extrair os dados do
  servidor. `docker builder prune -af` resolveu.
- **Hunting 2 com cap 110.** `SRO_HUNTING2` junto com `SRO_CAP110_MONSTERS` falha no build de assets:
  `Duplicate characterdata codename MOB_SD_AKERU` (120000 e 180000). O import do Hunting 2 não pula
  espécies que o cap 110 já adicionou. Ficou desligado.
- **Gears stones.** `SRO_MALL_CATEGORIES` adiciona 112 pedras sem nome em inglês e o build de assets
  recusa (`Missing item English titles (112)`). Ficou desligado.
- **Camada desligada deixa sobra.** Depois de tirar o Hunting 2, os tiles de minimapa que ele converteu
  ficaram no `.generated` da variante e o build parou em `Minimap conversion has no retail source`.
  Foi preciso mover os tiles sem `.ddj` correspondente para fora da árvore.
- **Sem banco.** Pharaoh Tomb, Qin-Shi, Pharaoh Temple, Jupiter, GOD, Muhan e Arabia pedem os exports.
  Os itens D10/D11 existem, mas sem drop nem loja (`no cap110-rewards.txt`, `shopsImported: false`).

## Rollback

Os `shard.env` anteriores ficam em `~/silkroad/backups/shard.env.before-*` no host. Tags publicadas:
`bab11e986a77-vsro188-v1696` (atual), `bab11e986a77-vsro188` (só 1.188), `dc3147229962` (v1.150).
Voltar o cap não desfaz níveis acima de 90 já ganhos.

## Shard PVP

Port-only, não nativo (`docs/superpowers/specs/2026-11-17-pvp-shard-design.md`). A stack roda um segundo
gameworld, `gameworld-pvp`, com volume `shard-pvp`, rota `/shards/pvp/transport/` e o shard `pvp` no
catálogo (`config/shards.prod.json`). Ele lê o `shard.env` (as flags de conteúdo) e depois o
`shard-pvp.env`, que vence. No host, antes do primeiro rollout com o PVP:

```sh
cp shard-pvp.env.example ~/silkroad/shard-pvp.env   # SRO_PVP_SERVER=on, SRO_LEVEL_CAP=110, SRO_BETA_MASTERY=on
```

`SRO_PVP_SERVER` só age no shard cujo id é `pvp`; em outro shard é ignorado com um aviso. Com ele ligado:
personagem criado no nível do cap, com todas as masteries e skills, 100.000.000 de ouro, em Samarkand;
o ferreiro, o armadureiro e o vendedor de acessórios de Samarkand vendem D11 raro +12 e as pedras de grau
11 a 1 de ouro; vender para NPC paga 0. A vendedora de poções (Nun Martel) vende a 1 de ouro as poções e
pílulas de grau máximo (aba Potion) e os buffs temporários mais fortes que o servidor aplica (aba Special:
dano e absorção +20%, HP e MP +4100, acerto, esquiva, agilidade, velocidade). Os quatro vendedores ficam
em fila na frente do ponto de chegada (`pvpserver.TraderSpot`), em vez dos lugares nativos.
