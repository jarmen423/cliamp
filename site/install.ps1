# Installs the latest cliamp fork release on Windows.
#   irm https://raw.githubusercontent.com/jarmen423/cliamp/main/site/install.ps1 | iex
# Set $env:INSTALL_DIR to pick the directory. Like install.sh, it prefers
# ~\.local\bin when that is already on PATH. Otherwise it installs to
# %LOCALAPPDATA%\Programs\cliamp and adds that to the user PATH.
#
# The body runs in a script block so that, under iex, its variables stay out of
# the caller's session and a failure throws instead of closing the window.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'

    $Repo = 'jarmen423/cliamp'

    # Windows PowerShell 5.1 can default to TLS 1.0, which GitHub refuses.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    # Releases ship amd64 only. Windows on ARM runs it under x64 emulation.
    $Arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $Arch) { $Arch = $env:PROCESSOR_ARCHITECTURE }
    switch ($Arch) {
        'AMD64' { }
        'ARM64' { Write-Host 'Windows on ARM: installing the x64 build, which runs under emulation.' }
        default { throw "Unsupported architecture: $Arch" }
    }

    $PathDirs = @($env:Path -split ';' | Where-Object { $_ } | ForEach-Object { $_.TrimEnd('\') })
    $InstallDir = $env:INSTALL_DIR
    if (-not $InstallDir) {
        $LocalBin = Join-Path $env:USERPROFILE '.local\bin'
        if ($PathDirs -contains $LocalBin) {
            $InstallDir = $LocalBin
        } else {
            $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\cliamp'
        }
    }

    $Asset = 'cliamp-windows-amd64.exe'
    $Base = "https://github.com/$Repo/releases/latest/download"
    $Tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory -Path $Tmp | Out-Null
    try {
        Write-Host "Downloading $Asset..."
        $Exe = Join-Path $Tmp $Asset
        $Checksums = Join-Path $Tmp 'checksums.txt'
        Invoke-WebRequest -UseBasicParsing -Uri "$Base/$Asset" -OutFile $Exe
        Invoke-WebRequest -UseBasicParsing -Uri "$Base/checksums.txt" -OutFile $Checksums

        # A release without a matching checksum is not installable.
        $Expected = $null
        foreach ($Line in Get-Content $Checksums) {
            $Fields = -split $Line
            if ($Fields.Count -ge 2 -and ($Fields[1] -eq $Asset -or $Fields[1] -eq "*$Asset")) {
                $Expected = $Fields[0].ToLower()
                break
            }
        }
        if (-not $Expected) {
            throw "Release has no checksum for $Asset"
        }
        $Actual = (Get-FileHash -Algorithm SHA256 -Path $Exe).Hash.ToLower()
        if ($Actual -ne $Expected) {
            throw "Checksum mismatch`n  expected: $Expected`n  got:      $Actual"
        }
        Write-Host 'Checksum verified.'

        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        $Installed = Join-Path $InstallDir 'cliamp.exe'
        try {
            Copy-Item -Force -Path $Exe -Destination $Installed
        } catch {
            throw "Could not write $Installed. Quit cliamp if it is running (cliamp quit) and run the installer again."
        }
        Write-Host "Installed cliamp to $Installed"
    } finally {
        Remove-Item -Recurse -Force -Path $Tmp -ErrorAction SilentlyContinue
    }

    # Add the directory to the user PATH. Read the raw registry value so that
    # %VAR% references in it are written back unexpanded.
    $Key = Get-Item -Path 'HKCU:\Environment'
    $UserPath = $Key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    $Entries = @($UserPath -split ';' | Where-Object { $_ })
    if ($PathDirs -notcontains $InstallDir.TrimEnd('\') -and $Entries -notcontains $InstallDir) {
        $NewPath = ($Entries + $InstallDir) -join ';'
        Set-ItemProperty -Path 'HKCU:\Environment' -Name 'Path' -Value $NewPath -Type ExpandString
        # Tell Explorer the environment changed, so new terminals see the PATH.
        if (-not ('CliampInstall.Env' -as [type])) {
            Add-Type -Namespace CliampInstall -Name Env -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
        }
        $Result = [UIntPtr]::Zero
        [CliampInstall.Env]::SendMessageTimeout([IntPtr]0xffff, 0x1a, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$Result) | Out-Null
        $env:Path = "$env:Path;$InstallDir"
        Write-Host "Added $InstallDir to your user PATH. Open a new terminal to use it everywhere."
    }

    Write-Host 'Run cliamp to start, or cliamp setup to connect streaming services.'
}
