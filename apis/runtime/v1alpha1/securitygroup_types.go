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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Rule struct {
	Action      string   `json:"action,omitempty"`
	Description string   `json:"description,omitempty"`
	Destination []string `json:"destination,omitempty"`
	Id          string   `json:"id,omitempty"`
	Ingress     bool     `json:"ingress"`
	Name        string   `json:"name,omitempty"`
	Port        string   `json:"port"`
	Priority    int32    `json:"priority,omitempty"`
	Protocol    string   `json:"protocol"`
	Source      []string `json:"source,omitempty"`
}

type SecurityGroupStatus struct {
	// CloudName is the cloud assigned name of the SG.
	CloudName string `json:"cloudName,omitempty"`
	// CloudId is the cloud assigned ID of the SG.
	CloudId string `json:"cloudId,omitempty"`
	// Provider specifies cloud provider of the SG.
	Provider CloudProvider `json:"provider,omitempty"`
	// Region indicates the cloud region of the SG.
	Region string `json:"region"`
	// Rules contains ingress and egress rules of the SG.
	Rules []Rule `json:"rules,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SecurityGroup is the Schema for the Security Group API.
// Security Group object is automatically created upon CloudProviderAccount CR add.
type SecurityGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status SecurityGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SecurityGroupList is a list of Security Group objects.
type SecurityGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SecurityGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecurityGroup{}, &SecurityGroupList{})
	SchemeBuilder.SchemeBuilder.Register(addSgConversionFuncs)
}

func addSgConversionFuncs(scheme *runtime.Scheme) error {
	return scheme.AddFieldLabelConversionFunc(SchemeGroupVersion.WithKind("SecurityGroup"),
		func(label, value string) (string, string, error) {
			switch label {
			case "metadata.name", "metadata.namespace", "status.cloudId":
				return label, value, nil
			default:
				return "", "", fmt.Errorf("field label not supported: %s", label)
			}
		},
	)
}
