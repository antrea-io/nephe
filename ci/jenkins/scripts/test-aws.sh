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
Setup and run integration tests on Kind cluster with AWS VMs.
[arguments]
        [--aws-access-key-id <AccessKeyID>]                  AWS Access Key ID.
        [--aws-secret-key <SecretKey>]                       AWS Secret Key.
        [--aws-service-user <ServiceUserName>]               AWS Service User Name.
        [--aws-service-user-role-arn <ServiceUserRoleARN>]   AWS Service User Role ARN.
        [--aws-region <Region>]                              AWS region where the setup will be deployed. Defaults to us-east-2.
        [--owner <OwnerName>]                                Setup will be prefixed with owner name.
        [--upgrade]                                          Run upgrade test."

function print_usage {
    echoerr "$_usage"
}

function print_help {
    echoerr "Try '$0 --help' for more information."
}

# Defaults
export TF_VAR_owner="ci"
export AWS_DEFAULT_REGION="us-east-2"
export UPGRADE=false 

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
    --owner)
    export TF_VAR_owner="$2"
    shift 2
    ;;
    --upgrade)
    export UPGRADE=true
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

if [[ "$AWS_SERVICE_USER_ROLE_ARN" != "" ]] && [[ "$AWS_SERVICE_USER_NAME" != "" ]]; then
    mkdir -p ~/.aws
    cat > ~/.aws/config <<EOF
[default]
region = $AWS_DEFAULT_REGION
role_arn = $AWS_SERVICE_USER_ROLE_ARN
source_profile = $AWS_SERVICE_USER_NAME
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

source $(dirname "${BASH_SOURCE[0]}")/install-common.sh
install_common_packages

echo "Building Nephe Docker image"
make build

install_kind
pull_docker_images

echo "Creating Kind cluster"
hack/install-cloud-tools.sh
ci/kind/kind-setup.sh create kind

# Create a key pair
KEY_PAIR="nephe-$$"
aws ec2 import-key-pair --key-name ${KEY_PAIR} --public-key-material fileb://~/.ssh/id_rsa.pub --region "${AWS_DEFAULT_REGION}"

export TF_VAR_aws_key_pair_name=${KEY_PAIR}

function cleanup() {
    # Delete key pair
    aws ec2 delete-key-pair  --key-name ${KEY_PAIR}  --region "${AWS_DEFAULT_REGION}"
}
trap cleanup EXIT

mkdir -p "$HOME"/logs

if [ "$UPGRADE" = true ] ; then
    ci/bin/upgrade.test -ginkgo.v -ginkgo.timeout 90m -ginkgo.focus=".*test-aws.*" -kubeconfig="$HOME"/.kube/config \
    -from-version=0.5.0 -to-version="latest" -chart-dir="build/charts/nephe" -cloud-provider=AWS -support-bundle-dir="$HOME"/logs
else
    ci/bin/integration.test -ginkgo.v -ginkgo.timeout 90m -ginkgo.focus=".*test-aws.*" -kubeconfig="$HOME"/.kube/config \
    -cloud-provider=AWS -support-bundle-dir="$HOME"/logs
fi
