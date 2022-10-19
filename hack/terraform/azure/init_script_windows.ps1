# Create temporary directory.
$TempDir = "C:\nephe-temp\"
New-Item -Path $TempDir -ItemType Directory
$ScriptDir = "C:\cygwin64\tmp"
$InstallLog = "$TempDir\terraform.log"

function Log($Info) {
    $time = $(get-date -Format g)
    "$time $Info " | Tee-Object $InstallLog -Append | Write-Host
}

Log "Dumping with_agent ${WITH_AGENT}, namespace ${NAMESPACE}, version ${ANTREA_VERSION}"
# Create files from the terraform variables.
$FileContent = '${K8S_CONF}'
Set-Content -Path "$TempDir\antrea-agent.kubeconfig" -Value $FileContent
$FileContent = '${ANTREA_CONF}'
Set-Content -Path "$TempDir\antrea-agent.antrea.kubeconfig" -Value $FileContent
Log "Copying script to $TempDir"
cp "$ScriptDir\install-vm-agent-wrapper.ps1" $TempDir

# Create ssh directory and copy public key
$UserProfile = "C:\cygwin64\home\azureuser\"
$sshPath = "$UserProfile\.ssh"
if (Test-Path "$sshPath") {
    Log "Directory $sshPath exists"
} else {
    New-Item -Path "$sshPath" -ItemType Directory
}

$FileContent = '${SSH_PUBLIC_KEY}'
Set-Content -Path "$sshPath\authorized_keys" -Value $FileContent
$LatestContent = Get-Content "$sshPath\authorized_keys"

$InstallArgs = "-Namespace ${NAMESPACE} -AntreaVersion ${ANTREA_VERSION} -KubeConfigPath $TempDir\antrea-agent.kubeconfig -AntreaKubeConfigPath $TempDir\antrea-agent.antrea.kubeconfig"
$cmd = "$TempDir\install-vm-agent-wrapper.ps1 $InstallArgs"
Invoke-Expression $cmd
