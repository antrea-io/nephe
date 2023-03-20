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

package utils

import (
	"fmt"
	"strings"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
)

var ErrorMsgUnknownCloudProvider = "missing cloud provider config. Please add AWS or Azure Config"

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

// TODO: Add comments on functions and evaluate if we can use deepequal compare?
func AreDiscoveredFieldsSameVirtualMachineStatus(s1, s2 runtimev1alpha1.VirtualMachineStatus) bool {
	if &s1 == &s2 {
		return true
	}
	if s1.Provider != s2.Provider {
		return false
	}
	if s1.State != s2.State {
		return false
	}
	if s1.VirtualPrivateCloud != s2.VirtualPrivateCloud {
		return false
	}
	if len(s1.Tags) != len(s2.Tags) ||
		len(s1.NetworkInterfaces) != len(s2.NetworkInterfaces) {
		return false
	}
	if !areTagsSame(s1.Tags, s2.Tags) {
		return false
	}
	if !areNetworkInterfacesSame(s1.NetworkInterfaces, s2.NetworkInterfaces) {
		return false
	}
	if s1.Agented != s2.Agented {
		return false
	}
	return true
}

func areTagsSame(s1, s2 map[string]string) bool {
	for key1, value1 := range s1 {
		value2, found := s2[key1]
		if !found {
			return false
		}
		if strings.Compare(strings.ToLower(value1), strings.ToLower(value2)) != 0 {
			return false
		}
	}
	return true
}

func areNetworkInterfacesSame(s1, s2 []runtimev1alpha1.NetworkInterface) bool {
	if &s1 == &s2 {
		return true
	}

	if len(s1) != len(s2) {
		return false
	}
	s1NameMap := convertNetworkInterfacesToMap(s1)
	s2NameMap := convertNetworkInterfacesToMap(s2)
	for key1, value1 := range s1NameMap {
		value2, found := s2NameMap[key1]
		if !found {
			return false
		}
		if strings.Compare(strings.ToLower(value1.Name), strings.ToLower(value2.Name)) != 0 {
			return false
		}
		if strings.Compare(strings.ToLower(value1.MAC), strings.ToLower(value2.MAC)) != 0 {
			return false
		}
		if len(value1.IPs) != len(value2.IPs) {
			return false
		}
		if !areIPAddressesSame(value1.IPs, value2.IPs) {
			return false
		}
	}
	return true
}

func areIPAddressesSame(s1, s2 []runtimev1alpha1.IPAddress) bool {
	s1Map := convertAddressToMap(s1)
	s2Map := convertAddressToMap(s2)
	for key1 := range s1Map {
		_, found := s2Map[key1]
		if !found {
			return false
		}
	}
	return true
}

func convertAddressToMap(addresses []runtimev1alpha1.IPAddress) map[string]struct{} {
	ipAddressMap := make(map[string]struct{})
	for _, address := range addresses {
		key := fmt.Sprintf("%v:%v", address.AddressType, address.Address)
		ipAddressMap[key] = struct{}{}
	}
	return ipAddressMap
}

func convertNetworkInterfacesToMap(nwInterfaces []runtimev1alpha1.NetworkInterface) map[string]runtimev1alpha1.NetworkInterface {
	nwInterfaceMap := make(map[string]runtimev1alpha1.NetworkInterface)

	for _, nwIFace := range nwInterfaces {
		nwInterfaceMap[nwIFace.Name] = nwIFace
	}
	return nwInterfaceMap
}
