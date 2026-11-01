# Documentation

## Start here

| Document | For |
| --- | --- |
| [Getting started](guides/GETTING_STARTED.md) | Running the server and client locally, from nothing |
| [Architecture](architecture/ARCHITECTURE.md) | How the server, client and asset pipeline fit together |
| [Contributing](../.github/CONTRIBUTING.md) | Getting a change merged: setup, native first, flags for non-native behaviour |
| [AGENTS.md](../AGENTS.md) | Code style (id Software) and working rules for contributors and coding agents |

## Building and measuring

| Document | For |
| --- | --- |
| [Asset pipeline](architecture/ASSET_PIPELINE.md) | Building and packaging game data |
| [Asset delivery](architecture/ASSET_DELIVERY.md) | How game data reaches the browser: sizes, packs, caching |
| [Profiling](guides/PROFILING.md) | Measuring frame rate and allocations, finding the cost, proving a fix |
| [3D viewer](guides/VIEWER.md) | The embeddable viewer page |
| [Server commands](../apps/server/cmd/README.md) | Server command catalog |

## Running a server

| Document | For |
| --- | --- |
| [Deployment](../apps/server/ops/docs/DEPLOYMENT.md) | Production topology, security, state and backups |
| [Nomad operations](../apps/server/ops/nomad/README.md) | Running the cluster |
| [Hosting the client](../apps/client-next/docs/HOSTING.md) | Serving the browser client behind an edge |
| [Release checklist](operations/RELEASE.md) | Cutting a release |
| [Accepted advisories](../apps/server/ops/docs/ACCEPTED_ADVISORIES.md) | Reviewed vulnerability exceptions |
| [Live item grants](operations/OPERATOR_ITEM_GRANTS.md) | Granting items on a running shard |
| [GM item picker](operations/GM_ITEM_PICKER.md) | The GM console's F3 picker |

## Features

| Document | For |
| --- | --- |
| [110-cap expansion](features/CAP110.md) | The optional cap, its imports, switches and open checks |
| [Attack pet system](pet-expansion/ATTACK-PET-SYSTEM.md) | Skills, gear, procs, drops, the website guide and source owners |
| [Pet drop configuration](pet-expansion/PET-LOOT-DB-CONFIG.md) | Native equipment tables and the DB drop factor |
| [Pet assets](pet-expansion/assets/README.md) | Icons, their build and provenance |
| [Experimental graphics](features/EXPERIMENTAL_GRAPHICS.md) | Port-only render stages |
| [Small features](features/SMALL_FEATURES.md) | Grab pets, pet nameplates, GM training dummy, beta mastery |

## Native research

| Document | For |
| --- | --- |
| [Reverse engineering](native/REVERSE_ENGINEERING.md) | Python-only native investigation and package setup |
| [Research progress](native/RESEARCH_PROGRESS.md) | Durable labels, binary identity, evidence and handoffs |

Per-feature native evidence lives in `../research`, outside this repository.

## Coding agents

| Document | For |
| --- | --- |
| [Agent guides](agents/README.md) | Rules that gates and reviewers hold, by topic |
| [Agent coordination](agents/AGENT-COORDINATION.md) | The shared log, reviews, merges and evidence |

[RETIRED.md](RETIRED.md) lists documents removed from the tree, and how to restore them.
