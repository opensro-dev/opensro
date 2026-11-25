/*
===========================================================================

jobinputs.go - the Nomad job templates and the inputs a deployment fills

A resolved deployment (deployment.go) becomes two jobs: the Agent's and
one GameWorld per shard. This file reads their templates and builds the
input variables each job is rendered with - the Agent's, a GameWorld's
(with the shard-settings table's port-only values), and the template
texts the account catalog token is spliced into. Nomad Variables, the
secrets the jobs read at run time, are variables.go's.

===========================================================================
*/
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

/*
================
jobTemplates
================
*/
func (deployment *deployment) jobTemplates() ([]byte, []byte, error) {
	agentTemplate, err := os.ReadFile(
		filepath.Join(deployment.JobsDir, agentTemplateName),
	)
	if err != nil {
		return nil, nil, err
	}
	gameTemplate, err := os.ReadFile(
		filepath.Join(deployment.JobsDir, gameTemplateName),
	)
	if err != nil {
		return nil, nil, err
	}
	return agentTemplate, gameTemplate, nil
}

/*
================
agentVariables
================
*/
func (deployment *deployment) agentVariables() map[string]any {
	return deployment.nodeVariables(deployment.AgentCPU, deployment.AgentMemoryMB, map[string]any{
		"binary_path":  slashPath(deployment.AgentBinary),
		"catalog_path": slashPath(deployment.Catalog),
		"directory_state_path": slashPath(filepath.Join(
			deployment.StateDir,
			"agent",
			"shard-leases.json",
		)),
		"accounts_db_path": slashPath(filepath.Join(
			deployment.StateDir,
			"agent",
			"accounts.db",
		)),
		"host_network":            deployment.Network,
		"nomad_namespace":         deployment.Namespace,
		"agent_port":              deployment.AgentPort,
		"agent_provisioning_port": deployment.AgentProvisioningPort,
		"release_id":              deployment.AgentReleaseID,
		"private_network":         boolEnvValue(deployment.PrivateNetwork),
		"allowed_origins":         deployment.AllowedOrigins,
		"identity_issuer":         deployment.IdentityIssuer,
		"identity_jwks_url":       deployment.IdentityJWKSURL,
		"bug_report_replay_default": boolEnvValue(
			deployment.BugReports.ReplayDefault,
		),
		"bug_report_max_bytes": strconv.FormatInt(
			deployment.BugReports.MaxBytes,
			10,
		),
		"bug_report_directory": deployment.BugReports.Directory,
		"maintenance_message":  deployment.PortOnly.maintenanceMessage(),
		// Port-only (#3925): announced-but-unopened shards list as
		// coming soon on the title server list; only the Agent reads it.
		"coming_soon_shards": deployment.PortOnly.comingSoonShards(),
		// deploy.py MAINTENANCE_GATE, the same file the GameWorld jobs
		// carry: the Agent names a not-operating refusal MAINTENANCE
		// only while this gate file exists (#2861).
		"maintenance_gate_path": slashPath(filepath.Join(
			deployment.StateDir,
			"maintenance-gate.json",
		)),
	})
}

/*
================
gameVariables
================
*/
func (deployment *deployment) gameVariables(
	game shardDeployment,
) map[string]any {
	return deployment.nodeVariables(deployment.GameCPU, deployment.GameMemoryMB, deployment.PortOnly.jobVariables(map[string]any{
		"shard_id":                         game.Definition.ID,
		"binary_path":                      slashPath(deployment.GameBinary),
		"catalog_path":                     slashPath(deployment.Catalog),
		"authority_dir":                    slashPath(game.AuthorityDir),
		"server_game_data_root":            slashPath(deployment.DataPaths.RuntimeRoot),
		"server_game_data_manifest_digest": deployment.DataPaths.ManifestDigest,
		"cert_dir": slashPath(filepath.Join(
			deployment.StateDir,
			"dev-certs",
		)),
		// deploy.py MAINTENANCE_GATE: present only inside a maintenance release.
		"maintenance_gate_path": slashPath(filepath.Join(
			deployment.StateDir,
			"maintenance-gate.json",
		)),
		"transport_cert_file": deployment.TransportCert,
		"transport_key_file":  deployment.TransportKey,
		"transport_tls_id":    deployment.TransportTLSID,
		"transport_wt_addr":   deployment.TransportWTAddr,
		"host_network":        deployment.Network,
		"nomad_namespace":     deployment.Namespace,
		"control_port":        game.ControlPort,
		"transport_port":      game.TransportPort,
		"release_id":          deployment.GameReleaseID,
		"private_network":     boolEnvValue(deployment.PrivateNetwork),
		"allowed_origins":     deployment.AllowedOrigins,
		"gm_characters":       deployment.GMCharacters,
		"beta_mastery":        deployment.BetaMastery,
		"stack_sizes":         deployment.StackSizes,
		"public_api_addr":     game.PublicAPIAddr,
		"transport_pprof":     boolEnvValue(game.PprofAddr != ""),
		"pprof_addr":          pprofAddrValue(game.PprofAddr),
	}))
}

/*
================
renderGameWorldTemplate
================
*/
func renderGameWorldTemplate(template []byte, shardID string) []byte {
	return bytes.ReplaceAll(
		template,
		[]byte(gameWorldShardToken),
		[]byte(shardID),
	)
}

/*
================
renderAgentTemplate
================
*/
func renderAgentTemplate(
	template []byte,
	chunkCount int,
) ([]byte, error) {
	if bytes.Count(template, []byte(agentAccountToken)) != 1 {
		return nil, fmt.Errorf(
			"agent job template must contain exactly one %s token",
			agentAccountToken,
		)
	}
	if chunkCount < 1 {
		return nil, fmt.Errorf("agent account catalog has no chunks")
	}
	if chunkCount > maxAccountChunks {
		return nil, fmt.Errorf(
			"agent account catalog requires %d chunks, limit is %d",
			chunkCount,
			maxAccountChunks,
		)
	}
	var rendered strings.Builder
	for index := 0; index < chunkCount; index++ {
		fmt.Fprintf(
			&rendered,
			`{{ with nomadVar %q }}{{ .payload }}{{ end }}`,
			accountChunkVariablePath(index),
		)
		rendered.WriteByte('\n')
	}
	return bytes.Replace(
		template,
		[]byte(agentAccountToken),
		[]byte(rendered.String()),
		1,
	), nil
}
