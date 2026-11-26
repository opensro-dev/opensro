<#
===========================================================================

processTreeJob.ps1 - hold a process tree inside one bounded Windows job

A process that raises its own priority escapes the inherited class that
processPriority.mjs relies on: Chrome launches its GPU process at
ABOVE_NORMAL (GpuProcessHighPriorityWin), so a probe started at idle
still put a software-WebGPU renderer above the desktop on every core
(2026-11-26). A job object is the operating system's own bound: with
JOB_OBJECT_LIMIT_PRIORITY_CLASS no member can raise its class, the CPU
rate hard cap holds the whole tree to a share of the machine, and every
process a member starts joins the job by itself.

Called by processPriority.mjs boundChildren; prints one JSON line.
The job is named per caller, so each launch from one caller reuses it,
and it lives exactly as long as its members (no kill-on-close).

===========================================================================
#>
param(
	[Parameter( Mandatory = $true )][int]$ParentPid,
	[Parameter( Mandatory = $true )][string]$ImageName,
	[ValidateSet( "Idle", "BelowNormal", "None" )][string]$PriorityClass = "BelowNormal",
	[ValidateRange( 0, 100 )][int]$CpuPercent = 50
)
$ErrorActionPreference = "Stop"

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public static class SroProcessTreeJob {
	const int JobObjectBasicLimitInformation = 2;
	const int JobObjectCpuRateControlInformation = 15;
	const uint JOB_OBJECT_LIMIT_PRIORITY_CLASS = 0x20;
	const uint JOB_OBJECT_CPU_RATE_CONTROL_ENABLE = 0x1;
	const uint JOB_OBJECT_CPU_RATE_CONTROL_HARD_CAP = 0x4;
	const uint PROCESS_SET_QUOTA = 0x0100;
	const uint PROCESS_TERMINATE = 0x0001;

	[StructLayout( LayoutKind.Sequential )]
	struct BasicLimit {
		public long PerProcessUserTimeLimit;
		public long PerJobUserTimeLimit;
		public uint LimitFlags;
		public UIntPtr MinimumWorkingSetSize;
		public UIntPtr MaximumWorkingSetSize;
		public uint ActiveProcessLimit;
		public UIntPtr Affinity;
		public uint PriorityClass;
		public uint SchedulingClass;
	}

	[StructLayout( LayoutKind.Sequential )]
	struct CpuRateLimit {
		public uint ControlFlags;
		public uint CpuRate;
	}

	[DllImport( "kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true )]
	static extern IntPtr CreateJobObjectW( IntPtr attributes, string name );
	[DllImport( "kernel32.dll", SetLastError = true )]
	static extern bool SetInformationJobObject( IntPtr job, int infoClass, ref BasicLimit info, int length );
	[DllImport( "kernel32.dll", SetLastError = true )]
	static extern bool SetInformationJobObject( IntPtr job, int infoClass, ref CpuRateLimit info, int length );
	[DllImport( "kernel32.dll", SetLastError = true )]
	static extern bool QueryInformationJobObject( IntPtr job, int infoClass, ref BasicLimit info, int length, IntPtr returned );
	[DllImport( "kernel32.dll", SetLastError = true )]
	static extern bool QueryInformationJobObject( IntPtr job, int infoClass, ref CpuRateLimit info, int length, IntPtr returned );
	[DllImport( "kernel32.dll", SetLastError = true )]
	static extern IntPtr OpenProcess( uint access, bool inherit, int pid );
	[DllImport( "kernel32.dll", SetLastError = true )]
	static extern bool AssignProcessToJobObject( IntPtr job, IntPtr process );
	[DllImport( "kernel32.dll", SetLastError = true )]
	static extern bool CloseHandle( IntPtr handle );

	public static IntPtr Open( string name, uint priorityClass, int cpuPercent ) {
		IntPtr job = CreateJobObjectW( IntPtr.Zero, name );
		if ( job == IntPtr.Zero ) throw new System.ComponentModel.Win32Exception();
		if ( priorityClass != 0 ) {
			BasicLimit basic = new BasicLimit();
			basic.LimitFlags = JOB_OBJECT_LIMIT_PRIORITY_CLASS;
			basic.PriorityClass = priorityClass;
			if ( !SetInformationJobObject( job, JobObjectBasicLimitInformation, ref basic, Marshal.SizeOf( basic ) ) ) {
				throw new System.ComponentModel.Win32Exception();
			}
		}
		if ( cpuPercent > 0 && cpuPercent < 100 ) {
			// CpuRate is in 1/100 of a percent of the whole machine.
			CpuRateLimit rate = new CpuRateLimit();
			rate.ControlFlags = JOB_OBJECT_CPU_RATE_CONTROL_ENABLE | JOB_OBJECT_CPU_RATE_CONTROL_HARD_CAP;
			rate.CpuRate = (uint)( cpuPercent * 100 );
			if ( !SetInformationJobObject( job, JobObjectCpuRateControlInformation, ref rate, Marshal.SizeOf( rate ) ) ) {
				throw new System.ComponentModel.Win32Exception();
			}
		}
		return job;
	}

	// The limits in force, read back from the job: the report says what
	// holds, not what was asked for.
	public static uint PriorityLimit( IntPtr job ) {
		BasicLimit basic = new BasicLimit();
		if ( !QueryInformationJobObject( job, JobObjectBasicLimitInformation, ref basic, Marshal.SizeOf( basic ), IntPtr.Zero ) ) return 0;
		return ( basic.LimitFlags & JOB_OBJECT_LIMIT_PRIORITY_CLASS ) != 0 ? basic.PriorityClass : 0;
	}

	public static uint CpuCapPercent( IntPtr job ) {
		CpuRateLimit rate = new CpuRateLimit();
		if ( !QueryInformationJobObject( job, JobObjectCpuRateControlInformation, ref rate, Marshal.SizeOf( rate ), IntPtr.Zero ) ) return 0;
		return ( rate.ControlFlags & JOB_OBJECT_CPU_RATE_CONTROL_ENABLE ) != 0 ? rate.CpuRate / 100 : 0;
	}

	public static bool Assign( IntPtr job, int pid ) {
		IntPtr process = OpenProcess( PROCESS_SET_QUOTA | PROCESS_TERMINATE, false, pid );
		if ( process == IntPtr.Zero ) return false;
		try {
			return AssignProcessToJobObject( job, process );
		} finally {
			CloseHandle( process );
		}
	}
}
"@

# Win32 priority class values for the job limit; 0 leaves priority alone.
$PRIORITY_CLASSES = @{ "Idle" = 0x40; "BelowNormal" = 0x4000; "None" = 0 }
# Two sweeps: a member started between the first sweep and its parent's
# assignment is caught by the second; after that every new process is
# started by a member and joins the job itself.
$SWEEPS = 2

$job = [SroProcessTreeJob]::Open( "Local\sro-process-tree-$ParentPid", $PRIORITY_CLASSES[$PriorityClass], $CpuPercent )
$assigned = @{}
for ( $sweep = 0; $sweep -lt $SWEEPS; $sweep++ ) {
	$table = Get-CimInstance Win32_Process -Property ProcessId, ParentProcessId, Name
	$roots = @( $table | Where-Object { $_.ParentProcessId -eq $ParentPid -and $_.Name -ieq $ImageName } | ForEach-Object { $_.ProcessId } )
	$queue = New-Object System.Collections.Generic.Queue[int]
	foreach ( $root in $roots ) { $queue.Enqueue( $root ) }
	while ( $queue.Count -gt 0 ) {
		$id = $queue.Dequeue()
		if ( -not $assigned.ContainsKey( $id ) ) {
			$assigned[$id] = [SroProcessTreeJob]::Assign( $job, $id )
		}
		foreach ( $child in ( $table | Where-Object { $_.ParentProcessId -eq $id } ) ) { $queue.Enqueue( $child.ProcessId ) }
	}
}
$joined = @( $assigned.Keys | Where-Object { $assigned[$_] } ).Count
$report = @{
	job = "sro-process-tree-$ParentPid"
	joined = $joined
	refused = $assigned.Count - $joined
	priorityClass = [int][SroProcessTreeJob]::PriorityLimit( $job )
	cpuPercent = [int][SroProcessTreeJob]::CpuCapPercent( $job )
}
Write-Output ( $report | ConvertTo-Json -Compress )
