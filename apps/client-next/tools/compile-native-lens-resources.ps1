<#
===========================================================================
compile-native-lens-resources.ps1 - compatibility entry for lens generation

The shared build owner now uses Windows PowerShell/.NET and the DirectX
runtime. CompilerRoot is accepted for older callers but is no longer needed.
===========================================================================
#>
param([string]$CompilerRoot)
$ErrorActionPreference = 'Stop'
$entry = Join-Path $PSScriptRoot '../../../scripts/build/shared/nativeLensResources.mjs'
& node $entry
if ($LASTEXITCODE -ne 0) {
	throw 'Native lens resource generation failed; see the error above.'
}
