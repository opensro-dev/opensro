# ===========================================================================
# sro-agent-accounts-policy.hcl - Agent-only access to account catalog chunks.
# Bind to namespace sro, job sro-agent, group agent, task agent when applying.
# The implicit workload policy covers exact task paths, not these descendants.
# ===========================================================================

namespace "sro" {
  variables {
    path "nomad/jobs/sro-agent/agent/agent/accounts/*" {
      capabilities = ["read", "list"]
    }
  }
}
