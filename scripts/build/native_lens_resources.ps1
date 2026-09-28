<#
===========================================================================
native_lens_resources.ps1 - load the lens generator in 32-bit PowerShell

The Node owner supplies private staging paths and publishes completed files.
Windows includes Add-Type; no Visual Studio or DirectX SDK is required.
===========================================================================
#>
param(
	[Parameter(Mandatory=$true)][string]$SourceRoot,
	[Parameter(Mandatory=$true)][string]$OutputRoot
)
$ErrorActionPreference = 'Stop'
if ([IntPtr]::Size -ne 4) {
	throw 'Run this helper through the native lens build owner (32-bit Windows PowerShell required).'
}
if (-not (Test-Path -LiteralPath "$env:WINDIR\SysWOW64\d3dx9_39.dll")) {
	throw 'Missing 32-bit d3dx9_39.dll. Install Microsoft DirectX End-User Runtime: https://www.microsoft.com/en-us/download/details.aspx?id=35 then rerun pnpm assets build.'
}
Add-Type -Path (Join-Path $PSScriptRoot 'NativeLensResources.cs')
[NativeLensResources]::Build($SourceRoot, $OutputRoot)
