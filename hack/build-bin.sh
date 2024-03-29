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


set -eo pipefail
echo "GOBIN=$(pwd)/bin CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go install -ldflags="-s -w" antrea.io/nephe/cmd/..."
GOBIN=$(pwd)/bin CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go install -ldflags="-s -w" antrea.io/nephe/cmd/...
echo "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go test -c antrea.io/nephe/test/integration/ -ldflags="-s -w" -o ./ci/bin/integration.test"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go test -c antrea.io/nephe/test/integration/ -ldflags="-s -w" -o ./ci/bin/integration.test
echo "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go test -c antrea.io/nephe/test/upgrade/ -ldflags="-s -w" -o ./ci/bin/upgrade.test"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go test -c antrea.io/nephe/test/upgrade/ -ldflags="-s -w" -o ./ci/bin/upgrade.test
