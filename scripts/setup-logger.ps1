<#
.SYNOPSIS
    The script configures and starts a Gobbler instance (logger Gobbler) to be used as 
    a logger for instrumented Gobbler ingestion instances. (1) start the logger Gobbler from 
    one terminal first : ./gobbler.exe --port 8081, then (2) run this script from another terminal. 

    The script registers the four gobbler self-logging item type definitions, configures logger Gobbler
    with file-mode output, and starts its pipeline.

.PARAMETER LoggerUrl
    Base URL of the logger Gobbler instance. Default: http://localhost:8081

.PARAMETER OutputDir
    Directory where the logger Gobbler writes CSV files.

.PARAMETER BatchSize
    Writer batch size (rows per flush). Default: 50

.PARAMETER QueueSize
    Per-type writer queue depth. Default: 100

.EXAMPLE
    .\setup-logger.ps1 -OutputDir C:\gobbler-logs

.EXAMPLE
    .\setup-logger.ps1 -LoggerUrl http://localhost:9000 -OutputDir /tmp/gobbler-logs -BatchSize 100
#>
[CmdletBinding()]
param(
    [string] $LoggerUrl   = 'http://localhost:8081',
    [string] $OutputDir   = $(throw '-OutputDir is required'),
    [int]    $BatchSize   = 50,
    [int]    $QueueSize   = 100
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ── helpers ──────────────────────────────────────────────────────────────────

function Invoke-Gobbler {
    param(
        [string] $Method,
        [string] $Path,
        [object] $Body = $null
    )

    $uri = "$LoggerUrl/gobbler/$Path"
    $params = @{ Method = $Method; Uri = $uri; ContentType = 'application/json' }
    if ($Body) {
        $params['Body'] = ($Body | ConvertTo-Json -Depth 10 -Compress)
    }

    try {
        $response = Invoke-RestMethod @params
        return $response
    } catch [System.Net.WebException] {
        $reader   = [System.IO.StreamReader]::new($_.Exception.Response.GetResponseStream())
        $bodyText = $reader.ReadToEnd()
        $reader.Dispose()
        throw "HTTP $($_.Exception.Response.StatusCode) from $Method $uri : $bodyText"
    }
}

function Add-Definition {
    param([hashtable] $Def)
    Write-Host "  Registering '$($Def.name)' ..."
    $result = Invoke-Gobbler -Method POST -Path 'definition/add' -Body $Def
    if ($result.status -ne 'ok') {
        throw "Unexpected response: $($result | ConvertTo-Json)"
    }
}

# ── 1. Verify the server is reachable ────────────────────────────────────────

Write-Host "Checking logger Gobbler at $LoggerUrl ..."
try {
    Invoke-RestMethod -Uri "$LoggerUrl/gobbler/" -Method GET | Out-Null
} catch {
    throw "Cannot reach logger Gobbler at $LoggerUrl. Is it running? Error: $_"
}

# ── 2. Register item type definitions ────────────────────────────────────────

Write-Host 'Registering item type definitions ...'

Add-Definition @{
    name          = 'gobbler-ingest-event'
    documentation = 'One record per POST /gobbler/ingest request on the instrumented Gobbler instance.'
    folder        = 'gobbler-ingest'
    latencyMinutes = 5
    orderedColumns = @(
        @{ name = 'requestId';  type = 'string'; optional = $true  }
        @{ name = 'itemsIn';    type = 'int';    optional = $false }
        @{ name = 'ingested';   type = 'int';    optional = $false }
        @{ name = 'rejected';   type = 'int';    optional = $false }
        @{ name = 'statusCode'; type = 'int';    optional = $false }
        @{ name = 'durationMs'; type = 'int';    optional = $false }
    )
}

Add-Definition @{
    name          = 'gobbler-writer-flush'
    documentation = 'One record per successful writer flush (FileWriter or BlobWriter).'
    folder        = 'gobbler-writer'
    latencyMinutes = 5
    orderedColumns = @(
        @{ name = 'itemType';     type = 'string'; optional = $false }
        @{ name = 'output';       type = 'string'; optional = $true  }
        @{ name = 'itemsFlushed'; type = 'int';    optional = $false }
    )
}

Add-Definition @{
    name          = 'gobbler-writer-error'
    documentation = 'One record per writer error (flush, rotate, or open failure).'
    folder        = 'gobbler-writer'
    latencyMinutes = 5
    orderedColumns = @(
        @{ name = 'itemType';  type = 'string'; optional = $false }
        @{ name = 'operation'; type = 'string'; optional = $false }
        @{ name = 'errorMsg';  type = 'string'; optional = $false }
    )
}

Add-Definition @{
    name          = 'gobbler-pipeline-event'
    documentation = 'One record per pipeline lifecycle event (start, stop, rotate).'
    folder        = 'gobbler-pipeline'
    latencyMinutes = 5
    orderedColumns = @(
        @{ name = 'event';    type = 'string'; optional = $false }
        @{ name = 'itemType'; type = 'string'; optional = $true  }
    )
}

# ── 3. Configure pipeline ─────────────────────────────────────────────────────

Write-Host "Configuring pipeline (mode=file, outputDir=$OutputDir) ..."
$configResult = Invoke-Gobbler -Method POST -Path 'pipeline/configure' -Body @{
    mode            = 'file'
    outputDir       = $OutputDir
    writerBatchSize = $BatchSize
    writerQueueSize = $QueueSize
}
if ($configResult.status -ne 'ok') {
    throw "Configure failed: $($configResult | ConvertTo-Json)"
}

# ── 4. Start pipeline ─────────────────────────────────────────────────────────

Write-Host 'Starting pipeline ...'
$startResult = Invoke-Gobbler -Method POST -Path 'pipeline/start'
if ($startResult.status -ne 'ok') {
    throw "Start failed: $($startResult | ConvertTo-Json)"
}

# ── 5. Verify running ─────────────────────────────────────────────────────────

$status = Invoke-Gobbler -Method GET -Path 'pipeline/status'
if (-not $status.running) {
    throw "Pipeline is not running after start. Status: $($status | ConvertTo-Json)"
}

Write-Host ''
Write-Host "Logger Gobbler is running."
Write-Host "  Definitions: $($status.registeredDefinitions)"
Write-Host "  Output:      $OutputDir"
Write-Host "  URL:         $LoggerUrl"
