# ===========================================================================
# gameworld.nomad.hcl - Nomad lifecycle, placement, and secret delivery for the service.
# ===========================================================================

variable "node_kernel" {
  type        = string
  default     = "windows"
  description = "attr.kernel.name of the nodes that may run this job (windows or linux)"
}

variable "task_user" {
  type        = string
  default     = ""
  description = "OS account the task runs as; empty keeps the Nomad client's account"
}

variable "task_uid" {
  type    = number
  default = -1
}

variable "task_gid" {
  type    = number
  default = -1
}

variable "datacenters" {
  type    = list(string)
  default = ["dc1"]
}

variable "nomad_namespace" {
  type = string
}

variable "shard_id" {
  type = string
}

variable "binary_path" {
  type = string
}

variable "catalog_path" {
  type = string
}

variable "authority_dir" {
  type = string
}

variable "server_game_data_root" {
  type = string
}

variable "server_game_data_manifest_digest" {
  type = string
}

variable "cert_dir" {
  type = string
}

variable "transport_cert_file" {
  type    = string
  default = ""
}

variable "transport_key_file" {
  type    = string
  default = ""
}

variable "transport_tls_id" {
  type = string
}

variable "host_network" {
  type    = string
  default = "loopback"
}

variable "control_port" {
  type = number
}

variable "transport_port" {
  type = number
}

variable "release_id" {
  type = string
}

variable "private_network" {
  type    = string
  default = "0"
}

# "1" serves /debug/pprof/ on the loopback control listener (sro-nomad
# deploy -pprof, loopback clusters only).
variable "transport_pprof" {
  type    = string
  default = "0"
}

variable "allowed_origins" {
  type = string
}

variable "gm_characters" {
  type    = string
  default = ""
}

variable "move_path_guard" {
  type    = string
  default = "enforce"
}

variable "move_client_clip" {
  type    = string
  default = "apply"
}

# Beta operator switches ("on" or "off"). beta_starter_kit gives every
# character an unlimited return scroll and +100% speed scroll (backfilled on
# entry); beta_player_map shows every online player on the world map (M).
variable "beta_starter_kit" {
  type    = string
  default = "on"
}

variable "beta_player_map" {
  type    = string
  default = "on"
}

# Beta builds share the 5000 total mastery budget. Set off for native
# CH 300 / EU min(2 * level, 240); individual mastery ceilings never change.
variable "beta_mastery" {
  type    = string
  default = "on"
}

# beta_silk is the silk one in-world hour earns into the account's real Item
# Mall wallet (BUG-062, port-only): earning pauses at 1500, and a new account
# starts with 300. Earned silk is REAL and survives "off", which only stops
# new credits; a launch wipes or zeroes it. "on" is 50.
variable "beta_silk" {
  type    = string
  default = "50"
}

# maintenance_gate_path is the maintenance release gate (ops/release
# deploy.py): while the file exists, EnterWorld tokens go only to the
# accounts it lists. Empty disables the gate.
variable "maintenance_gate_path" {
  type    = string
  default = ""
}

# party_masteries puts each member's two main mastery trees on the quick party
# board. Set off for the native roster rows.
variable "party_masteries" {
  type    = string
  default = "on"
}

# storage_auto_stack lets a storage quick move (right-click, Ctrl+click) land
# on a matching stack (port-only, not native; owner decision 2026-10-11).
# "off" restores the native empty-slot rule.
variable "storage_auto_stack" {
  type    = string
  default = "on"
}

# beta_growth holds every level's EXP and skill EXP to the level-1 kill pace,
# multiplies skill EXP by beta_skill_exp_rate on top, makes a kill's assigned
# rewards and ordinary drop passes beta_drop_rate times over (the unique
# prepass and the gold heap count stay native), multiplies gold heaps by
# beta_gold_rate and makes a rare (SoX) equipment drop beta_rare_rate times as
# likely. beta_drop_cap keeps a kill to that many ordinary items (a uniform
# random subset; gold heaps and a unique's prepass are never cut; 0 = no cap).
# "off" restores the native rates.
variable "beta_growth" {
  type    = string
  default = "on"
}

variable "beta_skill_exp_rate" {
  type    = string
  default = "100"
}

variable "beta_drop_rate" {
  type    = string
  default = "20"
}

variable "beta_drop_cap" {
  type    = string
  default = "16"
}

# 250 keeps gold per kill near its level before #463 stopped multiplying the
# heap count by beta_drop_rate (owner, 2026-10-10).
variable "beta_gold_rate" {
  type    = string
  default = "250"
}

# instant_inventory_expansion makes quest bag slots usable at the turn-in
# (port-only, not native: v1.150 learns its bag size at world entry). The
# owner approved it on for the beta; "off" restores the native wait.
variable "instant_inventory_expansion" {
  type    = string
  default = "on"
}

