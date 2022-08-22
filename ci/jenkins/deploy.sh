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

# This script uses terraform to deploy vm, there are 3 args. The first arg is
# testbed_name(terraform workspace name), the second is ip address or domain name
# for vc, the third arg is password for vc.

set -e

tesbted_name="$1"
vc_ip="$2"
vc_passwd="$3"
var_file="terraform.tfstate.d/${tesbted_name}/vars.tfvars"

if [ -z "${tesbted_name}" -o -z "${vc_ip}" -o -z "${vc_passwd}" ]; then
  echo "Usage: $0 <testbed_name> <vc_ip>"
  exit 1
fi

if [ ! -e ".terraform" ]; then
  terraform init
fi

if [ -e "terraform.tfstate.d/${tesbted_name}" ]; then
  terraform workspace "select" "${tesbted_name}"
else
  terraform workspace new "${tesbted_name}"
fi

cat > "${var_file}" <<EOF
testbed_name="${tesbted_name}"
vsphere_server="${vc_ip}"
EOF

echo ====== Creating VMs ======
if [ ! -e "terraform-${vc_ip}.tfvars" ]; then
    echo "terraform-${vc_ip}.tfvars does not exist, please check your pamameters and config file"
    exit 1
fi
terraform apply -auto-approve -var vsphere_password=${vc_passwd} -var-file=terraform-${vc_ip}.tfvars "-var-file=${var_file}" -parallelism=20
cp -f id_rsa id_rsa.pub "terraform.tfstate.d/${tesbted_name}/"
chmod 600 "terraform.tfstate.d/${tesbted_name}/id_rsa"
echo ====== Pulling Images from Internal Registry ======
ansible-playbook -vvv -i tfstate-inventory.py playbook/pre.yml -e 'ansible_python_interpreter=/usr/bin/python3'
cp -f playbook/jenkins_id_rsa playbook/jenkins_id_rsa.pub "terraform.tfstate.d/${tesbted_name}/"
chmod 600 "terraform.tfstate.d/${tesbted_name}/jenkins_id_rsa"
ansible-playbook -vvv -i tfstate-inventory.py playbook/post.yml -e 'ansible_python_interpreter=/usr/bin/python3'

./show.sh "${tesbted_name}"
./checkin.sh  "${tesbted_name}"
