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

package testing

import (
	"fmt"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"

	cloud "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/converter/target"
)

func SetupVirtualMachine(vm *cloud.VirtualMachine, name, namespace string, nics ...*cloud.NetworkInterface) {
	vm.Status.NetworkInterfaces = nil
	for _, nic := range nics {
		vm.Status.NetworkInterfaces = append(vm.Status.NetworkInterfaces, *nic)
	}
	vm.Name = name
	vm.Namespace = namespace
	vm.Status.Tags = map[string]string{"test-vm-tag": "test-vm-key"}
	vm.Status.VirtualPrivateCloud = "test-vm-vpc"
}

func SetupVirtualMachineOwnerOf(vm *source.VirtualMachineSource, name, namespace string,
	nics ...*cloud.NetworkInterface) {
	SetupVirtualMachine(&vm.VirtualMachine, name, namespace, nics...)
}

func SetupNetworkInterface(nic *cloud.NetworkInterface, name string, ips []string) {
	nic.Name = name
	nic.IPs = nil
	for _, ip := range ips {
		nic.IPs = append(nic.IPs, cloud.IPAddress{Address: ip})
	}
}

// SetupExternalEntitySources returns externalEntitySource resources for testing.
func SetupExternalEntitySources(ips []string, namespace string) map[string]target.ExternalEntitySource {
	sources := make(map[string]target.ExternalEntitySource)
	virtualMachine := &source.VirtualMachineSource{}
	sources["VirtualMachine"] = virtualMachine

	networkInterfaces := make([]*cloud.NetworkInterface, 0)
	for i, ip := range ips {
		name := "nic" + fmt.Sprintf("%d", i)
		nic := &cloud.NetworkInterface{}
		SetupNetworkInterface(nic, name, []string{ip})
		networkInterfaces = append(networkInterfaces, nic)
	}
	SetupVirtualMachineOwnerOf(virtualMachine, "test-vm", namespace, networkInterfaces...)

	return sources
}

func SetNetworkInterfaceIP(kind string, source client.Object, name string, ip string) client.Object {
	if kind == reflect.TypeOf(cloud.VirtualMachine{}).Name() {
		vm := (source).(*cloud.VirtualMachine)
		nics := &vm.Status.NetworkInterfaces
		//assign ip address to the nic passed to the function
		for i, nic := range *nics {
			if strings.Compare(nic.Name, name) == 0 {
				if strings.Compare(ip, "") == 0 {
					nic.IPs = nil
				} else {
					nic.IPs = []cloud.IPAddress{{Address: ip}}
				}
			}
			(*nics)[i] = nic
		}
		return vm
	}
	return nil
}
