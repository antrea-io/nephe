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
set -e

tesbted_name="$1"
vc_passwd="$2"
var_file="terraform.tfstate.d/${tesbted_name}/vars.tfvars"

if [ -z "${tesbted_name}" ]; then
  echo "Usage: $0 <testbed_name>"
  exit 1
fi

if [ ! -e ".terraform" ]; then
  terraform init
fi

if [ ! -e "terraform.tfstate.d/${tesbted_name}" ]; then
  echo "${tesbted_name} doesn't exist in local workspace"
  exit 1
fi

echo ====== Deleting ${tesbted_name} from Local Workspace ======
terraform workspace "select" "${tesbted_name}"
source ${var_file}
terraform destroy -lock=false -auto-approve -var vsphere_password=${vc_passwd} -var-file=terraform-${vsphere_server}.tfvars "-var-file=${var_file}" -parallelism=20
terraform workspace "select" default
terraform workspace delete "${tesbted_name}"
echo ====== Deleted ${tesbted_name} from Local Workspace ======
echo ====== Deleting ${tesbted_name} from Shared Workspace ======
rm -rf "../terraform.tfstate.d/current/${tesbted_name}"
echo ====== Deleted ${tesbted_name} from Shared Workspace ======
