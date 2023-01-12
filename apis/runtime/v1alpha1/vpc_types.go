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

	"antrea.io/nephe/apis/crd/v1alpha1"
)

type VpcInfo struct {
	Name          string
	Id            string
	CloudProvider v1alpha1.CloudProvider
	Region        string
	Tags          map[string]string
	Cidrs         []string
}

// +kubebuilder:object:root=true
type Vpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Info VpcInfo `json:"spec,omitempty"`
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
