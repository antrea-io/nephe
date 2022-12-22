#!/usr/bin/env bash

# Copyright 2022 Antrea Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

function echoerr {
    >&2 echo "$@"
}

# Constants.
ANTREA_AGENT_BIN="antrea-agent"
ANTREA_AGENT_CONF="antrea-agent.conf"
INSTALL_SCRIPT="install-vm.sh"

# Cloud specific constants.
AWS="AWS"
AZURE="AZURE"
AWS_METADATA_QUERY_ID="http://169.254.169.254/latest/meta-data/instance-id"
AZURE_METADATA_QUERY_ID="http://169.254.169.254/metadata/instance/compute/resourceId?api-version=2021-02-01&format=text"
AZURE_METADATA_QUERY_NAME="http://169.254.169.254/metadata/instance/compute/name?api-version=2021-02-01&format=text"

# Platform specific
OS=""

function print_usage {
    echoerr "$_usage"
}

function print_help {
    echoerr "Try '$0 --help' for more information"
}

_usage="Usage: $0 [arguments]

Downloads install script from Antrea and setup antrea-agent on AWS or Azure VM.

[arguments]
        --ns <Namespace>                                Namespace to be used by the antrea-agent.
        --kubeconfig <KubeconfigSavePath>               Path of the kubeconfig to access K8s API Server.
        --antrea-kubeconfig <AntreaKubeconfigSavePath>  Path of the kubeconfig to access Antrea API Server.
        --antrea-version <Version>                      Antrea version to be used.
        --help, -h                                      Print this message and exit."

while [[ $# -gt 0 ]]
do
key="$1"

case $key in
    --ns)
    NAMESPACE="$2"
    shift 2
    ;;
    --kubeconfig)
    KUBECONFIG="$2"
    shift 2
    ;;
    --antrea-kubeconfig)
    ANTREA_KUBECONFIG="$2"
    shift 2
    ;;
    --antrea-version)
    ANTREA_VERSION="$2"
    shift 2
    ;;
    -h|--help)
    print_usage
    exit 0
    ;;
    *)    # unknown option
    echoerr "Unknown option $1"
    print_help
    exit 1
    ;;
esac
done

if [ -z "$ANTREA_VERSION" ] || [ -z "$NAMESPACE" ] || [ -z "$KUBECONFIG" ] || [ -z "$ANTREA_KUBECONFIG" ]; then
  echoerr "Required fields are not set"
  print_usage
  exit 1
fi

function get_cloud_platform() {
    cat /sys/class/dmi/id/bios_version | grep -iq "amazon"
    if [ $? -eq 0 ]; then
        echo -n "$AWS"
        return
    fi
    cat /sys/class/dmi/id/bios_vendor | grep -iq "amazon"
    if [ $? -eq 0 ]; then
        echo -n "$AWS"
        return
    fi
    cat /sys/class/dmi/id/sys_vendor | grep -iq "microsoft"
    if [ $? -eq 0 ]; then
        echo -n "$AZURE"
        return
    fi
}

function check_os_prerequisites() {
    CLOUD=$(get_cloud_platform)
    if [ -z "$CLOUD" ]; then
        echoerr "Unknown cloud platform. Only AWS and Azure Clouds are supported"
        exit 2
    fi
    OS=$(grep -Po '^ID=\K.*' /etc/os-release | sed -e 's/^"//' -e 's/"$//')
    echo "Installing antrea-agent on $OS, cloud $CLOUD"
    case "$OS" in
        ubuntu)
          return
        ;;
        rhel)
          return
        ;;
    esac
    echoerr "Unsupported $OS platform"
    exit 2
}

function install_curl() {
    echo "Installing curl in $OS"
    case "$OS" in
        ubuntu)
            cmd="apt-get install curl -y"
            timeout --preserve-status --foreground 1m $cmd
            if [ $? -ne 0 ]; then
                echoerr "$cmd failed with status $ret at $(date)"
                echoerr "Exiting!! please check internet connectivity and re-run install script"
                exit 2
            fi
        ;;
    esac
}

function install_required_packages() {
    if ! command -v curl &> /dev/null; then
        install_curl
    fi
}

