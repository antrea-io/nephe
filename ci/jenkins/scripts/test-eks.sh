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
        [--aws-access-key-id <AccessKeyID>]                  AWS Access Key ID.
        [--aws-secret-key <SecretKey>]                       AWS Secret Key.
        [--aws-service-user <ServiceUserName>]               AWS Service User Name.
        [--aws-service-user-role-arn <ServiceUserRoleARN>]   AWS Service User Role ARN.
        [--aws-region <Region>]                              AWS region where the setup will be deployed. Defaults to us-east-2.
        [--eks-cluster-role <ClusterRole>]                   IAM Role for Cluster Control Plane.
        [--eks-node-role <NodeRole>]                         IAM Role for EKS worker node.
        [--owner <OwnerName>]                                Setup will be prefixed with owner name.
        [--with-agent]                                       Run test with agented Linux VMs.
        [--with-agent-windows]                               Run test with agented Windows VMs."

function print_usage {
    echoerr "$_usage"
}

function print_help {
    echoerr "Try '$0 --help' for more information."
}

# Defaults
export TF_VAR_owner="ci"
export AWS_DEFAULT_REGION="us-east-2"
export WITH_AGENT=false
export WITH_WINDOWS=false
export TEST_FOCUS=".*test-cloud-cluster.*"

while [[ $# -gt 0 ]]
do
key="$1"

case $key in
    --aws-access-key-id)
    AWS_ACCESS_KEY_ID="$2"
    shift 2
    ;;
    --aws-secret-key)
    AWS_SECRET_ACCESS_KEY="$2"
    shift 2
    ;;
    --aws-service-user-role-arn)
    AWS_SERVICE_USER_ROLE_ARN="$2"
    shift 2
    ;;
    --aws-service-user)
    AWS_SERVICE_USER_NAME="$2"
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
    --with-agent)
    export WITH_AGENT=true
    export TEST_FOCUS=".*test-with-agent*"
    shift 1
    ;;
    --with-agent-windows)
    export WITH_WINDOWS=true
    shift 1
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

if [[ "$AWS_SERVICE_USER_ROLE_ARN" != "" ]] && [[ "$AWS_SERVICE_USER_NAME" != "" ]]; then
    mkdir -p ~/.aws
    cat > ~/.aws/config <<EOF
[default]
role_arn = $AWS_SERVICE_USER_ROLE_ARN
source_profile = $AWS_SERVICE_USER_NAME
region = $AWS_DEFAULT_REGION
output = json
EOF
    cat > ~/.aws/credentials <<EOF
[$AWS_SERVICE_USER_NAME]
aws_access_key_id = $AWS_ACCESS_KEY_ID
aws_secret_access_key = $AWS_SECRET_ACCESS_KEY
EOF
elif [[ "$AWS_SERVICE_USER_ROLE_ARN" = "" ]] && [[ "$AWS_SERVICE_USER_NAME" = "" ]]; then
    export AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID
    export AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
else
    echoerr "invalid input; either specify both aws-service-user-role-arn and aws-service-user or none."
    exit 1
fi

# Set env for NEPHE CI
export NEPHE_CI=true
export NEPHE_CI_AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID
export NEPHE_CI_AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
export NEPHE_CI_AWS_ROLE_ARN=$AWS_SERVICE_USER_ROLE_ARN

echo "Installing AWS CLI"
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip -q awscliv2.zip
sudo ./aws/install

echo "Installing eksctl"
curl --silent --location "https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_$(uname -s)_amd64.tar.gz" | tar xz -C /tmp
chmod +x /tmp/eksctl && sudo mv /tmp/eksctl /usr/local/bin

source $(dirname "${BASH_SOURCE[0]}")/common.sh
install_common_packages

echo "Building Nephe Docker image"
make build

# Create SSH Key Pair
KEY_PAIR="nephe-$$"
aws ec2 import-key-pair --key-name ${KEY_PAIR} --public-key-material fileb://~/.ssh/id_rsa.pub --region ${AWS_DEFAULT_REGION}
export TF_VAR_aws_key_pair_name=${KEY_PAIR}

function cleanup() {
    $HOME/terraform/eks destroy
    aws ec2 delete-key-pair  --key-name ${KEY_PAIR}  --region ${AWS_DEFAULT_REGION}
}
trap cleanup EXIT

hack/install-cloud-tools.sh
echo "Creating EKS Cluster"
$HOME/terraform/eks create

wait_for_cert_manager "$HOME"/tmp/terraform-eks/kubeconfig

echo "Load locally built nephe image"
$HOME/terraform/eks load antrea/nephe:latest

mkdir -p $HOME/logs
ci/bin/integration.test -ginkgo.v -ginkgo.timeout 90m -ginkgo.focus=$TEST_FOCUS -kubeconfig=$HOME/tmp/terraform-eks/kubeconfig -cloud-provider=AWS -support-bundle-dir=$HOME/logs -with-agent=${WITH_AGENT} -with-windows=${WITH_WINDOWS}
