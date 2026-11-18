# Painel admin (fork)

Data: 2026-11-17. Branch: `feat/admin-panel`.

## Por que

O dono quer administrar o servidor sem SSH e sem script: mexer em personagens (incluindo level e SP) e criar
contas. O painel roda num container separado da stack de `deploy/docker-compose.prod.yml`.

## Objetivos

- Página de personagem: busca pelo nome exato, ficha (level, level máximo, EXP, SP, pontos de status, STR/INT,
  gold, HP/MP, online) e as ações que o GameWorld já tinha (resgate, itens, silk, gold, reset de stats, PK, mute,
  kick, emblema), mais definir level e definir SP.
- Página de contas: criar, trocar senha, desativar ou reativar, consultar.
- Tudo auditado pelo próprio servidor, com operador e motivo.

## Não objetivos

- Login no painel, papéis e várias pessoas de staff. Por enquanto só o dono usa. O nome do operador vem de
  `ADMIN_OPERATOR_NAME`, então dá para separar pessoas depois sem mexer no servidor.
- Busca parcial de personagem, listas, gráficos de economia, avisos globais e settings do shard. As APIs
  existem no GameWorld e podem entrar depois.
- Bloqueio de conta com prazo (as rotas `block` da API de contas). Desativar cobre o caso de hoje.

## Anti-objetivos

- Expor o painel na internet.
- Afrouxar as regras de loopback das APIs privadas do upstream.
- Escrever no SQLite do shard por fora do GameWorld.

## O que o código já impunha

- As ações de personagem ficam no GameWorld (`/internal/operations/player`, porta de controle 8791). Só
  respondem a um par em loopback, com `X-SRO-Local-Diagnostics: 1`, sem `Origin` e com o token de
  `<authority>/operator-token`. Sem esse arquivo, a API fica desligada.
- A API de contas fica no Agent (`127.0.0.1:8789`, só loopback, e o upstream recusa outro endereço), com o
  token `agent-provisioning-token`.
- Toda ação de personagem desconecta o jogador antes de gravar (lease de controle do transporte).

## Abordagens consideradas

Para o level e o SP:

1. **Ações novas na API de operador (escolhida).** Funcionam com o shard no ar e usam a mesma auditoria. Mexem em
   arquivos do upstream, o que o design do deploy evitava. O dono aceitou.
2. Ferramenta offline no molde do `sro-rename-character`. Não toca no upstream, mas exige parar o shard.

Para o painel:

1. **Serviço Go com HTML renderizado no servidor (escolhida).** Usa só a biblioteca padrão, gera uma imagem
   pequena e segue a linguagem da stack.
2. SPA em React com backend Node. A interface fica mais rica, mas são dois projetos a mais para manter.
3. Painel genérico (Appsmith/Retool). Pesado, configuração fora do git e ainda precisaria de ponte para o
   loopback.

Para o acesso:

1. **Túnel SSH (escolhida).** Porta só no loopback do host e nada na internet.
2. Subdomínio no Traefik. Exigiria login, CSRF e uma ponte entre redes.

## Como ficou

- **Servidor** (`apps/server`): `set-level` e `set-sp` em `internal/agent/api/operator.go`, ligadas em
  `cmd/services/sro-gameworld/player_operations.go`. `OperatorSetLevel` (`internal/game/action/operator_progress.go`)
  converte o level alvo em EXP e passa pela mesma caminhada de EXP de um kill (`ExperienceRefundUpdater`, sem
  taxa). Subir dá STR/INT e pontos por level e move o level máximo. Descer mantém isso, como o level-down
  nativo. O teto é o `SRO_LEVEL_CAP` do shard. A leitura do personagem passou a mostrar SP, EXP e level máximo.
  São ferramentas de staff atrás da credencial de operador (port-only, não nativo), como o reset de stats, por
  isso não têm flag `SRO_`.
- **Painel** (`deploy/admin`, módulo Go próprio): `admin serve` roda na rede do GameWorld
  (`network_mode: service:gameworld-global-official`). `admin relay` roda na rede do `identity` e repassa ao Agent
  só `POST /v1/accounts`, `GET /v1/accounts/{id}`, `PUT .../password` e `PUT .../disabled`. O token de
  provisionamento continua indo em toda chamada.
- **Vários shards:** a página de personagem tem um seletor de shard (`ADMIN_SHARDS`, `id=url`). O shard
  principal responde no loopback do próprio painel. O PVP responde pelo `admin-relay-pvp`, um relay em modo
  `operator` na rede do `gameworld-pvp`, que só repassa `GET/POST /internal/operations/player`. Cada shard
  tem o seu token (`admin-secrets/operator-token-<shard>`), copiado pelo `provision` daquele shard.
- **Proteções do painel:** só responde com Host de loopback (contra DNS rebinding) e recusa POST de outro site.
  Cada formulário gera um id de requisição novo.
- **Segredos:** o job `provision` cria o `operator-token` no authority do shard e copia ele e o token de
  provisionamento para o volume `admin-secrets`, o único que o painel monta. O painel nunca vê o anel de chaves
  de sessão nem o banco.
- **Rollout:** a imagem `admin-<tag>` entra no build, no publish e no pull. Depois de subir o GameWorld, o
  rollout recria `admin-relay` e `admin`, porque um GameWorld recriado deixaria o painel numa rede que não
  existe mais. A volta automática do rollout faz o mesmo, sem falhar quando a versão anterior não tem o painel.

## Verificação

- `go test` de `internal/game/action` (level sobe e desce pela caminhada real de EXP, recusas, SP) e de
  `internal/agent/api` (validação e despacho das duas ações).
- `cd deploy/admin && go test ./...`.
- `deploy/test/smoke.sh`: o painel sobe, cria uma conta que loga e alcança a API de operador do GameWorld com o
  token gerado.

## Deixado para quem continuar

- Testar `set-level` num personagem real no ar. Não há personagem num shard novo do smoke, e o teste unitário
  cobre a regra.