variable "beta_rare_rate" {
  type    = string
  default = "5"
}

# stack_sizes raises item stack caps per group (port-only, not native), e.g.
# "potion=2000,elixir=50". Groups: potion, petpotion, elixir, luckypowder,
# magicstone, attrstone (stones merge only with an equal assimilation value).
# Empty keeps every itemdata cap; an unknown group or a cap outside 1..65535
# stops GameWorld at boot.
variable "stack_sizes" {
  type    = string
  default = ""
}

# The community site's public read API listener (internal/agent/publicstats):
# a loopback address, or "off". sro-nomad gives the first shard the default
# and every other shard "off".
variable "public_api_addr" {
  type    = string
  default = "127.0.0.1:8790"
}

variable "cpu" {
  type    = number
  default = 2000
}

variable "memory_mb" {
  type    = number
  default = 1024
}

# sro-nomad replaces __SHARD_ID__ with the validated catalog ID before
# submitting this template. Nomad job IDs are block labels, not HCL values,
# so they cannot reference an input variable directly.
job "sro-gameworld-__SHARD_ID__" {
  namespace   = var.nomad_namespace
  datacenters = var.datacenters
  type        = "service"

  meta {
    sro_release_id       = var.release_id
    sro_transport_tls_id = var.transport_tls_id
  }

  constraint {
    attribute = "${attr.kernel.name}"
    value     = var.node_kernel
  }

  # A shard is stateful. Only a node whose operator-owned metadata explicitly
  # claims this shard may run its sole writer.
  constraint {
    attribute = "${meta.sro_shards}"
    operator  = "set_contains"
    value     = var.shard_id
  }

  update {
    max_parallel      = 1
    min_healthy_time  = "10s"
    healthy_deadline  = "3m"
    progress_deadline = "5m"
    auto_revert       = true
  }

  group "gameworld" {
    count = 1

    ephemeral_disk {
      size = 2048
    }

    network {
      mode = "host"

      port "control" {
        static       = var.control_port
        host_network = var.host_network
      }

      # One number is reserved for both the TCP WebSocket/HTTP listener and
      # the UDP WebTransport listener. raw_exec uses host networking, so no
      # port translation is involved.
      port "transport" {
        static       = var.transport_port
        host_network = var.host_network
      }
    }

    restart {
      attempts = 5
      interval = "1m"
      # A crashed process cannot release its 10-second Agent ownership lease.
      # Wait beyond that lease before starting a fresh process identity so one
      # crash consumes one restart attempt rather than several refused boots.
      delay = "12s"
      mode  = "fail"
    }

    reschedule {
      attempts       = 10
      interval       = "1h"
      delay          = "5s"
      delay_function = "exponential"
      max_delay      = "2m"
      unlimited      = false
    }

    task "gameworld" {
      driver = "raw_exec"
      # Empty on Windows (the Nomad service account runs the task). On Linux
      # the deployer names a dedicated unprivileged account.
      user = var.task_user

      config {
        command = var.binary_path
        # Keep host-level control-plane and cloud credentials out of this
        # unisolated native process. Nomad's allocation variables remain.
        denied_envvars = [
          "NOMAD_TOKEN",
          "NOMAD_LICENSE",
          "NOMAD_LICENSE_PATH",
          "CONSUL_*",
          "VAULT_*",
          "AWS_*",
          "AZURE_*",
          "ARM_*",
          "GOOGLE_*",
          "GITHUB_*",
          "GITLAB_*",
        ]
      }

      env {
        # Soft Go-runtime budget: three quarters of the task's memory, the
        # rest left to non-heap memory. It was a flat 128MiB, below what one
        # player's world already keeps live (about 100MB of monsters, nav and
        # references): the collector then ran almost without pause and took
        # 82% of 3.6 cores with one player online.
        GOMEMLIMIT = "${floor(var.memory_mb * 3 / 4)}MiB"
        SRO_SHARD_CATALOG_PATH             = var.catalog_path
        SRO_SHARD_ID                       = var.shard_id
        SRO_AGENT_URL_REQUIRE              = "1"
        SRO_AGENT_IDENTITY_FILE            = "${NOMAD_SECRETS_DIR}/nomad_sro_agent.jwt"
        SRO_AGENT_SESSION_PUBLIC_KEYS_PATH = "${NOMAD_SECRETS_DIR}/agent-session-public-keys.json"
        SRO_AUTHORITY_STATE_DIR            = var.authority_dir
        SRO_AUTHORITY_REQUIRE              = "1"
        SRO_GM_CHARACTERS                  = var.gm_characters
        SRO_SERVER_GAME_DATA_ROOT          = var.server_game_data_root
        SRO_SERVER_GAME_DATA_MANIFEST_DIGEST = var.server_game_data_manifest_digest
        SRO_GAMEWORLD_CONTROL_ADDR         = "${NOMAD_IP_control}:${NOMAD_PORT_control}"
        SRO_GAMEWORLD_PRIVATE_NETWORK      = var.private_network
        SRO_BENCHMARK_FIXTURE_CONTROL       = var.host_network == "loopback" && var.nomad_namespace == "default" ? "1" : "0"
        SRO_MOVE_PATH_GUARD                = var.move_path_guard
        SRO_MOVE_CLIENT_CLIP               = var.move_client_clip
        SRO_BETA_STARTER_KIT               = var.beta_starter_kit
        SRO_BETA_PLAYER_MAP                = var.beta_player_map
        SRO_BETA_MASTERY                   = var.beta_mastery
        SRO_BETA_SILK                      = var.beta_silk
        SRO_MAINTENANCE_GATE_PATH          = var.maintenance_gate_path
        SRO_PARTY_MASTERIES                = var.party_masteries
        SRO_STORAGE_AUTO_STACK             = var.storage_auto_stack
        SRO_BETA_GROWTH                    = var.beta_growth
        SRO_BETA_SKILL_EXP_RATE            = var.beta_skill_exp_rate
        SRO_BETA_DROP_RATE                 = var.beta_drop_rate
        SRO_BETA_GOLD_RATE                 = var.beta_gold_rate
        SRO_INSTANT_INVENTORY_EXPANSION    = var.instant_inventory_expansion
        SRO_BETA_RARE_RATE                 = var.beta_rare_rate
        SRO_BETA_DROP_CAP                  = var.beta_drop_cap
        SRO_STACK_SIZES                    = var.stack_sizes
        SRO_PUBLIC_API_ADDR                = var.public_api_addr
        SRO_PUBLIC_API_TOKEN_PATH          = "${NOMAD_SECRETS_DIR}/public-api-token"
        TRANSPORT_WT_ADDR                  = "${NOMAD_IP_transport}:${NOMAD_PORT_transport}"
        TRANSPORT_WS_ADDR                  = "${NOMAD_IP_transport}:${NOMAD_PORT_transport}"
        TRANSPORT_CERT_DIR                 = var.cert_dir
        TRANSPORT_CERT_FILE                = var.transport_cert_file
        TRANSPORT_KEY_FILE                 = var.transport_key_file
        TRANSPORT_ALLOWED_ORIGINS          = var.allowed_origins
        SRO_TRANSPORT_TLS_ID               = var.transport_tls_id
        TRANSPORT_PPROF                    = var.transport_pprof
        SRO_RELEASE_ID                     = var.release_id
      }

      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-gameworld-__SHARD_ID__/gameworld/gameworld" }}
{{ .agent_session_public_keys }}
{{ end }}
EOH

        destination = "secrets/agent-session-public-keys.json"
        change_mode = "noop"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      # The privacy-write token. index, not .public_api_token: a missing map
      # key must render empty, never the template's "<no value>". An empty
      # or short file leaves the write unserved. The GameWorld reads it once at boot, so a
      # rotated token applies at the next restart rather than kicking players.
      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-gameworld-__SHARD_ID__/gameworld/gameworld" }}{{ index . "public_api_token" }}{{ end }}
EOH

        destination = "secrets/public-api-token"
        change_mode = "noop"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      identity {
        name        = "sro_agent"
        aud         = ["sro-agent"]
        file        = true
        ttl         = "5m"
        change_mode = "noop"
      }

      # Resolve Agent through Nomad native service discovery. An empty
      # snapshot renders no URL and remains watched. The application-level
      # SRO_AGENT_URL_REQUIRE gate then refuses startup without falling back
      # to loopback; a later registration change restarts the task.
      template {
        data = <<EOH
{{ range nomadService 1 (env "NOMAD_ALLOC_ID") "sro-agent" }}
SRO_AGENT_URL=http://{{ .Address }}:{{ .Port }}
{{ end }}
EOH

        destination = "local/agent.env"
        env         = true
        change_mode = "restart"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      service {
        provider = "nomad"
        name     = "sro-gameworld-${var.shard_id}"
        port     = "transport"
        tags     = ["shard:${var.shard_id}"]

        check {
          name     = "liveness"
          type     = "http"
          path     = "/transport/healthz"
          interval = "10s"
          timeout  = "2s"

          check_restart {
            limit = 3
            grace = "30s"
          }
        }

        # Readiness is intentionally not a restart trigger. Dependency or
        # authority degradation removes the shard from service without
        # turning a shared outage into a fleet-wide restart storm.
        check {
          name      = "readiness"
          type      = "http"
          path      = "/transport/readyz"
          interval  = "5s"
          timeout   = "2s"
          on_update = "require_healthy"
        }
      }

      resources {
        cpu    = var.cpu
        memory = var.memory_mb
      }

      logs {
        max_files     = 10
        max_file_size = 100
      }

      shutdown_delay = "5s"
      kill_timeout   = "60s"
    }
  }
}
