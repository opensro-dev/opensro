# Deploy do OpenSRO em Docker: plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rodar o servidor e o cliente do fork em containers num host Docker, atrás do Traefik, em `https://game.example.com`, com deploy por um comando.

**Architecture:** `docker compose` no lugar do Nomad: containers `agent`, `gameworld-global-official`, `identity` (na rede do agent) e `web` (nginx). Imagens multi-arquitetura num ghcr privado; assets do cliente por rsync; fluxo `publish.sh` → `deploy.sh` → `rollout.sh`, no molde do deploy em que este se baseia.

**Tech Stack:** Docker/buildx, Compose v2, Go 1.27 (stdlib no `identity`), nginx, bash, rsync, Traefik existente.

**Spec:** `deploy/docs/2026-11-16-docker-deploy-design.md`

## Global Constraints

- Nenhum arquivo fora de `deploy/` é criado ou alterado. Se um passo parecer exigir isso, parar e perguntar ao dono.
- Domínio `game.example.com`; stack em `~/silkroad` no servidor; rede externa `traefik`; entrypoint `websecure` e certresolver `mytlschallenge` (configuráveis pelo `.env.prod`).
- Imagens em `<IMAGE_REPO>:<papel>-<tag>`, papéis `server`, `identity`, `web`; tag = commit (12 caracteres), mais `-dirty-<UTC>` se os fontes das imagens tiverem mudança não commitada.
- Plataformas padrão `linux/amd64,linux/arm64`; o servidor é arm64.
- O repositório de imagens é privado: `publish.sh` recusa publicar se ele estiver público, antes e depois do push.
- Shard único `global-official`; só o `web` entra na rede `traefik`; nenhum container publica porta no host.
- Token do `identity`: validade 2 h, renovado a cada 30 min, com os claims de `mintToken` em `scripts/local/run.mjs` (`iss`, `aud: "sro-agent"`, `iat`, `exp`, `nomad_namespace`, `nomad_job_id: "sro-gameworld-<shard>"`, `nomad_allocation_id`, `nomad_task: "gameworld"`), assinatura EdDSA, `kid` no cabeçalho.
- Namespace de workload em produção: `silkroad` (o mesmo valor em `SRO_NOMAD_NAMESPACE` e no claim).
- Segredos nunca na linha de comando nem versionados; `.env.prod`, `release.env` e `local.env` ficam fora do git.
- Scripts bash com `set -euo pipefail`, LF, UTF-8 (os gates de encoding do repositório valem para `deploy/`).
- Backups: os 5 mais recentes; releases de assets: as 2 mais recentes; imagens: a tag atual e a anterior.

## Review Focus

- Primeira subida num servidor vazio: sem `accounts.json` semente o rollout deve parar antes de subir o Agent e dizer o comando (`deploy/accounts.sh --bootstrap`). Teste em Task 6.
- Rollout de novo sobre volumes existentes não pode recriar nem trocar chaves (anel de sessão, token de provisionamento, chave do `identity`): os jogadores perderiam as sessões e o token velho deixaria de valer. Teste em Task 4 (rodar `provision` duas vezes e comparar hashes).
- Merge do upstream que muda um contrato (nome de variável, claim, catálogo): o smoke local tem que falhar no `publish.sh`, antes de qualquer push. Teste em Task 4 (smoke) e chamado na Task 7.
- Rollback com `IMAGE_TAG=<anterior>`: volta imagem e release de assets juntas, sem apagar a release atual até a próxima limpeza. Teste em Task 8.
- `deploy.sh` apontado para uma pasta errada no servidor (vazia de outra coisa, a do deploy em que este se baseia): recusa sem apagar nada. Teste em Task 8.

---

### Task 1: Serviço `identity`

**Files:**
- Create: `deploy/identity/go.mod` (módulo `opensro.online/server/deploy/identity`, `go 1.27.4`, `require opensro.online/server v0.0.0` com `replace opensro.online/server => ../../apps/server`)
- Create: `deploy/identity/main.go`, `deploy/identity/token.go`, `deploy/identity/publickeys.go`
- Create: `deploy/identity/cmd/seedhash/main.go` (só para o smoke: lê uma senha do stdin e imprime o hash bcrypt de custo 12, o `MaxBcryptCost` do upstream)
- Test: `deploy/identity/token_test.go`, `deploy/identity/main_test.go`, `deploy/identity/publickeys_test.go`

