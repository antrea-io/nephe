<#
  .SYNOPSIS
  Installs Antrea-Agent on Public Cloud VMs.

  .PARAMETER Namespace
  ExternalNode Namespace to be used.

  .PARAMETER AntreaVersion
  Antrea version to be used.

  .PARAMETER KubeConfigPath
  Specifies the path of the kubeconfig to access K8s API Server.

  .PARAMETER AntreaKubeConfigPath
  Specifies the path of the kubeconfig to access Antrea API Server.
#>
Param(
    [parameter(Mandatory = $true)] [string] $Namespace,
    [parameter(Mandatory = $true)] [string] $AntreaVersion,
    [parameter(Mandatory = $true)] [string] $KubeConfigPath,
    [parameter(Mandatory = $true)] [string] $AntreaKubeConfigPath
)

$ErrorActionPreference = "Stop"

# System related.
$SystemDrive = [environment]::GetEnvironmentVariable("SystemDrive")
$AntreaTemp =  $SystemDrive + "\antrea-tmp"
$WorkDir = [System.IO.Path]::GetDirectoryName($myInvocation.MyCommand.Definition)
$InstallLog = "$WorkDir\nephe-install.Log"
$OSVersion = ""

# Antrea constants.
$AntreaAgentBin = "antrea-agent.exe"
$AntreaAgentConf = "antrea-agent.conf"
$InstallScript = "install-vm.ps1"

# Cloud specific constants
$AWS = "AWS"
$AZURE = "Azure"
$AWSMetaRoot = "http://169.254.169.254/latest/meta-data"
$AzureMetaRoot = "http://169.254.169.254/metadata"
$Cloud = ""

# Antrea
$AntreaBranch = ""
$AntreaInstallScript = ""
$AntreaConfig = ""
$AgentBin = ""
$Nodename = ""

function ExitOnError() {
    exit 1
}

trap {
    Write-Output "Operation failed. Please look at $InstallLog for more information."
    Write-Output $_
    Log $_
    ExitOnError
}

function Log($Info) {
    $time = $(get-date -Format g)
    "$time $Info " | Tee-Object $InstallLog -Append | Write-Host
}

