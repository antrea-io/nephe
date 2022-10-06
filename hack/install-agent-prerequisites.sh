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

# source: https://github.com/antrea-io/antrea/blob/main/docs/external-node.md#prerequisites-on-kubernetes-cluster

set -e

export CLUSTER_NAME=$(kubectl config current-context)

case $CLUSTER_NAME in
    *"eks"*)
    export ANTREA_API_SERVER="https://$(kubectl get svc antrea -n kube-system -o jsonpath='{.status.loadBalancer.ingress[].hostname}')"
    ;;
    *"aks"*)
    export ANTREA_API_SERVER=$(kubectl get svc antrea -n kube-system -o jsonpath='{.status.loadBalancer.ingress[].ip}')
    ;;
    *)
    echoerr "Unknown cluster type. Only EKS and AKS are supported"
    exit 1
    ;;
esac

kubectl apply -f https://raw.githubusercontent.com/antrea-io/antrea/v1.8.0/build/yamls/externalnode/vm-agent-rbac.yml

# antrea-agent.kubeconfig
export SERVICE_ACCOUNT="vm-agent"
APISERVER=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$CLUSTER_NAME\")].cluster.server}")
TOKEN=$(kubectl -n vm-ns get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='$SERVICE_ACCOUNT')].data.token}"|base64 --decode)
kubectl config --kubeconfig=antrea-agent.kubeconfig set-cluster $CLUSTER_NAME --server=$APISERVER --insecure-skip-tls-verify=true
kubectl config --kubeconfig=antrea-agent.kubeconfig set-credentials antrea-agent --token=$TOKEN
kubectl config --kubeconfig=antrea-agent.kubeconfig set-context antrea-agent@$CLUSTER_NAME --cluster=$CLUSTER_NAME --user=antrea-agent
kubectl config --kubeconfig=antrea-agent.kubeconfig use-context antrea-agent@$CLUSTER_NAME

# antrea-agent.antrea.kubeconfig
export ANTREA_CLUSTER_NAME="antrea"
TOKEN=$(kubectl -n vm-ns get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='$SERVICE_ACCOUNT')].data.token}"|base64 --decode)
kubectl config --kubeconfig=antrea-agent.antrea.kubeconfig set-cluster $ANTREA_CLUSTER_NAME --server=$ANTREA_API_SERVER --insecure-skip-tls-verify=true
kubectl config --kubeconfig=antrea-agent.antrea.kubeconfig set-credentials antrea-agent --token=$TOKEN
kubectl config --kubeconfig=antrea-agent.antrea.kubeconfig set-context antrea-agent@$ANTREA_CLUSTER_NAME --cluster=$ANTREA_CLUSTER_NAME --user=antrea-agent
kubectl config --kubeconfig=antrea-agent.antrea.kubeconfig use-context antrea-agent@$ANTREA_CLUSTER_NAME

echo
echo "Finish generating agent kubeconfigs. Please run the following commands to create agented VMs using terraform"
echo 'export TF_VAR_agent=true
export TF_VAR_vm_agent_k8s_conf="$(pwd)/antrea-agent.kubeconfig"
export TF_VAR_vm_agent_antrea_conf="$(pwd)/antrea-agent.antrea.kubeconfig"
export TF_VAR_install_wrapper="$(pwd)/hack/install-wrapper.sh"'