Por que esse caminho de módulo: o Go só deixa importar `opensro.online/server/internal/...` de pacotes cujo caminho começa com `opensro.online/server/`. Com ele, o `identity` usa o código real do upstream (`auth.PublicAgentSessionKeyRing`) e os testes verificam o token com o verificador real do Agent (`workload.NewNomadWorkloadIdentityVerifier`). Um merge do upstream que mude esses contratos quebra estes testes, sem nenhum arquivo do upstream alterado.

**Interfaces:**
- Produces: binário `identity` com flags `-key <arquivo>` (chave Ed25519 JWK, criada se faltar, modo 0600, nunca sobrescrita), `-listen <addr>` (padrão `127.0.0.1:8080`), `-issuer <url>` (padrão `http://127.0.0.1:8080`), `-namespace <ns>`, `-out-dir <dir>`, `-shards <id,id>`. Serve `GET /jwks.json` (`{"keys":[{kty,crv,x,kid,alg:"EdDSA",use:"sig"}]}`) e escreve `<out-dir>/gameworld-<shard>.jwt` (modo 0644, escrita atômica por rename) na partida e a cada 30 min.
- Produces: `func mintToken(key ed25519.PrivateKey, kid, issuer, namespace, shard string, now time.Time) string`.
- Produces: modo `identity -derive-public <anel privado> -out <arquivo>`: escreve (atômico, 0644) o resultado de `auth.PublicAgentSessionKeyRing` sobre o arquivo lido; sai sem fazer nada se a saída já for igual.

- [ ] **Step 1: Testes que falham**:
  - `TestMintTokenClaims`: decodifica o payload e confere exatamente os claims da seção Global Constraints, `exp - iat == 7200`, `nomad_job_id == "sro-gameworld-global-official"`.
  - `TestAgentVerifierAcceptsToken`: sobe o handler do JWKS num `httptest.Server` em `127.0.0.1`, cria `workload.NewNomadWorkloadIdentityVerifier(issuer, issuer+"/jwks.json", nil)` e chama `Verify` no token. Esperado: sem erro, `JobID == "sro-gameworld-global-official"`, `Task == "gameworld"`. Este é o teste de contrato com o upstream.
  - `TestDerivePublicIsIdempotent` (em `publickeys_test.go`): gera um anel com `clusterprovision.EnsureIdentity(t.TempDir())`, deriva duas vezes e confere que o arquivo não muda e que bate com `auth.PublicAgentSessionKeyRing`.
  - `TestKeyIsCreatedOnceAndKept` (em `main_test.go`): carregar a chave duas vezes do mesmo arquivo dá o mesmo `kid` e os mesmos bytes; o arquivo tem modo 0600.
  - `TestJWKSServesThePublicKey`: `httptest` no handler, `x` decodificado é igual à chave pública.
- [ ] **Step 2:** `cd deploy/identity && go test ./...`. Esperado: falha de compilação (funções inexistentes).
- [ ] **Step 3:** Implementar o token e o JWKS só com stdlib (`crypto/ed25519`, `encoding/base64` RawURL, `encoding/json`, `net/http`); `-derive-public` chama `auth.PublicAgentSessionKeyRing`; `seedhash` usa `golang.org/x/crypto/bcrypt` (já no `go.sum` do upstream). Banners id-style em cada arquivo e função (AGENTS.md).
- [ ] **Step 4:** `go test ./... && go vet ./... && gofmt -l .`. Esperado: PASS e nenhuma saída do gofmt.
- [ ] **Step 5:** Commit `feat(deploy): identity service for the GameWorld workload token`.

### Task 2: Imagem do servidor

**Files:**
- Create: `deploy/docker/server.Dockerfile`, `deploy/docker/server.Dockerfile.dockerignore`
- Create: `deploy/docker/server-entrypoint.sh`

