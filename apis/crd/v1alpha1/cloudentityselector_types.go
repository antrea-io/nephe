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

// EntityMatch specifies match conditions to cloud entities.
// Cloud entities must satisfy all fields(ANDed) in EntityMatch to satisfy EntityMatch.
type EntityMatch struct {
	// MatchName matches cloud entities' name. If not specified, it matches any cloud entities.
	MatchName string `json:"matchName,omitempty"`
	// MatchID matches cloud entities' identifier. If not specified, it matches any cloud entities.
	MatchID string `json:"matchID,omitempty"`
}

// VirtualMachineSelector specifies VirtualMachine match criteria.
// VirtualMachines must satisfy all fields(ANDed) in a VirtualMachineSelector in order to satisfy match.
type VirtualMachineSelector struct {
	// VpcMatch specifies the virtual private cloud to which VirtualMachines belong.
	// VpcMatch is ANDed with VMMatch.
	// If it is not specified, VirtualMachines may belong to any virtual private cloud.
	VpcMatch *EntityMatch `json:"vpcMatch,omitempty"`
	// VMMatch specifies VirtualMachines to match.
	// It is an array, match satisfying any item on VMMatch is selected(ORed).
	// If it is not specified, all VirtualMachines matching VpcMatch are selected.
	VMMatch []EntityMatch `json:"vmMatch,omitempty"`
}

// CloudEntitySelectorSpec defines the desired state of CloudEntitySelector.
type CloudEntitySelectorSpec struct {
	// AccountName specifies cloud account in this CloudProvider.
	AccountName string `json:"accountName,omitempty"`
	// VMSelector selects the VirtualMachines the user has modify privilege.
	// VMSelector is mandatory, at least one selector under VMSelector is required.
	// It is an array, VirtualMachines satisfying any item on VMSelector are selected(ORed).
	VMSelector []VirtualMachineSelector `json:"vmSelector"`
}

// +kubebuilder:object:root=true

// +kubebuilder:resource:shortName="ces"
// CloudEntitySelector is the Schema for the cloudentityselectors API.
type CloudEntitySelector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CloudEntitySelectorSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// CloudEntitySelectorList contains a list of CloudEntitySelector.
type CloudEntitySelectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudEntitySelector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudEntitySelector{}, &CloudEntitySelectorList{})
}
