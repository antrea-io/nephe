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

# The script creates and deletes kind testbeds. Kind testbeds may be created with
# docker images preloaded, antrea-cni preloaded, antrea-cni's encapsulation mode,
# and docker bridge network connecting to worker Node.

CLUSTER_NAME=""
ANTREA_IMAGES="projects.registry.vmware.com/antrea/antrea-ubuntu:v1.13.0 "
ANTREA_IMAGES+="antrea/nephe:latest "
OTHER_IMAGES="quay.io/jetstack/cert-manager-controller:v1.8.2 "
OTHER_IMAGES+="quay.io/jetstack/cert-manager-webhook:v1.8.2 "
OTHER_IMAGES+="quay.io/jetstack/cert-manager-cainjector:v1.8.2 "
OTHER_IMAGES+="kennethreitz/httpbin "
OTHER_IMAGES+="byrnedo/alpine-curl"

IMAGES=$ANTREA_IMAGES
IMAGES+=$OTHER_IMAGES
ANTREA_CNI=true
POD_CIDR="10.10.0.0/16"
NUM_WORKERS=2
SUBNETS=""
NODE_IMG="kindest/node:v1.23.4"
IP_FAMILY="ipv4"
SERVICE_CIDR=""
KUBE_PROXY_MODE="iptables"

set -eo pipefail
function echoerr {
    >&2 echo "$@"
}

_usage="
Usage: $0 create CLUSTER_NAME [--pod-cidr POD_CIDR] [--antrea-cni true|false ] [--num-workers NUM_WORKERS] [--images IMAGES] [--subnets SUBNETS]
          destroy CLUSTER_NAME
          load-images CLUSTER_NAME IMAGES
          help
where:
  create: create a kind cluster with name CLUSTER_NAME
  destroy: delete a kind cluster with name CLUSTER_NAME
  load-images: load images to kind cluster with name CLUSTER_NAME
  --pod-cidr: specifies pod cidr used in kind cluster, default is $POD_CIDR
  --antrea-cni: specifies install Antrea CNI in kind cluster, default is true.
  --num-workers: specifies number of worker nodes in kind cluster, default is $NUM_WORKERS
  --images: specifies images loaded to kind cluster, default is $IMAGES
  --subnets: a subnet creates a separate docker bridge network with assigned subnet that worker nodes may connect to. Default is empty all worker
    Node connected to docker0 bridge network
"

function print_usage {
    echoerr "$_usage"
}

function print_help {
    echoerr "Try '$0 help' for more information."
}

