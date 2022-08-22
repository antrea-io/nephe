#!/usr/bin/env bash

set -eo pipefail

# Generate mocks from interfaces in local packages.
# Mock may include private methods, therefore may be consumed by tests within the same package.
# First field is target package of mock;
# second field is source to generate mock from.
MOCKGEN_TARGETS=(
  "common pkg/cloud-provider/cloudapi/common/cloud"
  "aws pkg/cloud-provider/cloudapi/aws/aws_api_wrappers"
  "aws pkg/cloud-provider/cloudapi/aws/aws_services"
  "azure pkg/cloud-provider/cloudapi/azure/azure_api_wrappers"
  "azure pkg/cloud-provider/cloudapi/azure/azure_services"
)
for target in "${MOCKGEN_TARGETS[@]}"; do
  read -r package name <<<"${target}"
  mockgen \
    -copyright_file hack/boilerplate.go.txt \
    -destination "${name}-mock_test.go" \
    -package ${package} \
    -source ${name}.go
done

# Generate mocks from interfaces in external packages.
# Mock may include only public methods, therefore may be consumed by any tests.
# First field is go path to package that exports the interfaces, the second field is
# source interfaces to generate mocks from, separated by comma; the third field is
# the package name of generated mock.
MOCKGEN_TARGETS=(
  "sigs.k8s.io/controller-runtime/pkg/client Client,StatusWriter controllerruntimeclient"
  "antrea.io/nephe/pkg/cloud-provider/securitygroup CloudSecurityGroupAPI cloudsecurity"
  "antrea.io/nephe/pkg/controllers/cloud NetworkPolicyController networkpolicy"
)

for target in "${MOCKGEN_TARGETS[@]}"; do
  read -r package interfaces dst <<<"${target}"
  mockgen \
    -copyright_file hack/boilerplate.go.txt \
    -destination "pkg/testing/${dst}/mock.go" \
    -package="${dst}" \
    "${package}" "${interfaces}"
done
