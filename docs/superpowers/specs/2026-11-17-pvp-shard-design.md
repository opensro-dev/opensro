# Shard PVP: nível máximo, Samarkand e equipamento pronto

Data: 2026-11-17. Fork `diorgesl/opensro`. Port-only, não nativo: nada aqui muda o shard
`global-official` nem o padrão nativo.

## Intenção

Um segundo shard, chamado **PVP**, só para combate entre jogadores. Ninguém farma nem sobe de
nível: o personagem nasce pronto, aparece em Samarkand e compra lá o equipamento final. Todo mundo
começa no mesmo patamar e a diferença fica na build (masteries, itens, atributos) e na habilidade.

## Objetivos

- Um shard `pvp` (nome "PVP") ao lado do `global-official`, na mesma stack Docker, mesma imagem e
  mesmos dados (a variante VSRO 1.188 + v1.696 de `deploy/docs/2026-11-17-variante-vsro188.md`).
- Personagem criado no PVP nasce:
  - no nível máximo do shard (`SRO_LEVEL_CAP`, hoje 110);
  - com todas as masteries da raça no máximo e todas as skills dessas masteries aprendidas
    ("tudo liberado", escolha do dono);
  - em Samarkand;
  - com ouro suficiente para não pensar em ouro (ver "Preços").
- Em Samarkand, NPCs vendem:
  - equipamento raro (`_RARE`) já **+12**;
  - pedras de alquimia para mudar atributos: pedras de atributo (brancos) e pedras mágicas (azuis).

## Não objetivos

- Mudar regras de combate, PK, murder, drop na morte ou fortalezas. O shard usa as regras atuais.
- Arena, ranking ou eventos de PvP próprios.
- Converter personagens que já existem: o shard é novo e vazio.
- Qualquer mudança no `global-official`.

## Anti-objetivos

- Uma flag do PVP ligada por engano no `global-official` não pode dar nível ou itens a ninguém lá.
  O env separado não basta: o código só aplica `SRO_PVP_SERVER` quando `SRO_SHARD_ID` é `pvp`; em
  qualquer outro shard a flag é ignorada com um aviso no log. (Decisão minha, revisão.)
- Item +12 de graça não pode virar ouro: vender para NPC paga 0 (como o kit do beta faz,
  `FreeGrantItem`).

## Restrições

- Regra do repo (AGENTS.md): comportamento não nativo fica atrás de `SRO_<FEATURE>`, desligado por
  padrão, marcado "port-only, not native", testado ligado e desligado.
- Estilo id Software, banners, constantes nomeadas, sem arquivo acima de 1000 linhas.
- A criação de personagem roda no GameWorld de cada shard (`agent/api/characters.go`,
  `handleCharacterCreate`, montado em `cmd/services/sro-gameworld/wiring.go`). Por isso uma flag no
  env do gameworld do PVP vale só para o PVP.
- A soma das masteries tem limite (`catalogue/levels.go`, `MasteryTotal`). Para caber todas no
  máximo, o PVP usa o ajuste que já existe, `SRO_BETA_MASTERY`, com cota suficiente para todas as
  masteries da raça no nível máximo (7 chinesas e 6 europeias, ×110).
- Os dados são os mesmos dos dois shards (uma imagem só). Nenhuma regra do PVP pode depender de
  dado que só exista com o PVP ligado.

## Abordagens

1. **Regras no servidor, aplicadas uma vez na criação do personagem** (escolhida). A criação lê a
   flag e já grava nível, masteries, skills, ouro e posição. A loja usa o mecanismo de lojas
   extras que monta a loja de alquimia de Jangan (`commerce/alchemyshop.go`, `extragoods.go`).
   Acontece uma vez, por personagem, e dá para testar.
2. **Regras aplicadas a cada entrada no mundo**, como o kit do beta (`enterworld/starterkit.go`).
   Também cobriria personagens antigos, mas reaplica a cada login e briga com o que o jogador mudou.
   Num shard novo a vantagem não existe.
3. **Script de operador** que edita o banco depois da criação. Manual, sujeito a corrida com o
   personagem online e difícil de testar.

