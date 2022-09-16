#!/bin/bash
_usage="Usage: $0 [--goVcPassword <Password for VC>]
  --goVcPassword          Password to the user name for VC.
  --terraform-dir         directory for terraform."
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
esac
done

if [ -z ${terraformDir} ]; then
  terraformDir="${HOME}/terraform.tfstate.d/current/"
fi
echo ${terraformDir}
for testbed_name in $(ls ${terraformDir}); do
  if [ -d ${terraformDir}/${testbed_name} ]; then
    start_time=$(date "+%s" --date `cat "${terraformDir}/${testbed_name}"/terraform.tfstate|jq -r .resources[6].instances[0].attributes.change_version`)
    curr_time=$(date "+%s")
    delta=$((${curr_time}-${start_time}))
    timeout=10800
    if [ ${delta} > ${timeout} ]; then
      echo "testbed ${testbed_name} is stale, and it will be destroyed"
      #./destroy.sh "${testbed_name}" "${goVcPassword}" "${terraformDir}"
    else
      echo "testbed ${testbed_name} is in use"
    fi
  fi
done