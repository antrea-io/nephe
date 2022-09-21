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

# This script helps to cleanup stale dynamic vm for nephe. It accepts two parameters,
# `goVcPassword` is password for vc the dynamic vm is deployed on, `terraform-dir` is
# the path to store vm information.

_usage="Usage: $0 [--goVcPassword <Password for VC>]
  --goVcPassword          Password to the user name for VC.
  --terraform-dir         directory for terraform."

# This is the max timeout we think the dynamic vm is stale.
timeout=14400
function echoerr() {
  >&2 echo "$@"
}

function print_usage() {
  echoerr "$_usage"
}

function print_help() {
  echoerr "Try '$0 --help' for more information."
}

while [[ $# -gt 0 ]]
do
key="$1"

case $key in
  --goVcPassword)
    goVcPassword="$2"
    shift 2
    ;;
  --terraform-dir)
    terraformDir="$2"
    shift 2
    ;;
  -h|--help)
    print_usage
    exit 0
    ;;
  *)
    echoerr "unknow option $1"
    print_help
    exit 1
    ;;
esac
done

if [ -z ${terraformDir} ]; then
  terraformDir="${HOME}/terraform.tfstate.d/current/"
fi
echo ${terraformDir}
chmod +x ./ci/jenkins/destroy.sh
for testbed_name in $(ls ${terraformDir}); do
  if [ -d ${terraformDir}/${testbed_name} ]; then
    start_time=$(date "+%s" --date `cat "${terraformDir}/${testbed_name}"/terraform.tfstate|jq -r .resources[6].instances[0].attributes.change_version`)
    curr_time=$(date "+%s")
    delta=$((${curr_time}-${start_time}))
    if [ ${delta} > ${timeout} ]; then
      echo "testbed ${testbed_name} is stale, and it will be destroyed"
      ./ci/jenkins/destroy.sh "${testbed_name}" "${goVcPassword}" "${terraformDir}"
    else
      echo "testbed ${testbed_name} is in use"
    fi
  fi
done