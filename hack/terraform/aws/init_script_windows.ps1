<powershell>
# Create temporary directory.
$TempDir = "C:\nephe-temp\"
New-Item -Path $TempDir -ItemType Directory
$InstallLog = "$TempDir\terraform.log"

function Log($Info) {
    $time = $(get-date -Format g)
    "$time $Info " | Tee-Object $InstallLog -Append | Write-Host
}

Log "Dumping with_agent ${WITH_AGENT}, namespace ${NAMESPACE}, version ${ANTREA_VERSION}"
# Create files from the terraform variables.
$FileContent = '${INSTALL_VM_AGENT_WRAPPER}'
Set-Content -Path "$TempDir\install-vm-agent-wrapper.ps1" -Value $FileContent
$FileContent = '${K8S_CONF}'
Set-Content -Path "$TempDir\antrea-agent.kubeconfig" -Value $FileContent
$FileContent = '${ANTREA_CONF}'
Set-Content -Path "$TempDir\antrea-agent.antrea.kubeconfig" -Value $FileContent
$FileContent = '${SSH_PUBLIC_KEY}'
$UserProfile = "C:\cygwin64\home\Administrator\"
Set-Content -Path "$UserProfile\.ssh\authorized_keys" -Value $FileContent

# Run install script for antrea-agent installation.
$InstallArgs = "-Namespace ${NAMESPACE} -AntreaVersion ${ANTREA_VERSION} -KubeConfigPath $TempDir\antrea-agent.kubeconfig -AntreaKubeConfigPath $TempDir\antrea-agent.antrea.kubeconfig"
$cmd = "$TempDir\install-vm-agent-wrapper.ps1 $InstallArgs"
Invoke-Expression $cmd

</powershell>
