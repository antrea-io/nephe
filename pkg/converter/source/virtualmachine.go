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

package source

import (
	config2 "antrea.io/nephe/pkg/config"
	"antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/util"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	"antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/converter/target"
)

// VirtualMachineSource says VirtualMachine is a source of converter targets.
type VirtualMachineSource struct {
	v1alpha1.VirtualMachine
	watch.EventType
}

// GetEndPointAddresses returns VirtualMachine's IP addresses.
func (v *VirtualMachineSource) GetEndPointAddresses() ([]string, error) {
	ipAddrs := util.GetVMIPAddresses(&v.VirtualMachine)
	ip := make([]string, 0, len(ipAddrs))
	for _, ipAddr := range ipAddrs {
		ip = append(ip, ipAddr.Address)
	}
	return ip, nil
}

// GetNetworkInterfaces returns VirtualMachine's IP addresses.
func (v *VirtualMachineSource) GetNetworkInterfaces() ([]v1alpha1.NetworkInterface, error) {
	return v.Status.NetworkInterfaces, nil
}

// GetEndPointPort returns nil as VirtualMachine has no associated port.
func (v *VirtualMachineSource) GetEndPointPort(_ client.Client) []antreatypes.NamedPort {
	return nil
}

// GetTags returns tags of VirtualMachine.
func (v *VirtualMachineSource) GetTags() map[string]string {
	return v.Status.Tags
}

// GetLabelsFromClient returns VirtualMachine specific labels.
func (v *VirtualMachineSource) GetLabelsFromClient(_ client.Client) map[string]string {
	return map[string]string{
		labels.ExternalEntityLabelKeyOwnerVmVpc:   v.Labels[labels.VpcName],
		labels.ExternalEntityLabelKeyCloudVpcName: v.Status.CloudVpcName,
		labels.ExternalEntityLabelKeyCloudVpcUID:  v.Labels[labels.CloudVpcUID],
		labels.ExternalEntityLabelKeyCloudVmName:  v.Status.CloudName,
		labels.ExternalEntityLabelKeyCloudVmUID:   v.Labels[labels.CloudVmUID],
		labels.ExternalEntityLabelKeyCloudRegion:  v.Status.Region,
	}
}

// GetExternalNodeName returns controller associated with VirtualMachine.
func (v *VirtualMachineSource) GetExternalNodeName(_ client.Client) string {
	return config2.ANPNepheController
}

// Copy returns a duplicate of VirtualMachineSource.
func (v *VirtualMachineSource) Copy() (duplicate interface{}) {
	newVM := &VirtualMachineSource{}
	v.VirtualMachine.DeepCopyInto(&newVM.VirtualMachine)
	return newVM
}

// EmbedType returns VirtualMachine resource.
func (v *VirtualMachineSource) EmbedType() client.Object {
	return &v.VirtualMachine
}

func (v *VirtualMachineSource) IsFedResource() bool {
	return false
}

var (
	_ target.ExternalEntitySource = &VirtualMachineSource{}
	_ target.ExternalNodeSource   = &VirtualMachineSource{}
	_ client.Object               = &VirtualMachineSource{}
)
