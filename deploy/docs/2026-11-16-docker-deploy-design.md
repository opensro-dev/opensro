# Deploy do OpenSRO em Docker (fork)

Data: 2026-11-16. Branch: `deploy/docker`.

## Por que

O fork precisa rodar em produção num host Docker que já roda o Traefik, em `https://game.example.com`,
com servidor e cliente web. O upstream (`opensro-dev/opensro`) roda sob Nomad (`sro-nomad`); aqui o dono
prefere tudo em containers Docker, gerenciados por `docker compose`, no fluxo de deploy que ele já usa em outro
projeto (publish, deploy e rollout).

O fork precisa continuar recebendo o upstream com `git fetch upstream && git merge upstream/main` sem conflito.

## Objetivos

- Um comando na máquina do dono (`deploy/deploy.sh`) builda as imagens, publica num registry privado, copia
  só os arquivos da stack para o servidor e faz o rollout por SSH. O servidor não compila nada.
- Stack em `~/silkroad` no servidor, um container por papel: `agent`, `gameworld` (um por shard ligado),
  `identity` e `web`.
- Atrás do Traefik já existente (rede externa `traefik`, TLS automático), domínio `game.example.com`.
- Backup do estado (SQLite) antes de cada rollout que mexe no servidor; rollback trocando a tag.

## Não objetivos

- WebTransport (UDP) em produção. O cliente usa WebSocket por padrão (WebTransport é uma opção
  Experimental), e o Traefik encaminha WebSocket. Fica para depois, se um dia valer abrir uma porta UDP.
- Mais de um shard. A stack nasce com `global-official`; adicionar outro shard é acrescentar um serviço
  `gameworld-<id>` e uma linha no catálogo, mas isso não é feito agora.
- Cadastro público de contas pelo site. As contas são criadas pelo dono com um script (ver abaixo).
- Monitoramento (Prometheus/Grafana) nesta primeira versão.
- CI de deploy (GitHub Actions). O deploy é manual, como em outro projeto do dono.

## Anti-objetivos

- Alterar qualquer arquivo que veio do upstream. Tudo o que é do deploy vive em `deploy/` (pasta que o
  upstream não tem). Se um ajuste no código do upstream parecer necessário, isso para e volta ao dono.
- Imagem pública. As imagens carregam dados derivados do cliente retail (`.generated`); o `publish.sh` recusa
  publicar num repositório público, como o deploy em que este se baseia faz.
- Desligar a checagem de identidade do Agent para facilitar o deploy.

## Restrições encontradas no código

- **Dados de jogo**: o servidor lê a projeção em `apps/server/.generated/game-data/1.150/server` ou o
  arquivo `server.srogz` ao lado (`internal/gamedata/resolve.go`, variável `SRO_SERVER_GAME_DATA_ROOT`). O
  cliente lê `.generated/client-public/assets` (packs + manifest). Ambos saem de `pnpm assets build full` e
  `pnpm task build server-game-data`, feitos neste Mac a partir do cliente em `~/Games/Silkroad-v1.150`. Não
  estão no git; o build das imagens só funciona numa máquina com esses dados.
- **Identidade GameWorld → Agent**: o Agent aceita chamadas de controle do GameWorld só com um JWT de
  workload no formato do Nomad (claims `nomad_namespace`, `nomad_job_id = sro-gameworld-<shard>`,
  `nomad_task = gameworld`, `aud = sro-agent`), verificado contra um JWKS (`SRO_NOMAD_IDENTITY_ISSUER`,
  `SRO_NOMAD_JWKS_URL`, `SRO_NOMAD_NAMESPACE`). O `pnpm local` (`scripts/local/run.mjs`, `prepareIdentity` e
  `mintToken`) já faz isso sem Nomad: chave Ed25519 local, JWKS servido por HTTP, token reescrito num arquivo
  antes de expirar, e o GameWorld relê o arquivo (`SRO_AGENT_IDENTITY_FILE`) a cada chamada.
- **Segredos que o Nomad guardava em Variables**: o anel de chaves de sessão do Agent
  (`SRO_AGENT_SESSION_KEYRING_PATH`), do qual o GameWorld recebe só as chaves públicas
  (`SRO_AGENT_SESSION_PUBLIC_KEYS_PATH`), e o token de provisionamento de contas
  (`SRO_AGENT_PROVISIONING_TOKEN_PATH`). Aqui viram arquivos em volumes Docker.
