param(
    [Parameter(Mandatory = $true)][string]$FilePath,
    [Parameter(Mandatory = $true)][string]$Old,
    [Parameter(Mandatory = $true)][string]$New,
    [Parameter(Mandatory = $true)][string]$TestPackage,
    [Parameter(Mandatory = $false)][string]$Pattern = ""
)

$ErrorActionPreference = "Stop"

# scripts -> skill -> skills -> .opencode -> repository root
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..\..\..")).Path
$tempRoot = Join-Path $env:TEMP ("go-commerce-mutation-" + [guid]::NewGuid().ToString("N"))

Write-Host "Copying repository to $tempRoot"
Copy-Item -Path $repoRoot -Destination $tempRoot -Recurse -Force

try {
    $gitDir = Join-Path $tempRoot ".git"
    if (Test-Path $gitDir) {
        Remove-Item -Path $gitDir -Recurse -Force
    }

    $target = Join-Path $tempRoot $FilePath
    if (-not (Test-Path $target)) {
        throw "Target file not found: $FilePath"
    }

    $content = [IO.File]::ReadAllText($target)
    if (-not $content.Contains($Old)) {
        throw "Mutation source text was not found in $FilePath"
    }

    $mutated = $content.Replace($Old, $New)
    [IO.File]::WriteAllText($target, $mutated, [Text.UTF8Encoding]::new($false))

    Push-Location $tempRoot
    try {
        $arguments = @("test", "-count=1")
        if ($Pattern) {
            $arguments += @("-run", $Pattern)
        }
        $arguments += $TestPackage
        & go @arguments
        $exitCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }

    if ($exitCode -eq 0) {
        Write-Host "LIVED: the mutation was not detected." -ForegroundColor Red
        exit 1
    }

    Write-Host "KILLED: the test suite detected the mutation." -ForegroundColor Green
    exit 0
}
finally {
    if (Test-Path $tempRoot) {
        Remove-Item -Path $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}