**Interfaces:**
- Consumes: os fontes de `apps/server`; o contexto adicional `gamedata` (diretório `apps/server/.generated/game-data/1.150`, passado com `--build-context gamedata=<dir>`).
- Produces: imagem com `/app/bin/{sro-agent,sro-gameworld,sro-authority-upgrade,sro-provision-identity,sro-init,hash-agent-password}`, `/app/config/{compatibility.json,shard-settings.json}`, `/app/game-data/1.150/server.srogz`, `SRO_SERVER_GAME_DATA_ROOT=/app/game-data/1.150/server`, usuário `10001:10001`, `curl` disponível (para `accounts.sh` e healthchecks). A imagem não carrega o binário do `identity`: ele tem imagem própria (Task 3).
- Produces: `server-entrypoint.sh <papel>`, onde `<papel>` ∈ `agent | gameworld | upgrade | provision | init | hash-password`; os papéis `provision` e `upgrade` estão descritos na Task 4.

- [ ] **Step 1:** Dockerfile em dois estágios: `golang:1.27.4` em `--platform=$BUILDPLATFORM`, `GOOS=linux GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath` de cada comando; runtime `debian:stable-slim` com `ca-certificates curl`. O dockerignore exclui tudo e libera `apps/server/go.mod`, `go.sum`, `cmd/`, `internal/`, `config/` (sem `testdata`, `.generated`, `*_test.go`).
- [ ] **Step 2:** Verificar o build: `docker buildx build -f deploy/docker/server.Dockerfile --build-context gamedata=/Users/diorgera/Projetos/opensro/apps/server/.generated/game-data/1.150 --load -t silkroad-server:test .` (a Task 7 troca o caminho fixo pelo resolvedor). Esperado: build ok.
- [ ] **Step 3:** `docker run --rm silkroad-server:test hash-password </dev/null`. Esperado: sai com "stdin is not a terminal" (o binário existe e roda). `docker run --rm --entrypoint ls silkroad-server:test /app/game-data/1.150`. Esperado: `server.srogz`.
- [ ] **Step 4:** Confirmar que o GameWorld lê só o `.srogz`: a Task 4 sobe a stack com essa imagem; se falhar por falta da projeção solta, copiar também `server/` (377 MB) e registrar o motivo no topo do Dockerfile.
- [ ] **Step 5:** Commit `feat(deploy): server image for the Agent, GameWorld and operator tools`.

### Task 3: Imagens `identity` e `web`, com nginx

**Files:**
- Create: `deploy/docker/identity.Dockerfile` (+ `.dockerignore` liberando só `deploy/identity`)
- Create: `deploy/docker/web.Dockerfile` (+ `.dockerignore`), `deploy/docker/nginx.conf`

**Interfaces:**
- Produces: imagem `identity` (`gcr.io/distroless/static-debian12:nonroot`, entrypoint `/identity`). O contexto do build precisa de `deploy/identity` e de `apps/server` (por causa do `replace`).
- Produces: imagem `web`: estágio `node:24` com `corepack pnpm install --frozen-lockfile` e `pnpm --filter @sro/client-next build` (saída em `apps/client-next/temp/artifacts/dist`); runtime `nginx:1.27-alpine` com o `dist` em `/usr/share/nginx/html` e os assets montados em `/usr/share/nginx/html/assets` (só leitura, Task 4).
- Produces: `nginx.conf` com:
  - `location /api/ { proxy_pass http://agent:8787/; }`, com `Host`, `X-Forwarded-For`, `X-Forwarded-Proto https` e sem cache;
  - `location /shards/global-official/transport/ { proxy_pass http://gameworld-global-official:8788/transport/; }`, com upgrade de WebSocket e `proxy_read_timeout 3600s`;
  - `/assets/packs/*.bin` com `Cache-Control: public, max-age=31536000, immutable`; manifestos (`manifest.json`, `packs/manifest.json`, `delivery/*`) com `no-cache`; `gzip_static on` para os sidecars `.gz`;
  - `try_files $uri /index.html` para o resto.

- [ ] **Step 1:** Escrever os dois Dockerfiles e o `nginx.conf` (a lista de proxy vem de `apps/client-next/docs/HOSTING.md`).
- [ ] **Step 2:** `docker buildx build ... --load` das duas imagens. Esperado: ok. `docker run --rm silkroad-web:test nginx -t`. Esperado: `syntax is ok` (os upstreams só resolvem na Task 4; o `nginx -t` usa `resolver` com nomes adiados, então declare os upstreams por variável com `resolver 127.0.0.11`).
- [ ] **Step 3:** Commit `feat(deploy): identity and web images`.

