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

KIND_VERSION=v0.12.0
KUBECTL_VERSION=v1.24.1
TERRAFORM_VERSION=1.2.2

echo "Installing Kind"
curl -Lo ./kind https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-$(uname)-amd64
chmod +x ./kind
sudo mv ./kind /usr/local/bin/kind

echo "Installing Kubectl"
curl -LO https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl
chmod +x ./kubectl && sudo mv ./kubectl /usr/local/bin/kubectl

echo "Installing Terraform"
sudo apt-get install unzip
curl -Lo ./terraform.zip https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip
unzip ./terraform.zip
chmod +x ./terraform && sudo mv ./terraform /usr/local/bin/terraform

echo "Installing Go 1.17"
curl -LO https://golang.org/dl/go1.17.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -zxf go1.17.linux-amd64.tar.gz -C /usr/local/
rm go1.17.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

echo "Installing AWS CLI"
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip -q awscliv2.zip
sudo ./aws/install

echo "Building Nephe Docker image"
make build

echo "Pulling Docker images to be used in tests"
docker pull kennethreitz/httpbin
docker pull byrnedo/alpine-curl
docker pull quay.io/jetstack/cert-manager-controller:v1.8.2
docker pull quay.io/jetstack/cert-manager-webhook:v1.8.2
docker pull quay.io/jetstack/cert-manager-cainjector:v1.8.2
docker pull projects.registry.vmware.com/antrea/antrea-ubuntu:v1.8.0

# Tag locally built nephe image
docker tag antrea/nephe:latest projects.registry.vmware.com/antrea/nephe:latest

echo "Creating kind cluster"
hack/install-cloud-tools.sh
ci/kind/kind-setup.sh create kind

# TODO: Expose these as command line arguments?
export TF_VAR_aws_access_key_id=$1
export TF_VAR_aws_access_key_secret=$2
export TF_VAR_region="us-west-1"

# Export AWS Credentials for AWS CLI
export AWS_ACCESS_KEY_ID=${TF_VAR_aws_access_key_id}
export AWS_SECRET_ACCESS_KEY=${TF_VAR_aws_access_key_secret}
export AWS_DEFAULT_REGION=${TF_VAR_region}

# Create a key pair
KEY_PAIR="nephe-$$"
aws ec2 import-key-pair --key-name ${KEY_PAIR} --public-key-material fileb://~/.ssh/id_rsa.pub --region ${TF_VAR_region}

export TF_VAR_aws_key_pair_name=${KEY_PAIR}
export TF_VAR_owner="nephe-ci"

function cleanup() {
  # Delete key pair
  aws ec2 delete-key-pair  --key-name ${KEY_PAIR}  --region ${TF_VAR_region}
}
trap cleanup EXIT

mkdir $HOME/logs
ci/bin/integration.test -ginkgo.v -ginkgo.focus=".*test-aws.*" -kubeconfig=$HOME/.kube/config -cloud-provider=AWS -support-bundle-dir=$HOME/logs
