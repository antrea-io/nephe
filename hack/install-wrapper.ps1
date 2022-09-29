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
$AntreaAgentBin="antrea-agent.exe"
$AntreaAgentConf="antrea-agent.conf"
$InstallScript="install-vm.ps1"

# Cloud specific constants
$AWS = "aws"
$AZURE = "azure"
$AWSMetaRoot = "http://169.254.169.254/latest/meta-data"
$AzureMetaRoot = "http://169.254.169.254/metadata"
$Cloud = ""

# Antrea
$AntreaBranch = ""
$AntreaTag = ""
$AntreaInstallScript = ""
$AntreaConfig = ""
$AgentBin = ""
$Nodename = ""

function ExitOnError()
{
    exit 1
}

trap
{
    Write-Output "Operation failed. Please look at $InstallLog for more information."
    Write-Output $_
    Log $_
    ExitOnError
}

function Log($Info)
{
    $time = $(get-date -Format g)
    "$time $Info " | Tee-Object $InstallLog -Append | Write-Host
}

function RemoveIfExists($path, $force=$true, $recurse=$false)
{
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

function SetOSVersion()
{
    $version = [System.Environment]::OSVersion.Version
    $Script:OSVersion = $version.Major.ToString() + "." + $version.Minor.ToString() + "." + $version.Build.ToString()
}

function InitCloud()
{
    $version = (Get-WmiObject -class Win32_ComputerSystemProduct -namespace root\CIMV2).Version
    $vendor = (Get-WmiObject -class Win32_ComputerSystemProduct -namespace root\CIMV2).Vendor
    if (($version -like "*amazon") -or ($vendor -like "amazon*")) {
        $script:Cloud = $AWS
    } elseif ($vendor -like "*Microsoft*Corporation") {
        $script:Cloud = $AZURE
    } else {
        Log "Unknown cloud platform. Only AWS and Azure Clouds are supported"
        ExitOnError
    }
}

function UpdateAntreaURL()
{
    $branchTag = $AntreaVersion.Substring(1,3)
    $Script:AntreaBranch = "release-${branchTag}"
    $Script:AntreaInstallScript = "https://raw.githubusercontent.com/antrea-io/antrea/${AntreaBranch}/hack/externalnode/install-vm.ps1"
    $Script:AntreaConfig = "https://raw.githubusercontent.com/antrea-io/antrea/${AntreaBranch}/build/yamls/externalnode/conf/antrea-agent.conf"
    $Script:AgentBin = "https://github.com/antrea-io/antrea/releases/download/${AntreaVersion}/antrea-agent-windows-x86_64.exe"
}

function GenerateNodename()
{
    $name = $null
    $retries = 5
    if ($Cloud -eq $AWS) {
        for ($i=0; $i -lt $retries; $i++) {
            try {
                $endPoint = "$AWSMetaRoot/instance-id"
                # NodeName is represented as vm-<instance-id>
                $name = "vm-" + (Invoke-WebRequest -uri $endPoint).Content
                break
            } catch {
                Log "Retrying query for $endPoint, attempt=$i"
                Start-Sleep -Seconds 5
                continue
            }
        }
    } elseif ($Cloud -eq $AZURE) {
        $vmId=""
        $vmName=""
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
        # NodeName is represented as vm-<vm-name>-<hash of the VM resource ID>
        $name = "vm-" + $vmName + "-" + $sum
    }
    $Script:Nodename = $name
    if ($Nodename -eq "") {
        Log "ERROR! Nodename cannot be empty for cloud $Cloud"
        ExitOnError
    }
}

function DownloadFile($src, $dst, $options)
{
    Log "Downloading file $src to $dst"
    curl.exe $options "$src" -o "$dst"
}

function DownloadAntreaFiles()
{
    RemoveIfExists $AntreaTemp -recurse $true
    New-Item $AntreaTemp -type directory -Force | Out-Null
    DownloadFile $AntreaInstallScript $AntreaTemp/$InstallScript
    DownloadFile $AntreaConfig $AntreaTemp/$AntreaAgentConf
    DownloadFile $AgentBin $AntreaTemp/$AntreaAgentBin "-L"
}

function Install()
{
    SetOSVersion
    Log "Installing antrea-agent on Windows $OSVersion, cloud $Cloud"
    UpdateAntreaURL
    DownloadAntreaFiles
    GenerateNodename
    $InstallArgs = "-NameSpace $NameSpace -BinaryPath $AntreaTemp/$AntreaAgentBin -ConfigPath $AntreaTemp/$AntreaAgentConf -KubeConfigPath $KubeConfigPath -AntreaKubeConfigPath $AntreaKubeConfigPath -NodeName $NodeName"
    Invoke-Expression "& `"$AntreaTemp\$InstallScript`" $InstallArgs"
    Log "Setting Environment variable NODE_NAME=$NodeName"
    RemoveIfExists $AntreaTemp -recurse $true
}

InitCloud
Install