function configure_networks {
  echo "Configuring networks"
  networks=$(docker network ls -f name=$CLUSTER_NAME --format '{{.Name}}')
  networks="$(echo $networks)"
  if [[ -z $SUBNETS ]] && [[ -z $networks || $networks == "kind" ]]; then
    echo "Using default docker bridges"
    return
  fi

  # Inject allow all iptables to preempt docker bridge isolation rules
  if [[ ! -z $SUBNETS ]]; then
    set +e
    docker run --net=host --privileged antrea/ethtool:latest iptables -C DOCKER-USER -j ACCEPT > /dev/null 2>&1
    if [[ $? -ne 0 ]]; then
      docker run --net=host --privileged antrea/ethtool:latest iptables -I DOCKER-USER -j ACCEPT
    fi
    set -e
  fi

  # remove old networks
  nodes="$(kind get nodes --name $CLUSTER_NAME | grep worker)"
  node_cnt=$(kind get nodes --name $CLUSTER_NAME | grep worker | wc -l)
  nodes=$(echo $nodes)
  networks+=" bridge"
  echo "removing worker nodes $nodes from networks $networks"
  for n in $networks; do
    rm_nodes=$(docker network inspect $n --format '{{range $i, $conf:=.Containers}}{{$conf.Name}} {{end}}')
    for rn in $rm_nodes; do
      if [[  $nodes =~ $rn ]]; then
        docker network disconnect $n $rn > /dev/null 2>&1
        echo "disconnected worker $rn from network $n"
      fi
    done
    if [[ $n != "bridge" ]]; then
      docker network rm $n > /dev/null 2>&1
      echo "removed network $n"
    fi
  done

  # create new bridge network per subnet
  i=0
  networks=()
  for s in $SUBNETS; do
    network=$CLUSTER_NAME-$i
    echo "creating network $network with $s"
    docker network create -d bridge --subnet $s $network >/dev/null 2>&1
    networks+=($network)
    i=$((i+1))
  done

  num_networks=${#networks[@]}
  if [[ $num_networks -eq 0 ]]; then
    networks+=("bridge")
    num_networks=$((num_networks+1))
  fi

  control_plane_ip=$(docker inspect $CLUSTER_NAME-control-plane --format '{{range $i, $conf:=.NetworkSettings.Networks}}{{$conf.IPAddress}}{{end}}')

  i=0
  for node in $nodes; do
    network=${networks[i]}
    docker network connect $network $node >/dev/null 2>&1
    echo "connected worker $node to network $network"
    node_ip=$(docker inspect $node --format '{{range $i, $conf:=.NetworkSettings.Networks}}{{$conf.IPAddress}}{{end}}')

    # reset network
    docker exec -t $node ip link set eth1 down
    docker exec -t $node ip link set eth1 name eth0
    docker exec -t $node ip link set eth0 up
    gateway=$(echo "${node_ip/%?/1}")
    docker exec -t $node ip route add default via $gateway
    echo "node $node is ready with ip change to $node_ip with gw $gateway"

    # change kubelet config before reset network
    docker exec -t $node sed -i "s/node-ip=.*/node-ip=$node_ip/g" /var/lib/kubelet/kubeadm-flags.env
    docker exec -t $node bash -c "echo '$control_plane_ip $CLUSTER_NAME-control-plane' >> /etc/hosts"

    docker exec -t $node pkill kubelet
    docker exec -t $node pkill kube-proxy || true
    i=$((i+1))
    if [[ $i -ge $num_networks ]]; then
      i=0
    fi
  done

  for node in $nodes; do
    node_ip=$(docker inspect $node --format '{{range $i, $conf:=.NetworkSettings.Networks}}{{$conf.IPAddress}}{{end}}')
    while true; do
      tmp_ip=$(kubectl describe node $node | grep InternalIP)
      if [[ "$tmp_ip" == *"$node_ip"* ]]; then
        break
      fi
      echo "current ip $tmp_ip, wait for new node ip $node_ip"
      sleep 2
    done
  done

  nodes="$(kind get nodes --name $CLUSTER_NAME)"
  nodes="$(echo $nodes)"
  for node in $nodes; do
    # disable tx checksum offload
    # otherwise we observe that inter-Node tunnelled traffic crossing Docker networks is dropped
    # because of an invalid outer checksum.
    docker exec "$node" ethtool -K eth0 tx off
  done
}

function delete_networks {
  networks=$(docker network ls -f name=$CLUSTER_NAME --format '{{.Name}}')
  networks="$(echo $networks)"
  if [[ ! -z $networks ]]; then
    docker network rm $networks > /dev/null 2>&1
    echo "deleted networks $networks"
  fi
}

function load_images {
  if [[ -z $CLUSTER_NAME ]]; then
    echoerr "cluster-name not provided"
    exit 1
  fi
  if [[ -z $IMAGES ]]; then
    echoerr "image names not provided"
    exit 1
  fi
  echo "load images"
  set +e
  for img in $IMAGES; do
    docker image inspect $img > /dev/null 2>&1
    if [[ $? -ne 0 ]]; then
      echoerr "docker image $img not found"
      continue
    fi
    kind load docker-image $img --name $CLUSTER_NAME > /dev/null 2>&1
    if [[ $? -ne 0 ]]; then
      echoerr "docker image $img failed to load"
      continue
    fi
    echo "loaded image $img"
  done
  set -e
}

function create {
  if [[ -z $CLUSTER_NAME ]]; then
    echoerr "cluster-name not provided"
    exit 1
  fi

  set +e
  kind get clusters | grep -w $CLUSTER_NAME > /dev/null 2>&1
  if [[ $? -eq 0 ]]; then
    echoerr "cluster $CLUSTER_NAME already created"
    exit 0
  fi
  set -e

  image=$NODE_IMG
  config_file="/tmp/kind.yml"
  cat <<EOF > $config_file
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
featureGates:
  NetworkPolicyEndPort: true
networking:
  disableDefaultCNI: $ANTREA_CNI
  podSubnet: $POD_CIDR
  serviceSubnet: $SERVICE_CIDR
  ipFamily: $IP_FAMILY
  kubeProxyMode: $KUBE_PROXY_MODE
nodes:
- role: control-plane
  image: $image
EOF
  for i in $(seq 1 $NUM_WORKERS); do
    echo -e "- role: worker" >> $config_file
    echo -e "  image: $image" >> $config_file

  done
  kind create cluster --name $CLUSTER_NAME --config $config_file

  ip=$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $CLUSTER_NAME-control-plane)
  address="https://$ip:6443"
  echo "Set API address to $address"
  kubectl config set-cluster kind-$CLUSTER_NAME --server $address

  # force coredns to run on control-plane node because it
  # is attached to docker0 bridge and uses host dns.
  # Worker Node may be configured to attach to custom bridges
  # which use dockerd as dns, causing coredns to detect
  # dns loop and crash
  patch=$(cat <<EOF
spec:
  template:
    spec:
      nodeSelector:
        kubernetes.io/hostname: $CLUSTER_NAME-control-plane
EOF
)
  kubectl patch deployment coredns -p "$patch" -n kube-system

  configure_networks
  load_images

  if [[ $ANTREA_CNI == true ]]; then
    kubectl apply --context kind-$CLUSTER_NAME -f $(dirname $0)/antrea-kind.yml
  fi

  # wait for cluster info
  while [[ -z $(kubectl cluster-info dump | grep cluster-cidr) ]]; do
    echo "waiting for k8s cluster readying"
    sleep 2
  done

  # Deploy cert-manager
  kubectl create namespace cert-manager
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.2/cert-manager.yaml
}

function destroy {
  if [[ -z $CLUSTER_NAME ]]; then
    echoerr "cluster-name not provided"
    exit 1
  fi
  kind delete cluster --name $CLUSTER_NAME
  delete_networks
}

while [[ $# -gt 0 ]]
 do
 key="$1"

  case $key in
    create)
      CLUSTER_NAME="$2"
      shift 2
      ;;
    destroy)
      CLUSTER_NAME="$2"
      destroy
      exit 0
      ;;
    load-images)
      CLUSTER_NAME="$2"
      IMAGES="${@:3}"
      load_images
      exit 0
      ;;
    --pod-cidr)
      POD_CIDR="$2"
      shift 2
      ;;
    --subnets)
      SUBNETS="$2"
      shift 2
      ;;
    --images)
      IMAGES="$2"
      shift 2
      ;;
    --antrea-cni)
      ANTREA_CNI="$2"
      shift 2
      ;;
    --num-workers)
      NUM_WORKERS="$2"
      shift 2
      ;;
    help)
      print_usage
      exit 0
      ;;
    *)    # unknown option
      echoerr "Unknown option $1"
      print_usage
      exit 1
      ;;
 esac
 done

create
