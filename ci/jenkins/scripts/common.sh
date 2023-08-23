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

KUBECTL_VERSION=v1.24.1
TERRAFORM_VERSION=1.2.2
KIND_VERSION=v0.12.0
CERT_MANAGER_VERSION=v1.8.2
ANTREA_VERSION=v1.12.0
GO_VERSION=1.21.0

function install_common_packages() {
    echo "Installing Kubectl ${KUBECTL_VERSION} version"
    curl -LO https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl
    chmod +x ./kubectl && sudo mv ./kubectl /usr/local/bin/kubectl

    sudo apt-get install -y pv jq unzip

    echo "Installing Terraform ${TERRAFORM_VERSION} version"
    curl -Lo ./terraform.zip https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip
    unzip ./terraform.zip
    chmod +x ./terraform && sudo mv ./terraform /usr/local/bin/terraform
    rm terraform.zip

    echo "Installing Go ${GO_VERSION} version"
    curl -LO https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz
    sudo rm -rf /usr/local/go && sudo tar -zxf go${GO_VERSION}.linux-amd64.tar.gz -C /usr/local/
    rm go${GO_VERSION}.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin

    echo "Installing Helm"
    curl https://baltocdn.com/helm/signing.asc | gpg --dearmor | sudo tee /usr/share/keyrings/helm.gpg > /dev/null
    sudo apt-get install apt-transport-https --yes
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/helm.gpg] https://baltocdn.com/helm/stable/debian/ all main" | sudo tee /etc/apt/sources.list.d/helm-stable-debian.list
    sudo apt-get update
    sudo apt-get install helm
}

function install_kind() {
    echo "Installing Kind ${KIND_VERSION} version"
    curl -Lo ./kind https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-$(uname)-amd64
    chmod +x ./kind
    sudo mv ./kind /usr/local/bin/kind
}

function pull_docker_images() {
    echo "Pulling Docker images to be used in tests"
    docker pull kennethreitz/httpbin
    docker pull byrnedo/alpine-curl
    docker pull quay.io/jetstack/cert-manager-controller:${CERT_MANAGER_VERSION}
    docker pull quay.io/jetstack/cert-manager-webhook:${CERT_MANAGER_VERSION}
    docker pull quay.io/jetstack/cert-manager-cainjector:${CERT_MANAGER_VERSION}
    docker pull projects.registry.vmware.com/antrea/antrea-ubuntu:${ANTREA_VERSION}
}

function wait_for_cert_manager() {
    i=1
    while [ "$(kubectl get pods --kubeconfig=$1 -l=app='cert-manager' -n cert-manager -o jsonpath='{.items[*].status.containerStatuses[0].ready}')" != "true" ]; do
        sleep 5
        echo "Waiting for Cert Manager to be ready."
        i=$(( $i + 1 ))
        if [ $i -eq 20 ]; then
            echo "Cert Manager failed to come up."
            exit 1
        fi
    done
    sleep 5
}
