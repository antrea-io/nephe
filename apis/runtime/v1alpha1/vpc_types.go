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

type VpcStatus struct {
	// CloudName is the cloud assigned name of the VPC.
	CloudName string `json:"cloudName,omitempty"`
	// CloudId is the cloud assigned ID of the VPC.
	CloudId string `json:"cloudId,omitempty"`
	// Provider specifies cloud provider of the VPC.
	Provider CloudProvider `json:"provider,omitempty"`
	// Region indicates the cloud region of the VPC.
	Region string `json:"region,omitempty"`
	// Tags indicates tags associated with the VPC.
	Tags map[string]string `json:"tags,omitempty"`
	// Cidrs is the IPv4 CIDR block associated with the VPC.
	Cidrs []string `json:"cidrs,omitempty"`
	// Managed flag indicates if the VPC is managed by Nephe.
	Managed bool `json:"managed,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Vpc is the Schema for the Vpc API.
// A Vpc object is automatically created upon CloudProviderAccount CR add.
type Vpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status VpcStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VpcList is a list of Vpc objects.
type VpcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Vpc `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Vpc{}, &VpcList{})
}
