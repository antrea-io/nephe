package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type VpcInfo struct {
	Name string
	Id   string
}

// +kubebuilder:object:root=true
type Vpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Info              VpcInfo `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// VpcList is a list of Vpc objects.
type VpcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Vpc `json:"items"`
}
