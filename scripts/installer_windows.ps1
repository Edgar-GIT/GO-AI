$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$installer = Join-Path $scriptDir "installer_gui.py"

if (!(Test-Path $installer)) {
    Write-Error "Missing installer payload: $installer"
    exit 1
}

$commands = @(
    @{ Name = "pyw.exe"; Args = @("-3", $installer) },
    @{ Name = "py.exe"; Args = @("-3", $installer) },
    @{ Name = "pythonw.exe"; Args = @($installer) },
    @{ Name = "python.exe"; Args = @($installer) }
)

foreach ($command in $commands) {
    $resolved = Get-Command $command.Name -ErrorAction SilentlyContinue
    if ($resolved) {
        & $resolved.Source @($command.Args)
        exit $LASTEXITCODE
    }
}

Write-Error "Python 3 is required to run the Gopher AI installer."
exit 1
