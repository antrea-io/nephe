// Copyright 2023 Antrea Authors.
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

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type AddressType string

const (
	// AddressTypeInternalIP is internal IP.
	AddressTypeInternalIP AddressType = "InternalIP"
	// AddressTypeExternalIP external IP.
	AddressTypeExternalIP AddressType = "ExternalIP"
)

type VMState string

type CloudProvider string

const (
	// AzureCloudProvider specifies Azure.
	AzureCloudProvider CloudProvider = "Azure"
	// AWSCloudProvider specifies AWS.
	AWSCloudProvider CloudProvider = "AWS"
)

const (
	Running      VMState = "running"
	Stopped      VMState = "stopped"
	Stopping     VMState = "stopping"
	ShuttingDown VMState = "shutting down"
	Starting     VMState = "starting"
	Unknown      VMState = "unknown"
)

func (this VMState) String() string {
	switch this {
	case Running:
		return "running"
	case Stopped:
		return "stopped"
	case Stopping:
		return "stopping"
	case ShuttingDown:
		return "shutting down"
	case Starting:
		return "starting"
	case Unknown:
		return "unknown status"
	default:
		return "unknown status"
	}
}

type IPAddress struct {
	AddressType AddressType `json:"addressType"`
	Address     string      `json:"address"`
}

// NetworkInterface contains information pertaining to NetworkInterface.
type NetworkInterface struct {
	Name string `json:"name,omitempty"`
	// Hardware address of the interface.
	MAC string `json:"mac,omitempty"`
	// IP addresses of this NetworkInterface.
	IPs []IPAddress `json:"ips,omitempty"`
}

// VirtualMachineStatus defines the observed state of VirtualMachine
// It contains observable parameters.
type VirtualMachineStatus struct {
	// Provider specifies cloud provider of this VirtualMachine.
	Provider CloudProvider `json:"provider,omitempty"`
	// Tags of this VirtualMachine. A corresponding label is also generated for each tag.
	Tags map[string]string `json:"tags,omitempty"`
	// NetworkInterfaces is array of NetworkInterfaces attached to this VirtualMachine.
	NetworkInterfaces []NetworkInterface `json:"networkInterfaces,omitempty"`
	// State indicates current state of the VirtualMachine.
	State VMState `json:"state,omitempty"`
	// Region indicates the cloud region of the VirtualMachine.
	Region string `json:"region,omitempty"`
	// Agented specifies if VM runs in agented mode, default is false.
	Agented bool `json:"agented"`
	// CloudId is the cloud assigned ID of the VM.
	CloudId string `json:"cloudId,omitempty"`
	// CloudName is the cloud assigned name of the VM.
	CloudName string `json:"cloudName,omitempty"`
	// CloudVpcId is the VPC ID this VirtualMachine belongs to.
	CloudVpcId string `json:"cloudVpcId,omitempty"`
	// CloudVpcName is the VPC Name this VirtualMachine belongs to.
	CloudVpcName string `json:"cloudVpcName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VirtualMachine is the Schema for the virtualmachines API
// A virtualMachine object is created automatically based on
// matching criteria specification of CloudEntitySelector.
type VirtualMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status VirtualMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VirtualMachineList contains a list of VirtualMachine.
type VirtualMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachine{}, &VirtualMachineList{})
	SchemeBuilder.SchemeBuilder.Register(addVmConversionFuncs)
}

func addVmConversionFuncs(scheme *runtime.Scheme) error {
	return scheme.AddFieldLabelConversionFunc(SchemeGroupVersion.WithKind("VirtualMachine"),
		func(label, value string) (string, string, error) {
			switch label {
			case "metadata.name", "metadata.namespace", "status.cloudId", "status.cloudVpcId", "status.region":
				return label, value, nil
			default:
				return "", "", fmt.Errorf("field label not supported: %s", label)
			}
		},
	)
}