O dono escolheu a 1.

## O que foi decidido

### Shard

- Id `pvp`, nome "PVP", `nativeServerId` 2, `publicTransportUrl` `/shards/pvp`.
- Mais um serviço `gameworld-pvp` no `docker-compose.prod.yml`, com volume `shard-pvp`, token de
  identidade próprio (`identity -shards global-official,pvp`), rota `/shards/pvp/transport/` no
  `nginx.conf`, e `env_file: ./shard-pvp.env`. `rollout.sh` passa a fazer backup, upgrade e rollout
  dos dois volumes e serviços. O `smoke.sh` sobe os dois.
- `shard-pvp.env` no host, com as flags do PVP, `SRO_LEVEL_CAP=110`, `SRO_BETA_MASTERY` e as mesmas
  flags de conteúdo do `shard.env`. Fica fora do repo como o `shard.env`; o repo traz um
  `shard-pvp.env.example`.

### Regras (uma flag, `SRO_PVP_SERVER`, port-only, desligada por padrão)

Com ela ligada, na criação:

- **Nível**: o cap do shard, com EXP zerada. Só o ganho automático por nível (+1 STR e +1 INT,
  `progression/levelup.go`) é aplicado; os 3 pontos livres de cada nível ficam para o jogador
  distribuir, porque a build é o que diferencia os jogadores e não existe como desfazer uma
  distribuição sem o pergaminho do mall. (Decisão minha, revisão.)
- **Masteries**: todas as da raça no nível máximo permitido (o nível do personagem).
- **Skills**: todas as skills dessas masteries com requisito atendido, na maior rank que os dados
  têm. Com o build atual (`SRO_CAP110_SKILLS` sem `SRO_CAP110_SKILL_DATABASE`) isso para nas ranks
  da mastery 100; as ranks 101-110 dependem de um export que o clone público não gera.
- **Posição**: Samarkand, no ponto de retorno da cidade.
- **Ouro**: uma quantia fixa (ver "Preços").
- **Itens que vendem para NPC**: pagam 0 enquanto a flag estiver ligada.

E em Samarkand:

- **Loja de equipamento PVP**: abas por tipo (armas, armaduras, escudos, acessórios), cada item já
  +12. Só o melhor grau para o nível 110: D11 Nova (`_11_A_RARE`) e os sets D11 (`_11_SET_A_RARE`,
  `_11_SET_B_RARE`). Ficam fora D10 (pior que D11 para quem já é 110) e as variantes
  `_RARE_ACTIVE` e `_RARE_HONOR`. Isso cabe em poucas abas de 45; se uma categoria passar de 45,
  ela se divide em mais abas. (Decisão minha, revisão.)
- **Loja de alquimia PVP**: as mesmas famílias de pedras de atributo e pedras mágicas da loja de
  Jangan, mas no grau 11 (a pedra precisa ter o mesmo grau do item, `item/alchemy/stones.go`; a de
  Jangan é grau 9 e não serviria). Sem elixires nem pós: o +12 da loja é o teto do shard.
  (Decisão minha, revisão.)

### Preços

- Tudo nas lojas PVP custa 1 de ouro.
- O personagem nasce com 100.000.000 de ouro, para reparo, poções e pedras sem limite prático.

## O que fica para quem constrói

- Onde a primeira posição do personagem é resolvida hoje (`characterFromCreate` não define posição)
  e qual é o ponto de retorno de Samarkand nos dados.
- Quais NPCs de Samarkand recebem as lojas e como as abas são sintetizadas (seguir
  `alchemyshop.go`).
- Como entregar o item +12: o pacote de venda já carrega `Plus`, `Variance` e opções mágicas
  (`commerce/package.go`, `grant.go`). Atributos brancos e azuis do item vendido: o padrão do pacote
  nativo (sem bônus), o jogador ajusta com as pedras.
- Como masteries e skills são gravadas no personagem e qual rotina já resolve "skills de uma
  mastery até um nível".
- Testes: flag desligada não muda nada; ligada, personagem criado tem nível, masteries, skills,
  posição e ouro esperados; loja só existe com a flag; venda para NPC paga 0.