### Task 4: Compose, catálogo de produção e smoke local

**Files:**
- Create: `deploy/config/shards.prod.json`
- Create: `deploy/docker-compose.prod.yml`, `deploy/docker-compose.smoke.yml` (override do smoke: sem rede `traefik`, `web` publicado em `127.0.0.1:18080`, imagens `:test` locais)
- Create: `deploy/.env.prod.example`, `deploy/.gitignore` (`.env.prod`, `release.env`, `local.env`, `backups/`, `assets/`)
- Create: `deploy/test/smoke.sh`
- Modify: `deploy/docker/server-entrypoint.sh` (papéis `provision` e `upgrade`)

**Interfaces:**
- Consumes: imagens das Tasks 2 e 3; `identity` flags da Task 1.
- Produces: serviços do compose `identity`, `agent`, `gameworld-global-official`, `provision` (job), `derive-public` (job), `upgrade` (job), `web`; volumes `cluster`, `agent`, `identity`, `identity-out`, `shard-global-official`; bind `${ASSETS_DIR:-./assets/current}` no `web`.
- Produces: `smoke.sh [--keep]`: sobe a stack com o override do smoke num projeto `silkroad-smoke` com volumes novos, roda `provision`, grava a semente `smoke` com senha aleatória (hash de `go run ./cmd/seedhash` em `deploy/identity`, porque o `hash-agent-password` exige terminal) no volume `cluster`, espera os healthchecks, faz `GET http://127.0.0.1:18080/api/readyz` e `GET http://127.0.0.1:18080/shards/global-official/transport/readyz` (200), e derruba tudo com `down -v` salvo `--keep`. Sai 0 só se tudo passou.

Valores fixos:
- `shards.prod.json`: cópia de `global-official` de `apps/server/config/shards.json` com `transportUrl: "http://gameworld-global-official:8788"`, `controlUrl: "http://gameworld-global-official:8791"`, `publicTransportUrl: "/shards/global-official"`; sem o shard `test`.
- `agent`: `SRO_AGENT_PRIVATE_NETWORK=1`, `SRO_AGENT_API_ADDR=0.0.0.0:8787`, `SRO_AGENT_ALLOWED_ORIGINS=https://${SRO_DOMAIN}`, `SRO_SHARD_CATALOG_PATH=/app/deploy-config/shards.prod.json`, `SRO_AGENT_ACCOUNTS_PATH=/cluster/accounts.json`, `SRO_AGENT_ACCOUNTS_DB_PATH=/data/agent/accounts.db`, `SRO_AGENT_DIRECTORY_STATE_PATH=/data/agent/shard-leases.json`, `SRO_AGENT_PROVISIONING_TOKEN_PATH=/cluster/agent-provisioning-token`, `SRO_AGENT_SESSION_KEYRING_PATH=/cluster/agent-session-keys.json`, `SRO_NOMAD_IDENTITY_ISSUER=http://127.0.0.1:8080`, `SRO_NOMAD_JWKS_URL=http://127.0.0.1:8080/jwks.json`, `SRO_NOMAD_NAMESPACE=silkroad`. Healthcheck `curl -fsS http://127.0.0.1:8787/readyz`.
- `identity`: `network_mode: service:agent`, `-key /identity/key.json -out-dir /out -shards global-official -namespace silkroad`; o volume `identity-out` montado em `/out`.
- `gameworld-global-official`: `SRO_SHARD_ID=global-official`, `SRO_AGENT_URL=http://agent:8787`, `SRO_AGENT_URL_REQUIRE=1`, `SRO_AGENT_IDENTITY_FILE=/identity/gameworld-global-official.jwt` (volume `identity-out` só leitura), `SRO_AGENT_SESSION_PUBLIC_KEYS_PATH=/cluster/agent-session-public-keys.json` (`cluster` só leitura), `SRO_GAMEWORLD_CONTROL_ADDR=0.0.0.0:8791`, `SRO_GAMEWORLD_PRIVATE_NETWORK=1`, `TRANSPORT_WS_ADDR=0.0.0.0:8788`, `TRANSPORT_WT_ADDR=0.0.0.0:8788`, `TRANSPORT_ALLOWED_ORIGINS=https://${SRO_DOMAIN}`, `SRO_AUTHORITY_REQUIRE=1`, `SRO_AUTHORITY_STATE_DIR=/data/shard`, `TRANSPORT_CERT_DIR=/data/shard/certs`; `stop_grace_period: 45s`; healthcheck `curl -fsS http://127.0.0.1:8788/transport/readyz`; regras `SRO_*` do `.env.prod` passam por `env_file`.
- `provision` (papel do entrypoint): roda `sro-provision-identity -state-dir /cluster`, e `sro-init -shard global-official -authority-dir /data/shard` só se `/data/shard` não tiver authority (conferir os nomes das flags no `sro-init`). Nunca sobrescreve. O anel público vem de um serviço de uma vez `derive-public` com a imagem `identity` (`-derive-public /cluster/agent-session-keys.json -out /cluster/agent-session-public-keys.json`), que roda logo depois do `provision`.
- `upgrade`: `sro-authority-upgrade -authority-dir /data/shard -commit`.
- `web`: redes `default` e `traefik`; labels `traefik.enable=true`, `traefik.docker.network=traefik`, router `silkroad` com `Host(\`${SRO_DOMAIN}\`)`, entrypoint e certresolver por variável, `loadbalancer.server.port=80`.

