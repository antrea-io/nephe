// Copyright 2022 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
)

var (
	ErrorMsgUnknownCloudProvider = "missing cloud provider config. Please add AWS or Azure Config"
	ErrorMsgSecretReference      = "error fetching Secret reference"
)

// GetVMIPAddresses returns IP addresses of all network interfaces attached to the vm.
func GetVMIPAddresses(vm *runtimev1alpha1.VirtualMachine) []runtimev1alpha1.IPAddress {
	ipLen := len(vm.Status.NetworkInterfaces)
	if ipLen == 0 {
		return nil
	}
	ips := make([]runtimev1alpha1.IPAddress, 0, ipLen)
	for _, value := range vm.Status.NetworkInterfaces {
		ips = append(ips, value.IPs...)
	}
	return ips
}

// GetAccountProviderType returns cloud provider type from CPA.
func GetAccountProviderType(account *crdv1alpha1.CloudProviderAccount) (runtimev1alpha1.CloudProvider, error) {
	if account.Spec.AWSConfig != nil {
		return runtimev1alpha1.AWSCloudProvider, nil
	} else if account.Spec.AzureConfig != nil {
		return runtimev1alpha1.AzureCloudProvider, nil
	} else {
		return "", fmt.Errorf("%s", ErrorMsgUnknownCloudProvider)
	}
}
