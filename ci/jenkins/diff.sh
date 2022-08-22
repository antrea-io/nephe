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

set -e

tesbted_name="$1"

if [ -z "${tesbted_name}" ]; then
  echo "Usage: $0 <testbed_name>"
  exit 1
fi

if [ ! -e "terraform.tfstate.d/${tesbted_name}" ]; then
  echo "${tesbted_name} does not exist in local workspace"
  exit 1
fi

if [ ! -e "../terraform.tfstate.d/current/${tesbted_name}" ]; then
  echo "${tesbted_name} does not exist in remote workspace"
  exit 1
fi

echo ====== Showing Differences for ${tesbted_name} between Local and Shared Workspaces======
diff -sur "terraform.tfstate.d/${tesbted_name}" "../terraform.tfstate.d/current/${tesbted_name}"