- [ ] **Step 1:** Adicionar o papel `provision` e `upgrade` ao `server-entrypoint.sh` e o serviço `derive-public` ao compose.
- [ ] **Step 2:** Escrever compose, override, `.env.prod.example` (`SRO_DOMAIN=game.example.com`, `TRAEFIK_ENTRYPOINT=websecure`, `TRAEFIK_CERTRESOLVER=mytlschallenge`, `LOG_LEVEL=info`) e `smoke.sh`.
- [ ] **Step 3:** `deploy/test/smoke.sh`. Esperado: sai 0 e imprime as duas respostas 200.
- [ ] **Step 4:** Idempotência: `deploy/test/smoke.sh --keep`, guardar `sha256sum` dos arquivos de `/cluster` e `/identity/key.json`, rodar `docker compose ... run --rm provision` de novo, comparar. Esperado: hashes iguais. Incluir essa checagem no próprio `smoke.sh`.
- [ ] **Step 5:** Commit `feat(deploy): compose stack, production shard catalog and local smoke test`.

### Task 5: Contas

**Files:**
- Create: `deploy/accounts.sh`
- Modify: `deploy/test/smoke.sh` (cria uma segunda conta pelo script)

**Interfaces:**
- Consumes: `DEPLOY_HOST`, `DEPLOY_PATH` (Task 7), serviço `agent`, `hash-password` do entrypoint.
- Produces: `accounts.sh --bootstrap <id>`: abre `ssh -t` para o servidor e roda `docker compose run --rm -it agent hash-password`, lê o hash e grava `[{"id":"<id>","passwordHash":"<hash>"}]` em `/cluster/accounts.json` mandando o JSON pelo stdin de `docker compose run --rm -T --entrypoint sh provision -c 'set -e; test ! -e /cluster/accounts.json; umask 077; cat > /cluster/accounts.json'`, que recusa se o arquivo já existir. `accounts.sh <id>`: lê a senha sem eco localmente (`read -s`, duas vezes), envia por stdin do ssh para `docker compose exec -T agent curl -fsS -X POST -H "Authorization: Bearer $(cat /cluster/agent-provisioning-token)" --data @- http://127.0.0.1:8789/v1/accounts` com `{"id","password"}`. A senha nunca aparece em argv nem em log.
- Uma variável `ACCOUNTS_LOCAL=1` faz o script falar com o projeto do smoke em vez de ssh (para o teste).