- **Origens permitidas**: `SRO_AGENT_ALLOWED_ORIGINS` no Agent e `TRANSPORT_ALLOWED_ORIGINS` no GameWorld
  precisam conter `https://game.example.com`. O Agent precisa ver `X-Forwarded-Proto: https` para emitir cookies
  `Secure` (apps/client-next/docs/HOSTING.md).
- **Rotas de borda** (HOSTING.md): `/api/` vai para o Agent tirando o prefixo `/api`; cada
  `/shards/<id>/transport/` vai para o `transportUrl` do shard, com upgrade de WebSocket.
- **Estado**: cada shard é um SQLite em WAL (`SRO_AUTHORITY_STATE_DIR`); as contas ficam em
  `SRO_AGENT_ACCOUNTS_DB_PATH`. Upgrade de schema é `sro-authority-upgrade` (o `pnpm local` roda antes de
  subir), nunca uma transformação silenciosa no boot.
- **Servidor arm64**. Imagens multi-arquitetura, como em outro projeto do dono.

## Abordagens consideradas

1. **Compose no lugar do Nomad (escolhida).** Cada papel é um container. O compose faz o que o Nomad fazia de
   supervisão (restart, healthcheck, ordem de subida); um container `identity` faz o papel da identidade de
   workload; os segredos viram arquivos em volumes. Custo: escrever o `identity` (pequeno) e a primeira
   provisão dos segredos.
2. **Nomad dentro de um container.** Mantém `sro-nomad deploy`, mas vira um container grande gerenciando
   processos dentro dele, com logs e restarts do Nomad e não do Docker. Vai contra o pedido de containers
   separados.
3. **Tirar a checagem de identidade do Agent.** Exigiria mudar código de segurança do upstream só por causa do
   deploy, o que quebra o anti-objetivo do fork. Descartada.

O dono escolheu a 1.

## Desenho

### Layout no repositório (só arquivos novos)

```
deploy/
  docs/                        este documento
  docker/
    server.Dockerfile          um Dockerfile para todos os binários Go; ARG CMD escolhe qual
    web.Dockerfile             build do cliente (Vite) + nginx com os assets publicados
    web.dockerignore / server.dockerignore
    nginx.conf                 rotas /api/ e /shards/<id>/transport/, estáticos com cache
  identity/                    módulo Go próprio (go.mod próprio, só stdlib)
    main.go                    gera/guarda a chave, serve /jwks.json, reescreve o token do GameWorld
  config/
    shards.prod.json           catálogo de produção (transportUrl/controlUrl com nomes de container)
  docker-compose.prod.yml
  .env.prod.example
  deploy.env                   configurações versionadas do dono (sem segredos)
  publish.sh  deploy.sh  rollout.sh  accounts.sh
  README.md                    manual curto de deploy, em português
```

`local.env` (por máquina) e `.env.prod` (só no servidor) nunca são versionados; `deploy/.gitignore` cobre os
dois.

### Imagens

Um repositório privado no ghcr, tags `<repo>:<papel>-<tag>`, onde `<tag>` é o commit (12 caracteres) mais
`-dirty-<UTC>` se os fontes das imagens tiverem mudanças não commitadas (mesma regra do deploy em que este se baseia).

- `server-<tag>`: estágio de build `golang` na plataforma do builder, compilando com `-trimpath` para
  `TARGETARCH` os comandos `sro-agent`, `sro-gameworld`, `sro-authority-upgrade`, `sro-provision-identity`,
  `sro-init` e o necessário para contas (ver Contas). Runtime `debian:stable-slim` (ou distroless se nada
  precisar de shell), usuário sem privilégio, com `apps/server/config` (compatibility.json, shard-settings.json)
  e `server.srogz` copiados para dentro. O comando do container escolhe o binário.
- `identity-<tag>`: o módulo `deploy/identity`, binário estático em `distroless/static`.
- `web-<tag>`: estágio Node que roda `pnpm install --frozen-lockfile` e o build do cliente; runtime `nginx`
  só com o código do cliente. Os assets não entram na imagem (decisão do dono, 2026-11-16).

