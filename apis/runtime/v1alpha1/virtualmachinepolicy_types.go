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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Realization string

const (
	Success    Realization = "SUCCESS"
	InProgress Realization = "IN-PROGRESS"
	Failed     Realization = "FAILED"
)

type NetworkPolicyStatus struct {
	// Realization shows the status of a NetworkPolicy.
	Realization Realization
	// Reason shows the error message on NetworkPolicy application error.
	Reason string
}

// VirtualMachinePolicyStatus defines the observed state of VirtualMachinePolicy.
// It contains observable parameters.
type VirtualMachinePolicyStatus struct {
	// Realization shows the summary of applied network policy status.
	Realization Realization `json:"realization,omitempty"`
	// NetworkPolicyDetails shows all the statuses of applied network policies.
	NetworkPolicyDetails map[string]*NetworkPolicyStatus `json:"networkPolicyStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// VirtualMachinePolicy is the Schema for the VirtualMachinePolicy API.
// A VirtualMachinePolicy object is converted from cloud.NetworkPolicyStatus for external access.
type VirtualMachinePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status VirtualMachinePolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// VirtualMachinePolicyList contains a list of VirtualMachinePolicy.
type VirtualMachinePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachinePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachinePolicy{}, &VirtualMachinePolicyList{})
}
