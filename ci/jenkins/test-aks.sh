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
KUBECTL_VERSION=v1.24.1
TERRAFORM_VERSION=1.2.2

echo "Installing Kubectl"
curl -LO https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl
chmod +x ./kubectl && sudo mv ./kubectl /usr/local/bin/kubectl

echo "Installing Terraform"
sudo apt-get install unzip
curl -Lo ./terraform.zip https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip
unzip ./terraform.zip
chmod +x ./terraform && sudo mv ./terraform /usr/local/bin/terraform

sudo apt-get install -y pv bzip2 jq

echo "Installing Go 1.17"
curl -LO https://golang.org/dl/go1.17.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -zxf go1.17.linux-amd64.tar.gz -C /usr/local/
rm go1.17.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

echo "Installing Azure CLI"
curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash

echo "Building Nephe Docker image"
make build

export TF_VAR_azure_client_subscription_id=$1
export TF_VAR_azure_client_id=$2
export TF_VAR_azure_client_tenant_id=$3
export TF_VAR_azure_client_secret=$4
export TF_VAR_owner="nephe-ci"

az login --service-principal -u $TF_VAR_azure_client_id -p $TF_VAR_azure_client_secret --tenant $TF_VAR_azure_client_tenant_id

# Set AKS exports too
export TF_VAR_aks_client_subscription_id=${TF_VAR_azure_client_subscription_id}
export TF_VAR_aks_client_id=${TF_VAR_azure_client_id}
export TF_VAR_aks_client_tenant_id=${TF_VAR_azure_client_tenant_id}
export TF_VAR_aks_client_secret=${TF_VAR_azure_client_secret}

function cleanup() {
  echo "Destroying AKS Cluster"
  $HOME/terraform/aks destroy
}

trap cleanup EXIT
hack/install-cloud-tools.sh
echo "Creating AKS Cluster"
$HOME/terraform/aks create

sleep 60
timeout 10 $HOME/terraform/aks kubectl get node -o wide
docker tag antrea/nephe:latest projects.registry.vmware.com/antrea/nephe:latest
$HOME/terraform/aks load projects.registry.vmware.com/antrea/nephe

mkdir -p $HOME/logs
ci/bin/integration.test -ginkgo.v -ginkgo.focus=".*test-cloud-cluster.*" -kubeconfig=$HOME/tmp/terraform-aks/kubeconfig -cloud-provider=Azure -support-bundle-dir=$HOME/logs