- [ ] **Step 1:** Escrever o script; validar `<id>` com `^[A-Za-z0-9_]{3,16}$` (conferir contra `domain.AccountIDValid` e usar a mesma regra).
- [ ] **Step 2:** No `smoke.sh`, criar a conta `smoke2` com `ACCOUNTS_LOCAL=1` e conferir `GET /v1/accounts/smoke2` (201 na criação, 200 na consulta).
- [ ] **Step 3:** `deploy/test/smoke.sh`. Esperado: sai 0.
- [ ] **Step 4:** Commit `feat(deploy): account bootstrap and creation`.

### Task 6: `rollout.sh`

**Files:**
- Create: `deploy/rollout.sh`
- Test: `deploy/test/rollout_test.sh`

**Interfaces:**
- Consumes: o compose da Task 4; `IMAGE_REPO`, `IMAGE_TAG`, `ASSETS_RELEASE` do ambiente ou de `release.env`.
- Produces: ao final, `release.env` com `IMAGE_REPO`, `IMAGE_TAG`, `ASSETS_RELEASE`, `PREVIOUS_IMAGE_TAG`, `PREVIOUS_ASSETS_RELEASE`.

Ordem, abortando no primeiro erro:
1. Conferir `.env.prod` (existe, sem `change-me`, `SRO_DOMAIN` definido), Docker, rede `traefik` e `assets/releases/$ASSETS_RELEASE`.
2. `pull`; checar a arquitetura das três imagens contra o host.
3. `run --rm provision` e `run --rm derive-public` (idempotentes; criam o volume `cluster` na primeira vez).
4. Se o volume `cluster` não tem `accounts.json`: parar com `rode deploy/accounts.sh --bootstrap <id> e depois este rollout de novo`.
5. `stop` do gameworld e depois do agent.
6. Backup dos volumes `cluster`, `agent`, `identity`, `shard-global-official` em `~/silkroad/backups/silkroad-<UTC>.tar.gz` (por `docker run --rm -v <vol>:/v/<vol>:ro alpine tar`), mantendo 5.
7. `run --rm upgrade`.
8. Trocar `assets/current` para a release (`ln -sfn` num nome temporário e `mv -T`).
9. `up -d --wait` de `agent identity gameworld-global-official web`.
10. Gravar `release.env`; apagar imagens deste repositório que não sejam a tag atual nem a anterior; apagar releases de assets além das 2 mais recentes, sem tocar a `current` nem a anterior.

- [ ] **Step 1:** `rollout_test.sh`, sobre o projeto `silkroad-smoke` com imagens `:test` (sem pull, `ROLLOUT_SKIP_PULL=1`): (a) volume vazio → sai ≠ 0 com a mensagem do bootstrap; (b) com semente → sai 0, `release.env` escrito, `/api/readyz` 200; (c) segundo rollout com outra `ASSETS_RELEASE` → `current` aponta para ela e `PREVIOUS_ASSETS_RELEASE` é a primeira; (d) há exatamente um backup novo por rollout.
- [ ] **Step 2:** Rodar. Esperado: falha (script inexistente).
- [ ] **Step 3:** Implementar `rollout.sh`.
- [ ] **Step 4:** `deploy/test/rollout_test.sh`. Esperado: PASS nos quatro casos.
- [ ] **Step 5:** Commit `feat(deploy): rollout on the Docker host`.

### Task 7: `publish.sh` e configurações

**Files:**
- Create: `deploy/publish.sh`, `deploy/settings.sh`, `deploy/deploy.env`

**Interfaces:**
- Produces: `deploy.env` com `DEPLOY_PATH=silkroad`, `IMAGE_REPO=ghcr.io/<github-user>/silkroad`, `SRO_DOMAIN=game.example.com` e `PLATFORM` comentado. Sem segredos.
- Produces: `settings.sh` com `load_settings VAR...` (ambiente vence `deploy/local.env`, que vence `deploy.env`), mesma semântica de `tools/deploy/settings.sh` do deploy em que este se baseia.
- Produces: `generated_dirs` em `settings.sh`, que imprime o `client-public` e o `game-data/1.150` resolvidos com `node -e` importando `scripts/lib/generatedRoot.mjs` (nunca um caminho montado à mão; AGENTS.md "Worktrees").
- Produces: `publish.sh` imprime só a tag em stdout; o resto vai para stderr.

