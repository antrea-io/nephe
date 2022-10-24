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

# This script helps configure Nephe cluster and generates necessary kubeconfigs for importing agented VMs.
# command reference: https://github.com/antrea-io/antrea/blob/main/docs/external-node.md#prerequisites-on-kubernetes-cluster.

set -e

# Constants.
# Namespace is constant due to upstream limitation. The RBAC yaml provided by Antrea has hardcoded Namespace vm-ns.
NAMESPACE="vm-ns"
EKS="eks"
AKS="aks"
CLUSTER_NAME=$(kubectl config current-context)
ANTREA_CLUSTER_NAME="antrea"

# Defaults.
K8S_KUBECONFIG="antrea-agent.kubeconfig"
ANTREA_KUBECONFIG="antrea-agent.antrea.kubeconfig"
SERVICE_ACCOUNT="vm-agent"

function echoerr {
    >&2 echo "$@"
}

function print_usage {
    echoerr "$_usage"
}

function print_help {
    echoerr "Try '$0 --help' for more information."
}

_usage="Usage: $0 [arguments]

Configure Nephe cluster and generates necessary kubeconfigs for importing agented VMs.

[arguments]
        --cluster-type <Type>                           Type of the Nephe cluster.
        --antrea-version <Version>                      Antrea version to be used.
        --kubeconfig <KubeconfigSavePath>               Path to store the generated K8s API Server kubeconfig.
        --antrea-kubeconfig <AntreaKubeconfigSavePath>  Path to store the generated Antrea API Server kubeconfig.
        --service-account <ServiceAccount>              Service account to be used by the antrea-agent.
        --help, -h                                      Print this message and exit."

while [[ $# -gt 0 ]]
do
key="$1"

case $key in
    --cluster-type)
    CLUSTER_TYPE="$2"
    shift 2
    ;;
    --antrea-version)
    ANTREA_VERSION="$2"
    shift 2
    ;;
    --kubeconfig)
    K8S_KUBECONFIG="$2"
    shift 2
    ;;
    --antrea-kubeconfig)
    ANTREA_KUBECONFIG="$2"
    shift 2
    ;;
    --service-account)
    SERVICE_ACCOUNT="$2"
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

if [ -z "$CLUSTER_TYPE" ] || [ -z "$ANTREA_VERSION" ]; then
  echoerr "Required fields are not set."
  print_usage
  exit 1
fi

function get_antrea_api_server() {
  # Cluster Type to lower case.
  case $(echo "$CLUSTER_TYPE" | tr '[:upper:]' '[:lower:]') in
      $EKS)
      echo "https://$(kubectl get svc antrea -n kube-system -o jsonpath='{.status.loadBalancer.ingress[].hostname}')"
      ;;
      $AKS)
      echo "https://$(kubectl get svc antrea -n kube-system -o jsonpath='{.status.loadBalancer.ingress[].ip}')"
      ;;
      *)
      echoerr "Unknown cluster type. Only EKS and AKS are supported."
      exit 1
      ;;
  esac
}

function set_agent_rbac() {
  kubectl apply -f https://raw.githubusercontent.com/antrea-io/antrea/$ANTREA_VERSION/build/yamls/externalnode/vm-agent-rbac.yml
}

function generate_k8s_kubeconfig() {
  API_SERVER=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$CLUSTER_NAME\")].cluster.server}")
  TOKEN=$(kubectl -n $NAMESPACE get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='$SERVICE_ACCOUNT')].data.token}" | base64 --decode)
  kubectl config --kubeconfig=$K8S_KUBECONFIG set-cluster $CLUSTER_NAME --server=$API_SERVER --insecure-skip-tls-verify=true
  kubectl config --kubeconfig=$K8S_KUBECONFIG set-credentials antrea-agent --token=$TOKEN
  kubectl config --kubeconfig=$K8S_KUBECONFIG set-context antrea-agent@$CLUSTER_NAME --cluster=$CLUSTER_NAME --user=antrea-agent
  kubectl config --kubeconfig=$K8S_KUBECONFIG use-context antrea-agent@$CLUSTER_NAME
}

function generate_antrea_kubeconfig() {
  ANTREA_API_SERVER=$(get_antrea_api_server)
  TOKEN=$(kubectl -n $NAMESPACE get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='$SERVICE_ACCOUNT')].data.token}" | base64 --decode)
  kubectl config --kubeconfig=$ANTREA_KUBECONFIG set-cluster $ANTREA_CLUSTER_NAME --server=$ANTREA_API_SERVER --insecure-skip-tls-verify=true
  kubectl config --kubeconfig=$ANTREA_KUBECONFIG set-credentials antrea-agent --token=$TOKEN
  kubectl config --kubeconfig=$ANTREA_KUBECONFIG set-context antrea-agent@$ANTREA_CLUSTER_NAME --cluster=$ANTREA_CLUSTER_NAME --user=antrea-agent
  kubectl config --kubeconfig=$ANTREA_KUBECONFIG use-context antrea-agent@$ANTREA_CLUSTER_NAME
}

set_agent_rbac
generate_k8s_kubeconfig
generate_antrea_kubeconfig
