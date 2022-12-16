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

# This script destroys vms when test are finished. There two args for this
# script. The first arg is testbed_name (terraform workspace name). The 
# second is password for vc.
set -ex

testbed_name="$1"
vc_passwd="$2"
terraform_dir="$3"
vsphere_server_1="$4"
if [ ! -z "${vsphere_server_1}" ]; then
  vsphere_server=${vsphere_server_1}
fi

if [ -z "${testbed_name}" ]; then
  echo "Usage: $0 <testbed_name>"
  exit 1
fi

if [ -z ${terraform_dir} ]; then
  var_file="terraform.tfstate.d/${testbed_name}/vars.tfvars"
  terraform_dir="terraform.tfstate.d/${testbed_name}"
  if [ ! -e ".terraform" ]; then
    terraform init
  fi
else
  var_file="${terraform_dir}/${testbed_name}/vars.tfvars"
  tf_var_file="${terraform_dir}/terraform-${vsphere_server}.tfvars"
  terraform_dir="${terraform_dir}/${testbed_name}"
  if [ -e ${var_file} ]; then
    terraform workspace new ${testbed_name}
  fi
fi

echo ${terraform_dir} ${var_file}
if [ ! -e "${terraform_dir}" ]; then
  echo "${testbed_name} doesn't exist in local workspace"
  exit 0
fi

echo ====== Deleting ${testbed_name} from Local Workspace ======
terraform workspace "select" "${testbed_name}"
source ${var_file}
if [ -z ${tf_var_file} ]; then
  tf_var_file="terraform-${vsphere_server}.tfvars"
fi
terraform destroy -lock=false -auto-approve -var vsphere_password=${vc_passwd} -var-file=${tf_var_file} "-var-file=${var_file}" -parallelism=20
terraform workspace "select" default
terraform workspace delete "${testbed_name}"
echo ====== Deleted ${testbed_name} from Local Workspace ======
echo ====== Deleting ${testbed_name} from Shared Workspace ======
rm -rf "${HOME}/terraform.tfstate.d/current/${testbed_name}"
echo ====== Deleted ${testbed_name} from Shared Workspace ======