function update_antrea_url() {
    ANTREA_BRANCH="release-$(echo $ANTREA_VERSION | cut -b 2-4)"
    # Temporary change to use script from nephe repo.
    # ANTREA_INSTALL_SCRIPT="https://github.com/antrea-io/antrea/releases/download/${ANTREA_VERSION}/install-vm.sh"
    ANTREA_INSTALL_SCRIPT="https://raw.githubusercontent.com/antrea-io/nephe/temp/hack/install-vm.sh"
    AGENT_BIN="https://github.com/antrea-io/antrea/releases/download/${ANTREA_VERSION}/antrea-agent-linux-x86_64"
    ANTREA_CONFIG="https://raw.githubusercontent.com/antrea-io/antrea/${ANTREA_BRANCH}/build/yamls/externalnode/conf/antrea-agent.conf"
}

function download_file() {
    from=$1
    to=$2
    echo "Downloading file $from to $to"
    # Redirection option is required to download large files.
    curl -L --connect-timeout 30 -# --retry 3 "$from" --output "$to"
    if [ $? -ne 0 ]; then
        echoerr "Failed to download file $from"
        exit 2
    fi
}

function download_antrea_files() {
    # Create a temp directory.
    tmp_dir=$( mktemp -d )
    if [ $? -ne 0 ]; then
        echoerr "Failed to create a temporary directory, rc $?"
        exit 2
    fi
    download_file "${ANTREA_INSTALL_SCRIPT}" "${tmp_dir}"/${INSTALL_SCRIPT}
    download_file "${AGENT_BIN}" "${tmp_dir}"/${ANTREA_AGENT_BIN}
    download_file "${ANTREA_CONFIG}" "${tmp_dir}"/${ANTREA_AGENT_CONF}
}

function generate_nodename() {
    if [ "$CLOUD" == $AWS ]; then
        # NodeName is represented as virtualmachine-<instance-id>
        NODENAME="virtualmachine-$(curl ${AWS_METADATA_QUERY_ID})"
    elif [ "$CLOUD" == $AZURE ]; then
        vm_name=$(curl -s -H Metadata:true "${AZURE_METADATA_QUERY_NAME}")
        vm_id=$(curl -sS -H Metadata:true "${AZURE_METADATA_QUERY_ID}")
        # Convert to lowercase
        vm_name=$(echo -n "$vm_name" | tr '[:upper:]' '[:lower:]')
        vm_id=$(echo -n "$vm_id" | tr '[:upper:]' '[:lower:]')
        # Convert to ASCII
        vm_id_ascci=$(echo -n "$vm_id" | od  -An -tu1)
        # Remove leading/trailing spaces
        vm_id_ascci=$(echo "$vm_id_ascci" | sed 's/ *$//g')
        # Compute sum of ASCII values of vm_id
        sum=0
        IFS=" "
        for i in $vm_id_ascci
        do
            sum=$(($sum + $i))
        done
        # NodeName is represented as virtualmachine-<vm-name>-<hash of the VM resource ID>
        NODENAME="virtualmachine-${vm_name}-${sum}"
    fi

    if [ -z "$NODENAME" ]; then
        echoerr "NODENAME cannot be empty for cloud $CLOUD"
        exit 2
    fi
}

function install() {
    echo "Running antrea $INSTALL_SCRIPT script"
    chmod +x "${tmp_dir}"/$INSTALL_SCRIPT
    chmod +x "${tmp_dir}"/$ANTREA_AGENT_BIN
    "${tmp_dir}"/$INSTALL_SCRIPT --ns "$NAMESPACE" --config "${tmp_dir}"/${ANTREA_AGENT_CONF} \
        --kubeconfig "$KUBECONFIG" --antrea-kubeconfig "$ANTREA_KUBECONFIG" \
        --antrea-version "$ANTREA_VERSION" --nodename "$NODENAME" --containerize
    echo "Set antrea-agent Service Environment variable NODE_NAME=$NODENAME"
    # Delete the temporary directory.
    rm -rf "${tmp_dir}"
}

check_os_prerequisites
install_required_packages
update_antrea_url
download_antrea_files
generate_nodename
install