**Assets do cliente por rsync.** O build gera 7,2 GB em `.generated/client-public`, dos quais o navegador
baixa os packs (cerca de 1,7 GB) e o que mais o manifesto publicado referencia. O `deploy.sh` envia por
`rsync` só o que o cliente serve para `~/silkroad/assets/releases/<id>`, usando a release anterior como base
(`--link-dest`), então só o que mudou trafega e o que não mudou vira hardlink. O `<id>` é o hash do manifesto
dos packs. O `rollout.sh` troca o link `~/silkroad/assets/current` para a release nova só depois que os
containers sobem, e o `web` monta `current` só para leitura. O `release.env` guarda o `<id>` junto com a tag
das imagens, então um rollback volta código e assets juntos. Ficam as duas últimas releases de assets. O
construtor define quais caminhos de `client-public` o cliente realmente serve (packs, manifestos, sidecars
pré-comprimidos) a partir de `docs/architecture/ASSET_DELIVERY.md`, e envia só esses.

O contexto de build é a raiz do repositório, com dockerignore que exclui tudo e libera só o que cada imagem lê
(padrão do deploy em que este se baseia). O `.generated` é lido do checkout principal; numa worktree, o `publish.sh` resolve o
caminho pelo mesmo resolvedor do repositório (`scripts/lib/generatedRoot.mjs`) e passa como contexto adicional
(`docker buildx build --build-context generated=<caminho>`), em vez de copiar ou linkar.

### Containers (compose `silkroad`)

| Serviço | Imagem | Rede | Papel |
|---|---|---|---|
| `identity` | identity | a do `agent` (`network_mode: service:agent`) | Chave Ed25519 no volume `identity`; serve `http://127.0.0.1:8080/jwks.json` dentro do namespace de rede do Agent; reescreve `gameworld-<shard>.jwt` num volume que o gameworld monta só para leitura, antes de expirar. |
| `agent` | server, `sro-agent` | interna | Contas (`accounts.db`), leases, API em `:8787` com `SRO_AGENT_PRIVATE_NETWORK=1` (sem isso ele só escuta em loopback). Volume `agent`. |
| `gameworld-global-official` | server, `sro-gameworld` | interna | Shard `global-official`, transporte WS em `:8788`, controle em `:8791`. Volume `shard-global-official`. |
| `upgrade` | server, `sro-authority-upgrade` | interna | Job de uma vez antes do gameworld (como `migrate` no deploy em que este se baseia). |
| `web` | web | interna + `traefik` | nginx: estáticos, `/api/` para `agent:8787/`, `/shards/global-official/transport/` para `gameworld-global-official:8788/transport/`. Único container com rota no Traefik. |

Só o `web` entra na rede `traefik`, com o router `Host(\`game.example.com\`)`, entrypoint e certresolver nos mesmos
nomes de um Traefik comum (`websecure`, `mytlschallenge`, configuráveis). Nada publica porta no host.

`shards.prod.json` é igual ao do upstream para `global-official`, trocando `transportUrl` por
`http://gameworld-global-official:8788`, `controlUrl` por `http://gameworld-global-official:8791` e mantendo
`publicTransportUrl: "/shards/global-official"`. O shard `test` fica fora.

**Por que o `identity` divide a rede com o Agent** (decisão do construtor, revisão de 2026-11-16): o
verificador do Agent (`internal/security/workload/verifier.go`) recusa issuer e JWKS em `http` fora de
loopback. Servir o JWKS em `127.0.0.1` dentro do namespace do Agent cumpre a regra sem CA própria e sem mexer
no upstream, e o JWKS nem fica visível para os outros containers. Custo: o `identity` reinicia junto com o
Agent, e o healthcheck dele passa pelo namespace do Agent. O token precisa de todos os claims que
`mintToken` (`scripts/local/run.mjs`) emite, incluindo `nomad_allocation_id`.

O GameWorld sempre carrega ou cria um certificado de transporte em `TRANSPORT_CERT_DIR`, mesmo só com
WebSocket; esse diretório fica gravável dentro do volume do shard.

### Segredos e primeira subida

Volumes nomeados guardam o que o Nomad guardava em Variables:

- `cluster`: `agent-session-keys.json` (anel privado do Agent), `agent-session-public-keys.json` (o que o
  GameWorld lê), `agent-provisioning-token`.
- `identity`: a chave Ed25519 da identidade de workload.

