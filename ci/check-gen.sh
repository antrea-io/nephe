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

#

# This script makes sure that the checked-in generated code is up-to-date.

set -eo pipefail

function echoerr {
    >&2 echo "$@"
}

make generate
diff="$(git status --porcelain)"
if [ ! -z "$diff" ]; then
    echoerr "The generated code is not up-to-date"
    echoerr $diff
    echoerr "You can regenerate it with 'make generate' and commit the changes"
    exit 1
fi

make manifests
diff="$(git status --porcelain)"

if [ ! -z "$diff" ]; then
    echoerr "The generated manifests is not up-to-date"
    echoerr $diff
    echoerr "You can regenerate it with 'make manifests' and commit the changes"
    exit 1
fi

make mock
diff="$(git status --porcelain)"

if [ ! -z "$diff" ]; then
    echoerr "The generated mock is not up-to-date"
    echoerr $diff
    echoerr "You can regenerate it with 'make mock' and commit the changes"
    exit 1
fi