- [ ] **Step 1:** Implementar `publish.sh`: conferir que `server.srogz` e `client-public/assets/packs/manifest.json` existem; calcular a tag (fontes: `apps/server/{cmd,internal,config,go.mod,go.sum}`, `apps/client-next`, `deploy/docker`, `deploy/identity`, `pnpm-lock.yaml`); checagem de visibilidade do ghcr igual à do deploy em que este se baseia (antes e depois); builder `silkroad-publish` (docker-container) quando o driver atual é `docker`; build local das três imagens com `:test` e `deploy/test/smoke.sh` (pula com `SKIP_SMOKE=1`, avisando); `buildx build --push --provenance=false` das três para `PLATFORM`.
- [ ] **Step 2:** Rodar `deploy/publish.sh` de verdade contra o ghcr. Esperado: smoke ok, três tags `server-<tag>`, `identity-<tag>`, `web-<tag>` publicadas, pacote privado. Isto exige `docker login ghcr.io` com token `write:packages` feito pelo dono; se não estiver feito, parar e pedir ao dono para rodar `! docker login ghcr.io`.
- [ ] **Step 3:** Commit `feat(deploy): image publication`.

### Task 8: `deploy.sh`, assets por rsync e README

**Files:**
- Create: `deploy/deploy.sh`, `deploy/README.md`
- Test: `deploy/test/deploy_guard_test.sh`

**Interfaces:**
- Consumes: `publish.sh` (Task 7), `rollout.sh` (Task 6), `settings.sh`.
- Produces: `deploy.sh` com `SKIP_PUBLISH=1 IMAGE_TAG=<tag>` para só fazer o rollout de uma tag publicada.

Comportamento: recusar `DEPLOY_PATH` vazio, `/`, `.`, `..`, com aspas,; no servidor, recusar uma pasta não vazia que não tenha o marcador `.silkroad`. Assets: `ASSETS_RELEASE` = os 16 primeiros caracteres do sha256 de `assets/packs/manifest.json`; se `~/silkroad/assets/releases/$ASSETS_RELEASE` já existir, pular; senão `rsync -a --delete --link-dest=../<release em current>` para `releases/$ASSETS_RELEASE.partial` e renomear no fim. A lista do que vai: o que `apps/client-next/tools/beta/public-data.mjs` publica (ler esse arquivo e replicar a seleção num `--include-from` gerado; registrar no README de onde veio a regra). Stack: `tar` de `docker-compose.prod.yml`, `rollout.sh`, `config/`, `.env.prod.example`, `accounts.sh`, preservando `.env.prod`, `release.env`, `backups/`, `assets/` (mesma troca atômica do deploy em que este se baseia). Por fim, `ssh` rodando `rollout.sh` com `IMAGE_REPO`, `IMAGE_TAG` e `ASSETS_RELEASE`.

- [ ] **Step 1:** `deploy_guard_test.sh`: chama `deploy.sh` com `DRY_GUARD=1` (só valida o caminho e sai) para `DEPLOY_PATH` em `"" / . .. another-stack another-stack/gameserver "a'b"`. Esperado: todos saem ≠ 0. Para `silkroad`: sai 0.
- [ ] **Step 2:** Rodar. Esperado: falha (script inexistente). Implementar. Rodar. Esperado: PASS.
- [ ] **Step 3:** `README.md` em português: pré-requisitos (dados gerados, `docker login ghcr.io`, rede `traefik` no servidor, DNS `game.example.com`), primeiro deploy (`deploy.sh` → `.env.prod` → `accounts.sh --bootstrap` → `deploy.sh` de novo), deploy normal, só rollout de tag, rollback (`IMAGE_TAG` e `ASSETS_RELEASE` do `PREVIOUS_*` do `release.env`), restaurar backup, criar conta, puxar o upstream (`git fetch upstream && git merge upstream/main`, depois `deploy.sh`, que roda o smoke).
- [ ] **Step 4:** Primeiro deploy real, com o dono acompanhando (ação externa: confirmar antes): `deploy.sh`; o dono cria o `.env.prod` no servidor; `accounts.sh --bootstrap <id>`; `deploy.sh` de novo; abrir `https://game.example.com` e logar.
- [ ] **Step 5:** Commit `feat(deploy): deploy command, asset sync and manual`.