Na primeira vez, o `rollout.sh` roda um job `provision` que cria o que falta e nunca substitui o que existe:
`sro-provision-identity` (anel de sessão, token de provisionamento e `public-api-token`), o anel público
derivado com `auth.PublicAgentSessionKeyRing` (como `sro-nomad/secrets.go`) e `sro-init -shard <id>` quando o
diretório do shard ainda não existe (o `upgrade` pula um shard ausente). O `sro-bootstrap-development` não é
usado em produção: ele cria a conta de desenvolvimento.

`.env.prod` no servidor guarda só o que é configuração: domínio, `IMAGE_REPO`, regras `SRO_*` do shard e o
nível de log.

### Contas

O Agent recusa subir com `accounts.db` vazio e sem catálogo semente (`openAccountAuthority`). Por isso a
primeira conta entra por semente (decisão do construtor, revisão de 2026-11-16): `deploy/accounts.sh
--bootstrap` pergunta usuário e senha (sem eco), gera o hash com `hash-agent-password` (incluído na imagem
do servidor) e grava `accounts.json` no volume `cluster`; o Agent passa a ler `SRO_AGENT_ACCOUNTS_PATH` e
insere só as contas que faltam, então a semente pode ficar lá. O `rollout.sh` recusa a primeira subida sem
esse arquivo e diz o comando.

As contas seguintes: `deploy/accounts.sh` cria uma conta pela API de provisionamento do Agent, que só escuta
em loopback, com `docker compose exec agent` e um cliente HTTP dentro da imagem do servidor (por isso o
`agent` não usa distroless puro). A senha nunca vai na linha de comando.

### Fluxo de deploy

Igual ao do deploy em que este se baseia, adaptado:

1. `publish.sh` (Mac): confere que `.generated/client-public` e `server.srogz` existem; builda `server`,
   `identity` e `web` para `linux/amd64,linux/arm64` com buildx; confere que o repositório é privado antes e
   depois do push; imprime a tag.
2. `deploy.sh` (Mac): roda o `publish.sh`, envia os assets por rsync (acima), envia `docker-compose.prod.yml`, `rollout.sh`, `config/` e
   `.env.prod.example` para `~/silkroad` (preservando `.env.prod` e `release.env`), recusa um destino que não
   pareça ser a stack do Silkroad, e chama o `rollout.sh` por SSH.
3. `rollout.sh` (servidor): confere `.env.prod`, Docker e a rede `traefik`; faz pull; confere a arquitetura
   da imagem; para o gameworld e depois o agent de forma limpa (SIGTERM; o shutdown do gameworld leva até 30 s),
para que nenhum SQLite esteja sendo escrito durante a cópia, faz backup dos volumes
   `agent`, `shard-*` e `cluster` em `~/silkroad/backups` (mantém os 5 mais recentes); roda `provision` e
   `upgrade`; sobe `identity`, `agent`, gameworld e `web` esperando cada healthcheck; só então reescreve
   `release.env`; apaga imagens antigas deste repositório, mantendo a atual e a anterior.

Healthchecks: Agent `GET /readyz`; GameWorld `GET /transport/readyz`; identity `GET /jwks.json`; web `GET /`.

Rollback: `IMAGE_TAG=<anterior> ~/silkroad/rollout.sh`. Rollback de dados é restaurar um backup, manual e
documentado no README.

### Compatibilidade com o upstream

O deploy depende destes contratos do upstream: os nomes de variáveis `SRO_*`/`TRANSPORT_*` acima, o formato
do token de workload, o formato de `shards.json` e os endpoints de saúde. O `publish.sh` roda um teste rápido
antes de buildar: sobe a stack localmente com Docker, com estado temporário, e espera o Agent e o GameWorld
ficarem prontos. Se um merge do upstream mudar um desses contratos, o deploy falha ali, e não no servidor.

## O que fica para quem for construir

- Imagem base exata do runtime do servidor (slim ou distroless) e versão de Go/Node fixadas pelas do
  repositório (`apps/server/go.mod`, `package.json`).
- Como o `identity` agenda a renovação do token (o `pnpm local` usa `tokenLifetimeMs` e `tokenRefreshMs`; usar
  os mesmos valores).
- O comando exato de build do cliente para produção (o upstream usa `build:beta` para releases; avaliar se
  serve aqui ou se `build` basta) e os cabeçalhos de cache do nginx para packs com hash no nome.
- Se `server.srogz` sozinho basta para o GameWorld (o upstream prova isso com `pnpm check server` sem a
  projeção solta) ou se a projeção solta também precisa ir na imagem.
