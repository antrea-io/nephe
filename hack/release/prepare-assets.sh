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

# This script generates all the assets required for an Antrea Github release to
# the provided directory.

# Usage: VERSION=v1.0.0 ./prepare-assets.sh <output dir>
# In addition to the VERSION environment variable (which is required), the
# PRERELEASE environment variable can also be set to true or false (it will
# default to false).

set -eo pipefail

function echoerr {
    >&2 echo "$@"
    exit 1
}

if [ -z "$VERSION" ]; then
    echoerr "Environment variable VERSION must be set"
fi

if [ -z "$1" ]; then
    echoerr "Argument required: output directory for assets"
fi

: "${PRERELEASE:=false}"
if [ "$PRERELEASE" != "true" ] && [ "$PRERELEASE" != "false" ]; then
    echoerr "Environment variable PRERELEASE should only be set to 'true' or 'false'"
fi
export PRERELEASE

THIS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

pushd $THIS_DIR/../.. > /dev/null

mkdir -p "$1"
OUTPUT_DIR=$(cd "$1" && pwd)

export IMG_TAG=$VERSION
export IMG_NAME=projects.registry.vmware.com/antrea/nephe
./hack/generate-manifest.sh > "$OUTPUT_DIR"/nephe.yml
ls "$OUTPUT_DIR" | cat
