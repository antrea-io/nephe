#!/usr/bin/env bash

# Copyright 2022 Antrea Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eo pipefail

function echoerr {
    >&2 echo "$@"
}

_usage="Usage: $0 [--help|-h]
Generate a YAML manifest for Nephe using Kustomize and print it to stdout.
        --help    | -h                      Print this message and exit
Environment variables IMG_NAME and IMG_TAG must be set when release mode is enabled.
"

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
    -h|--help)
    print_usage
    exit 0
    ;;
    *)    # unknown option
    echoerr "Unknown option $1"
    exit 1
    ;;
esac
done

if [ -z "$IMG_TAG" ]; then
    echoerr "Environment variable IMG_TAG must be set"
    print_help
    exit 1
fi

if [ -z "$IMG_NAME" ]; then
    echoerr "Environment variable IMG_NAME must be set"
    print_help
    exit 1
fi

WORK_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

source $WORK_DIR/verify-kustomize.sh

if [ -z "$KUSTOMIZE" ]; then
    KUSTOMIZE="$(verify_kustomize)"
elif ! $KUSTOMIZE version > /dev/null 2>&1; then
    echoerr "$KUSTOMIZE does not appear to be a valid kustomize binary"
    print_help
    exit 1
fi

KUSTOMIZATION_DIR=$WORK_DIR/../
TMP_DIR=$(mktemp -d $KUSTOMIZATION_DIR/overlays.XXXXXXXX)
pushd $TMP_DIR > /dev/null

cp -r $WORK_DIR/../config/. .
cd manager
$KUSTOMIZE edit set image projects.registry.vmware.com/antrea/nephe:latest=$IMG_NAME:$IMG_TAG

# Remove debug log from the manifest
$KUSTOMIZE build ../default | sed -n '/enable-debug-log/!p'

rm -rf $TMP_DIR
