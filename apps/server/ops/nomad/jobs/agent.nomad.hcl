# ===========================================================================
# agent.nomad.hcl - Nomad lifecycle, placement, and secret delivery for the service.
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

variable "binary_path" {
  type = string
}

variable "catalog_path" {
  type = string
}

variable "directory_state_path" {
  type = string
}

# The live account database (internal/security/auth/accounts.go). It must
# outlive allocations: it is the login authority.
variable "accounts_db_path" {
  type = string
}

variable "host_network" {
  type    = string
  default = "loopback"
}

variable "agent_port" {
  type    = number
  default = 8787
}

# Loopback port of the Agent's account provisioning API. A second Agent on the
# same host (an isolated test stack) needs its own. sro-nomad always passes it
# (provisioning.DefaultPort unless overridden); this default is a fallback.
variable "agent_provisioning_port" {
  type    = number
  default = 8789
}

variable "release_id" {
  type = string
}

variable "private_network" {
  type    = string
  default = "0"
}

variable "allowed_origins" {
  type = string
}

variable "identity_issuer" {
  type = string
}

variable "identity_jwks_url" {
  type = string
}

# In-game bug reports (internal/agent/bugreport). The webhook that turns
# them on is a secret and arrives through the Nomad variable, not here.
variable "bug_report_replay_default" {
  type    = string
  default = "1"
}

variable "bug_report_max_bytes" {
  type    = string
  default = "10485760"
}

variable "cpu" {
  type    = number
  default = 500
}

variable "memory_mb" {
  type    = number
  default = 256
}

job "sro-agent" {
  namespace   = var.nomad_namespace
  datacenters = var.datacenters
  type        = "service"

  meta {
    sro_release_id = var.release_id
  }

  constraint {
    attribute = "${attr.kernel.name}"
    value     = var.node_kernel
  }

  constraint {
    attribute = "${meta.sro_agent}"
    value     = "true"
  }

  update {
    max_parallel      = 1
    min_healthy_time  = "10s"
    healthy_deadline  = "2m"
    progress_deadline = "5m"
    auto_revert       = true
  }

  group "agent" {
    count = 1

    ephemeral_disk {
      size = 1024
    }

    network {
      mode = "host"

      port "http" {
        static       = var.agent_port
        host_network = var.host_network
      }
    }

    restart {
      attempts = 5
      interval = "1m"
      delay    = "2s"
      mode     = "fail"
    }

    reschedule {
      attempts       = 10
      interval       = "1h"
      delay          = "5s"
      delay_function = "exponential"
      max_delay      = "2m"
      unlimited      = false
    }

    task "agent" {
      driver = "raw_exec"
      # Empty on Windows (the Nomad service account runs the task). On Linux
      # the deployer names a dedicated unprivileged account.
      user = var.task_user

      config {
        command = var.binary_path
        # raw_exec inherits the Nomad client's host environment. Never let
        # control-plane or cloud credentials cross into the game process.
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
        SRO_SHARD_CATALOG_PATH            = var.catalog_path
        SRO_AGENT_ACCOUNTS_PATH           = "${NOMAD_SECRETS_DIR}/accounts.json"
        SRO_AGENT_API_ADDR                = "${NOMAD_IP_http}:${NOMAD_PORT_http}"
        SRO_AGENT_DIRECTORY_STATE_PATH    = var.directory_state_path
        SRO_AGENT_ACCOUNTS_DB_PATH        = var.accounts_db_path
        SRO_AGENT_PROVISIONING_TOKEN_PATH = "${NOMAD_SECRETS_DIR}/provisioning-token"
        SRO_AGENT_PROVISIONING_ADDR       = "127.0.0.1:${var.agent_provisioning_port}"
        SRO_AGENT_PRIVATE_NETWORK         = var.private_network
        SRO_AGENT_ALLOWED_ORIGINS         = var.allowed_origins
        SRO_NOMAD_IDENTITY_ISSUER         = var.identity_issuer
        SRO_NOMAD_JWKS_URL                = var.identity_jwks_url
        SRO_NOMAD_NAMESPACE               = var.nomad_namespace
        SRO_AGENT_SESSION_KEYRING_PATH    = "${NOMAD_SECRETS_DIR}/agent-session-keys.json"
        SRO_RELEASE_ID                    = var.release_id
        SRO_BUG_REPORT_REPLAY_DEFAULT     = var.bug_report_replay_default
        SRO_BUG_REPORT_MAX_BYTES          = var.bug_report_max_bytes
      }

      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-agent/agent/agent" }}
{{ .agent_session_keyring }}
{{ end }}
EOH

        destination = "secrets/agent-session-keys.json"
        change_mode = "noop"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-agent/agent/agent" }}{{ .agent_provisioning_token }}{{ end }}
EOH

        destination = "secrets/provisioning-token"
        change_mode = "restart"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      # SRO_BUG_REPORT_DISCORD_WEBHOOK exists only when the deployer set it:
      # without the item the file is empty and bug reports stay off.
      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-agent/agent/agent" }}{{ range $name, $value := . }}{{ if eq $name "bug_report_discord_webhook" }}SRO_BUG_REPORT_DISCORD_WEBHOOK={{ $value }}{{ end }}{{ end }}{{ end }}
EOH

        destination = "secrets/bug-report.env"
        env         = true
        change_mode = "restart"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      template {
        data = <<EOH
__ACCOUNT_VARIABLES__
EOH

        destination = "secrets/accounts.json"
        change_mode = "restart"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      service {
        provider = "nomad"
        name     = "sro-agent"
        port     = "http"

        check {
          name     = "liveness"
          type     = "http"
          path     = "/healthz"
          interval = "10s"
          timeout  = "2s"

          check_restart {
            limit = 3
            grace = "20s"
          }
        }

        check {
          name      = "readiness"
          type      = "http"
          path      = "/readyz"
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
        max_file_size = 50
      }

      shutdown_delay = "5s"
      kill_timeout   = "20s"
    }
  }
}
