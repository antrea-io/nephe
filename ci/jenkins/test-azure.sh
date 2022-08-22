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

echo "Building Nephe Docker image"
make build

echo "Pulling Docker images to be used in tests"
docker pull kennethreitz/httpbin
docker pull byrnedo/alpine-curl
docker pull quay.io/jetstack/cert-manager-controller:v1.8.2
docker pull quay.io/jetstack/cert-manager-webhook:v1.8.2
docker pull quay.io/jetstack/cert-manager-cainjector:v1.8.2
docker pull projects.registry.vmware.com/antrea/antrea-ubuntu:v1.8.0

docker tag antrea/nephe:latest projects.registry.vmware.com/antrea/nephe:latest

echo "Creating kind cluster"
hack/install-cloud-tools.sh
ci/kind/kind-setup.sh create kind

export TF_VAR_azure_client_subscription_id=$1
export TF_VAR_azure_client_id=$2
export TF_VAR_azure_client_tenant_id=$3
export TF_VAR_azure_client_secret=$4
export TF_VAR_owner="nephe-ci"

mkdir -p $HOME/logs
ci/bin/integration.test -ginkgo.v -ginkgo.focus=".*test-azure.*" -kubeconfig=$HOME/.kube/config -cloud-provider=Azure -support-bundle-dir=$HOME/logs
