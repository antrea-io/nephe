#!/bin/bash

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

# define the cleanup_testbed function

buildNumber=""
vcHost=""
vcUser=""
dataCenterName=""
dataStore=""
vcCluster=""
resourcePoolPath=""
vcNetwork=""
virtualMachine=""
goVcPassword=""
testType=""

_usage="Usage: $0 [--buildnumber <jenkins BUILD_NUMBER>] [--vchost <VC IPaddress/Domain Name>] [--vcuser <VC username>]
                  [--datacenter <datacenter to deploy vm>] [--datastore <dataStore name>] [--vcCluster <clusterName to deploy vm>]
                  [--resourcePool <resourcePool name>] [--vcNetwork <network used to delpoy vm>] [--virtualMachine <vm template>]
                  [--goVcPassword <Password for VC>] [--testType <type of test to be run>]
Setup a VM to run nephe e2e tests.
        --buildnumber           A number that is used to distinguish vm name from others.
        --vchost                VC ipAddress or domain name to deploy vm.
        --vcuser                User name for VC.
        --goVcPassword          Password to the user name for VC.
        --datacenter            Data center that is used to deploy vm.
        --datastore             Data store that is used to deploy vm.
        --vcCluster             VC cluster that is used to deploy vm.
        --resourcePool          Resource pool that is used to deploy vm.
        --vcNetwork             Network that is used to deploy vm.
        --virtualMachine        VM template that is used to deploy vm.
        --testType              The type of tests that will be triggered."

function echoerr {
    >&2 echo "$@"
}

function print_usage {
    echoerr "$_usage"
}

function print_help {
    echoerr "Try '$0 --help' for more information."
}

while [[ $# -gt 0 ]]
do
key="$1"

case $key in
    --buildnumber)
    buildNumber="$2"
    shift 2
    ;;
    --vchost)
    vcHost="$2"
    shift 2
    ;;
    --vcuser)
    vcUser="$2"
    shift 2
    ;;
    --datacenter)
    dataCenterName="$2"
    shift 2
    ;;
    --datastore)
    dataStore="$2"
    shift 2
    ;;
    --vcCluster)
    vcCluster="$2"
    shift 2
    ;;
    --vcNetwork)
    vcNetwork="$2"
    shift 2
    ;;
    --resourcePool)
    resourcePoolPath="$2"
    shift 2
    ;;
    --virtualMachine)
    virtualMachine="$2"
    shift 2
    ;;
    --goVcPassword)
    goVcPassword="$2"
    shift 2
    ;;
    --testType)
    testType="$2"
    shift 2
    ;;
esac
done

echo "Install necessary packages on jenkins slave"
sudo apt install -y unzip ansible

OLDPWD=`pwd`

echo "Generate ssh keys for jenkins and dynamic vm"
cd ci/jenkins
if [ ! -e id_rsa ]; then
  ssh-keygen -t rsa -P '' -f id_rsa
fi
if [ ! -e playbook/jenkins_id_rsa ];then
  ssh-keygen -t rsa -P '' -f playbook/jenkins_id_rsa
fi

# TODO: path is hardcoded in ansible. Take this as input later
chmod 0600 id_rsa
chmod 0600 playbook/jenkins_id_rsa

echo "Deploy dynamic vm for test"
rm -rf terraform-${vcHost}.tfvars
cat << EOF > terraform-${vcHost}.tfvars
vsphere_user="${vcUser}"
vm_count=1
vsphere_datacenter="${dataCenterName}"
vsphere_datastore="${dataStore}"
vsphere_compute_cluster="${vcCluster}"
vsphere_resource_pool="${resourcePoolPath}"
vsphere_network="${vcNetwork}"
vsphere_virtual_machine="${virtualMachine}"
EOF
cat terraform-${vcHost}.tfvars
testbed_name="nephe-test-${testType}-${buildNumber}"
./deploy.sh ${testbed_name} ${vcHost} ${goVcPassword}

# Fetch dynamic vm ip addr
ip_addr=`cat terraform.tfstate.d/${testbed_name}/terraform.tfstate | jq -r .outputs.vm_ips.value[0]`

#TODO: Scp'ing the code. Need to find better way
scp -r -i id_rsa ${OLDPWD}/* ubuntu@${ip_addr}:~/

function cleanup_testbed() {
  echo "=== retrieve logs ==="
  scp -r -i id_rsa ubuntu@${ip_addr}:~/logs ${OLDPWD}

  echo "=== cleanup vm ==="
  ./destroy.sh "${testbed_name}" "${goVcPassword}"

  cd ${OLDPWD}
  tar zvcf logs.tar.gz logs
}

trap cleanup_testbed EXIT
# TODO: Dont like passing credentials from one machine to another
case $testType in
    aws)
    echo "Run tests on Kind cluster with AWS VMs"
    ssh -i id_rsa ubuntu@${ip_addr} "~/ci/jenkins/test-aws.sh ${AWS_ACCESS_KEY_ID} ${AWS_ACCESS_KEY_SECRET}"
    ;;
    azure)
    echo "Run tests on Kind cluster with Azure VMs"
    ssh -i id_rsa ubuntu@${ip_addr} "~/ci/jenkins/test-azure.sh ${AZURE_SUBSCRIPTION_ID} ${AZURE_APP_ID} ${AZURE_TENANT_ID} ${AZURE_PASSWORD}"
    ;;
    eks)
    echo "Run tests on EKS cluster with AWS VMs"
    ssh -i id_rsa ubuntu@${ip_addr} "~/ci/jenkins/test-eks.sh ${AWS_ACCESS_KEY_ID} ${AWS_ACCESS_KEY_SECRET} ${EKS_IAM_ROLE} ${EKS_IAM_INSTANCE_PROFILE}"
    ;;
    aks)
    echo "Run tests on AKS cluster with Azure VMs"
    ssh -i id_rsa ubuntu@${ip_addr} "~/ci/jenkins/test-aks.sh ${AZURE_SUBSCRIPTION_ID} ${AZURE_APP_ID} ${AZURE_TENANT_ID} ${AZURE_PASSWORD}"
    ;;
esac
