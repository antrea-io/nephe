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
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/converter/target"
)

func SetupVirtualMachine(vm *runtimev1alpha1.VirtualMachine, name, namespace string, agented bool,
	nics ...*runtimev1alpha1.NetworkInterface) {
	vm.Status.NetworkInterfaces = nil
	for _, nic := range nics {
		vm.Status.NetworkInterfaces = append(vm.Status.NetworkInterfaces, *nic)
	}
	vm.Name = name
	vm.Namespace = namespace
	vm.Status.Tags = map[string]string{"test-vm-tag": "test-vm-key"}
	vm.Status.Agented = agented
}

func SetupVirtualMachineOwnerOf(vm *source.VirtualMachineSource, name, namespace string,
	agented bool, nics ...*runtimev1alpha1.NetworkInterface) {
	SetupVirtualMachine(&vm.VirtualMachine, name, namespace, agented, nics...)
}

func SetupNetworkInterface(nic *runtimev1alpha1.NetworkInterface, name string, ips []string) {
	nic.Name = name
	nic.IPs = nil
	for _, ip := range ips {
		nic.IPs = append(nic.IPs, runtimev1alpha1.IPAddress{Address: ip})
	}
}

// SetupExternalEntitySources returns externalEntitySource resources for testing.
func SetupExternalEntitySources(ips []string, namespace string) map[string]target.ExternalEntitySource {
	sources := make(map[string]target.ExternalEntitySource)
	virtualMachine := &source.VirtualMachineSource{}
	sources["VirtualMachine"] = virtualMachine

	networkInterfaces := make([]*runtimev1alpha1.NetworkInterface, 0)
	for i, ip := range ips {
		name := "nic" + fmt.Sprintf("%d", i)
		nic := &runtimev1alpha1.NetworkInterface{}
		SetupNetworkInterface(nic, name, []string{ip})
		networkInterfaces = append(networkInterfaces, nic)
	}
	SetupVirtualMachineOwnerOf(virtualMachine, "test-vm", namespace, false, networkInterfaces...)

	return sources
}

// SetupExternalNodeSources returns externalNodeSource resources for testing.
func SetupExternalNodeSources(ips []string, namespace string) map[string]target.ExternalNodeSource {
	sources := make(map[string]target.ExternalNodeSource)
	virtualMachine := &source.VirtualMachineSource{}
	sources["VirtualMachine"] = virtualMachine

	networkInterfaces := make([]*runtimev1alpha1.NetworkInterface, 0)
	// Currently only one NetworkInterface with multiple IPs is supported.
	i := 0
	name := "nic" + fmt.Sprintf("%d", i)
	nic := &runtimev1alpha1.NetworkInterface{}
	SetupNetworkInterface(nic, name, ips)
	networkInterfaces = append(networkInterfaces, nic)
	SetupVirtualMachineOwnerOf(virtualMachine, "test-vm", namespace, true, networkInterfaces...)

	return sources
}

func SetNetworkInterfaceIP(kind string, source client.Object, name string, ip string) client.Object {
	if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
		vm := (source).(*runtimev1alpha1.VirtualMachine)
		nics := &vm.Status.NetworkInterfaces
		//assign ip address to the nic passed to the function
		for i, nic := range *nics {
			if strings.Compare(nic.Name, name) == 0 {
				if strings.Compare(ip, "") == 0 {
					nic.IPs = nil
				} else {
					nic.IPs = []runtimev1alpha1.IPAddress{{Address: ip}}
				}
			}
			(*nics)[i] = nic
		}
		return vm
	}
	return nil
}
