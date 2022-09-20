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

_usage="Usage: $0 [arguments]
Setup and run integration tests on EKS cluster with AWS VMs.

[arguments]
        [--aws-access-key-id <AccessKeyID>]  AWS Access Key ID.
        [--aws-secret-key <SecretKey>]       AWS Secret Key.
        [--aws-region <Region>]              The AWS region where the setup will be deployed. Defaults to us-west-2.
        [--eks-cluster-role <ClusterRole>]   IAM Role for Cluster Control Plane.
        [--eks-node-role <NodeRole>]         IAM Role for EKS worker node.
        [--owner <OwnerName>]                Setup will be prefixed with owner name."

function print_usage {
    echoerr "$_usage"
}

function print_help {
    echoerr "Try '$0 --help' for more information."
}

# Defaults
export TF_VAR_owner="ci"
export AWS_DEFAULT_REGION="us-west-2"

while [[ $# -gt 0 ]]
do
key="$1"

case $key in
    --aws-access-key-id)
    export AWS_ACCESS_KEY_ID="$2"
    shift 2
    ;;
    --aws-secret-key)
    export AWS_SECRET_ACCESS_KEY="$2"
    shift 2
    ;;
    --aws-region)
    export AWS_DEFAULT_REGION="$2"
    shift 2
    ;;
    --eks-cluster-role)
    export TF_VAR_eks_cluster_iam_role_name="$2"
    shift 2
    ;;
    --eks-node-role)
    export TF_VAR_eks_iam_instance_profile_name="$2"
    shift 2
    ;;
    --owner)
    export TF_VAR_owner="$2"
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

if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
    echoerr "AWS credentials must be set."
    print_usage
    exit 1
fi

if [ -z "$TF_VAR_eks_cluster_iam_role_name" ] || [ -z "$TF_VAR_eks_iam_instance_profile_name" ]; then
    echoerr "EKS Cluster IAM roles must be set."
    print_usage
    exit 1
fi

echo "Installing AWS CLI"
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip -q awscliv2.zip
sudo ./aws/install

echo "Installing eksctl"
curl --silent --location "https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_$(uname -s)_amd64.tar.gz" | tar xz -C /tmp
chmod +x /tmp/eksctl && sudo mv /tmp/eksctl /usr/local/bin

source $(dirname "${BASH_SOURCE[0]}")/install-common.sh
install_common_packages

echo "Building Nephe Docker image"
make build

# Create SSH Key Pair
KEY_PAIR="nephe-$$"
aws ec2 import-key-pair --key-name ${KEY_PAIR} --public-key-material fileb://~/.ssh/id_rsa.pub --region ${AWS_DEFAULT_REGION}
export TF_VAR_aws_key_pair_name=${KEY_PAIR}

function wait_for_cert_manager() {
    i=1
    while [ "$($HOME/terraform/eks kubectl get pods -l=app='cert-manager' -n cert-manager -o jsonpath='{.items[*].status.containerStatuses[0].ready}')" != "true" ]; do
        sleep 5
        echo "Waiting for Cert Manager to be ready."
        i=$(( $i + 1 ))
        if [ $i -eq 20 ]; then
            echo "Cert Manager failed to come up."
            exit 1
        fi
    done
}

function cleanup() {
    $HOME/terraform/eks destroy
    aws ec2 delete-key-pair  --key-name ${KEY_PAIR}  --region ${AWS_DEFAULT_REGION}
}
trap cleanup EXIT

hack/install-cloud-tools.sh
echo "Creating EKS Cluster"
$HOME/terraform/eks create

wait_for_cert_manager

echo "Tag and Load locally built nephe image"
docker tag antrea/nephe:latest projects.registry.vmware.com/antrea/nephe:latest
$HOME/terraform/eks load projects.registry.vmware.com/antrea/nephe

mkdir -p $HOME/logs
ci/bin/integration.test -ginkgo.v -ginkgo.focus=".*test-cloud-cluster.*" -kubeconfig=$HOME/tmp/terraform-eks/kubeconfig -cloud-provider=AWS -support-bundle-dir=$HOME/logs
