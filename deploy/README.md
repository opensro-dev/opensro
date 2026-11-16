# Deploy em Docker

O fork roda num host Docker atrás do Traefik, com containers no lugar do Nomad do upstream. O desenho completo
está em [docs/2026-11-16-docker-deploy-design.md](docs/2026-11-16-docker-deploy-design.md).

Tudo do deploy fica nesta pasta. Nenhum arquivo do upstream é alterado, então puxar o original continua sendo:

```sh
git fetch upstream && git merge upstream/main
```

Depois de um merge, o próximo `deploy/deploy.sh` roda o smoke test antes de publicar. Se o upstream mudar algo
que o deploy usa, ele para na sua máquina e não no servidor.

## O que roda

| Container | Papel |
|---|---|
| `identity` | Faz o papel da identidade de workload do Nomad: guarda uma chave Ed25519, serve o JWKS em loopback e renova o token do GameWorld. É dono da rede em que o Agent roda. |
| `agent` | Login, contas e leases dos shards. Roda na rede do `identity` e aparece como `agent:8787`. |
| `gameworld-global-official` | O shard. WebSocket em `:8788`; o WebTransport fica desligado. |
| `web` | nginx com o cliente: `/api/` vai para o Agent, `/shards/global-official/transport/` vai para o GameWorld. É o único container na rede `traefik`. |

Jobs de uma vez, que o `rollout.sh` roda: `provision` (cria chaves, tokens e o authority do shard que faltarem,
sem nunca trocar nada que já existe), `derive-public` (as chaves públicas de sessão que o GameWorld lê) e
`upgrade` (atualiza o schema do authority quando ele é antigo).

Volumes: `cluster` (chaves e tokens do Agent, `accounts.json`), `agent` (`accounts.db`), `identity` (a chave do
token), `shard-global-official` (o SQLite do shard), `identity-out` e `gamedata-cache` (descartáveis).

## Pré-requisitos

- **Dados de jogo gerados no checkout principal**, a partir do cliente v1.150:
  ```sh
  export SRO_GAME_ROOT=~/Games/Silkroad-v1.150 SRO_PYTHON=~/Projetos/opensro/.venv/bin/python
  pnpm assets prepare && pnpm assets build full && pnpm task build server-game-data
  ```
- **`docker login ghcr.io`** com um token clássico que tenha `write:packages`. O repositório de imagens tem que
  ser privado: as imagens carregam dados derivados do cliente, e o `publish.sh` se recusa a publicar num
  repositório público.
- **No servidor**: Docker com Compose, a rede `traefik` do Traefik, `docker login ghcr.io` com um token que
  leia o pacote, e o DNS de `game.example.com` apontando para ele.
- **`deploy/deploy.env`**: `cp deploy/deploy.example.env deploy/deploy.env` e preencha o host, a pasta e o
  repositório de imagens. Esse arquivo é ignorado pelo git.

## Primeiro deploy

1. `deploy/deploy.sh`. O rollout para em `.env.prod` porque ele ainda não existe.
2. No servidor: `cd ~/silkroad && cp .env.prod.example .env.prod` e revise os valores.
3. Regras do shard (opcional): crie `~/silkroad/shard.env` com `SRO_*=valor`, uma por linha. Sem esse arquivo, o
   GameWorld usa os padrões do catálogo do upstream e lista na partida quais aplicou.
4. `IMAGE_TAG=<tag> SKIP_PUBLISH=1 deploy/deploy.sh`, com a tag que o passo 1 publicou. Desta vez o rollout para
   pedindo a primeira conta.
5. `deploy/accounts.sh --bootstrap <id>`. A senha é digitada no servidor, sem eco.
6. `IMAGE_TAG=<tag> SKIP_PUBLISH=1 deploy/deploy.sh` de novo. Abra `https://game.example.com` e entre.

## Uso normal

| Quero | Comando |
|---|---|
| Publicar e subir o que está no checkout | `deploy/deploy.sh` |
| Subir uma tag já publicada | `IMAGE_TAG=<tag> SKIP_PUBLISH=1 deploy/deploy.sh` |
| Só rodar o rollout no servidor | `cd ~/silkroad && ./rollout.sh` (repete o que está em `release.env`) |
| Voltar para a versão anterior | no servidor: `IMAGE_TAG=<PREVIOUS_IMAGE_TAG> ASSETS_RELEASE=<PREVIOUS_ASSETS_RELEASE> ./rollout.sh`, com os valores de `release.env` |
| Criar uma conta | `deploy/accounts.sh <id>` |
| Ver os logs | no servidor: `cd ~/silkroad && docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f gameworld-global-official` |

Os assets vão por rsync para `~/silkroad/assets/releases/<id>`, onde `<id>` vem do hash do manifesto dos packs.
Só o que mudou atravessa a rede, e uma release que já está no servidor não é reenviada. Ficam as duas mais
recentes.

## Backups

Cada rollout para o GameWorld e o Agent e grava todos os volumes de estado em
`~/silkroad/backups/silkroad-<UTC>.tar.gz`. Ficam os 5 mais recentes. Para restaurar um:

```sh
cd ~/silkroad
C="docker compose -f docker-compose.prod.yml --env-file .env.prod --env-file release.env"
$C stop
for v in cluster agent identity shard-global-official; do
    docker run --rm -v silkroad_$v:/v alpine sh -c 'rm -rf /v/* /v/.[!.]* 2>/dev/null; true'
done
docker run --rm $(for v in cluster agent identity shard-global-official; do printf -- '-v silkroad_%s:/backup/%s ' $v $v; done) \
    -v "$PWD/backups:/b:ro" alpine tar -C /backup -xzf /b/silkroad-<UTC>.tar.gz
./rollout.sh
```

## Testes

Rodam na sua máquina, com as imagens locais (`deploy/build-images.sh`):

| Teste | O que prova |
|---|---|
| `cd deploy/identity && go test ./...` | o token passa no verificador real do Agent do upstream |
| `deploy/test/smoke.sh` | a stack sobe, `/api` e `/shards` respondem, o login funciona, `accounts.sh` cria conta, `provision` não troca chaves |
| `deploy/test/rollout_test.sh` | primeira subida sem conta, rollout, troca de assets, backup, rollback, release ausente, Agent reiniciado sozinho |
| `deploy/test/publish_test.sh` | tag e recusa sem `IMAGE_REPO` |
| `deploy/test/deploy_test.sh` | proteção do `DEPLOY_PATH`, release de assets completa, smoke servindo dela |