function RemoveIfExists($path, $force=$true, $recurse=$false) {
    $cmd = "Remove-Item `"$path`""
    if ($force) {
        $cmd = $cmd + " -Force"
    }
    if ($recurse) {
        $cmd = $cmd + " -Recurse"
    }
    if (Test-Path -Path $path) {
        iex $cmd
    }
}

function SetOSVersion() {
    $version = [System.Environment]::OSVersion.Version
    $Script:OSVersion = $version.Major.ToString() + "." + $version.Minor.ToString() + "." +
                            $version.Build.ToString()
}

function InitCloud() {
    $version = (Get-WmiObject -class Win32_ComputerSystemProduct -namespace root\CIMV2).Version
    $vendor = (Get-WmiObject -class Win32_ComputerSystemProduct -namespace root\CIMV2).Vendor
    # On AWS, either version or vendor field is set for an instance size.
    if ($version.ToLower().Contains("amazon") -or ($vendor.ToLower().Contains("amazon"))) {
        $script:Cloud = $AWS
    } elseif ($vendor -like "*Microsoft*Corporation") {
        $script:Cloud = $AZURE
    } else {
        Log "Error unknown cloud platform. Only $AWS and $AZURE clouds are supported"
        ExitOnError
    }
}

function UpdateAntreaURL() {
    # Convert v1.xx.0 to 1.xx
    $tempVersion = $AntreaVersion.Substring(0, $AntreaVersion.Length-2)
    $branchTag = $tempVersion.Substring(1)
    $Script:AntreaBranch = "release-${branchTag}"
    $Script:AntreaInstallScript = "https://github.com/antrea-io/antrea/releases/download/${AntreaVersion}/install-vm.ps1"
    $Script:AgentBin = "https://github.com/antrea-io/antrea/releases/download/${AntreaVersion}/antrea-agent-windows-x86_64.exe"
    $Script:AntreaConfig = "https://raw.githubusercontent.com/antrea-io/antrea/${AntreaBranch}/build/yamls/externalnode/conf/antrea-agent.conf"
}

function GenerateNodename() {
    $name = $null
    $retries = 5
    if ($Cloud -eq $AWS) {
        for ($i = 0; $i -lt $retries; $i++) {
            try {
                $endPoint = "$AWSMetaRoot/instance-id"
                # NodeName is represented as virtualmachine-<instance-id>
                $name = "virtualmachine-" + (Invoke-WebRequest -uri $endPoint).Content
                break
            } catch {
                Log "Retrying query for $endPoint, attempt=$i"
                Start-Sleep -Seconds 5
                continue
            }
        }
    } elseif ($Cloud -eq $AZURE) {
        $vmId = ""
        $vmName = ""
        for ($i=0; $i -lt $retries; $i++) {
            try {
                $endPoint = "$AzureMetaRoot/instance/compute/resourceId?api-version=2021-01-01&format=text"
                $vmId = (Invoke-RestMethod -Headers @{"Metadata"="true"} -Method GET -Uri $endPoint).ToLower()
                $endPoint = "$AzureMetaRoot/instance/compute/name?api-version=2021-01-01&format=text"
                $vmName = (Invoke-RestMethod -Headers @{"Metadata"="true"} -Method GET -Uri $endPoint).ToLower()
                break
            } catch {
                Log "Retrying query for $endPoint, attempt=$i"
                Start-Sleep -Seconds 5
                continue
            }
        }
        # Convert to ASCII
        $vmIdAscii = [int[]][char[]]$vmId
        # Compute sum of ASCII values of vmId
        $sum=0
        foreach ($i in $vmIdAscii) {
            $sum = $sum + $i
        }
        # NodeName is represented as virtualmachine-<vm-name>-<hash of the VM resource ID>
        $name = "virtualmachine-" + $vmName + "-" + $sum
    }
    $Script:Nodename = $name
    if ($null -eq $Nodename) {
        Log "Error Nodename cannot be empty for cloud $Cloud"
        ExitOnError
    }
}

function DownloadFile($src, $dst) {
    Log "Downloading file $src to $dst"
    $retries = 3
    for ($i = 1; $i -le $retries; $i++) {
        try {
            # Redirection option is required to download large files.
            $options = @("-s", "-L")
            curl.exe $options "$src" -o "$dst"
        } catch {
            Log "Failed to download file $src to $dst, retry $i"
            if ($i -ge $retries) {
                Log "Error failed to download file $src after $retries attempts"
                ExitOnError
            }
        }
    }
}

function DownloadAntreaFiles() {
    RemoveIfExists $AntreaTemp -recurse $true
    New-Item $AntreaTemp -type directory -Force | Out-Null
    DownloadFile $AntreaInstallScript $AntreaTemp\$InstallScript
    DownloadFile $AntreaConfig $AntreaTemp\$AntreaAgentConf
    DownloadFile $AgentBin $AntreaTemp\$AntreaAgentBin
}

function Install() {
    SetOSVersion
    Log "Installing antrea-agent on Windows $OSVersion, cloud $Cloud"
    UpdateAntreaURL
    DownloadAntreaFiles
    GenerateNodename
    Log "Set Environment variable NODE_NAME=$NodeName"
    [Environment]::SetEnvironmentVariable("NODE_NAME", $NodeName, [System.EnvironmentVariableTarget]::Machine)
    $InstallArgs = "-NameSpace $NameSpace -BinaryPath $AntreaTemp\$AntreaAgentBin -ConfigPath $AntreaTemp\$AntreaAgentConf -KubeConfigPath $KubeConfigPath -AntreaKubeConfigPath $AntreaKubeConfigPath -NodeName $NodeName"
    Invoke-Expression "& `"$AntreaTemp\$InstallScript`" $InstallArgs"
    RemoveIfExists $AntreaTemp -recurse $true
}

InitCloud
Install
