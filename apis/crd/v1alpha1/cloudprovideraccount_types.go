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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:validation:Enum=Azure;AWS
// CloudProvider specifies a cloud provider.
type CloudProvider string

const (
	// AzureCloudProvider specifies Azure.
	AzureCloudProvider CloudProvider = "Azure"
	// AWSCloudProvider specifies AWS.
	AWSCloudProvider CloudProvider = "AWS"
)

// CloudProviderAccountSpec defines the desired state of CloudProviderAccount.
type CloudProviderAccountSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster.
	// Important: Run "make" to regenerate code after modifying this file.

	// PollIntervalInSeconds defines account poll interval (default value is 60, if not specified).
	PollIntervalInSeconds *uint `json:"pollIntervalInSeconds,omitempty"`
	// Cloud provider account config.
	AWSConfig *CloudProviderAccountAWSConfig `json:"awsConfig,omitempty"`
	// Cloud provider account config.
	AzureConfig *CloudProviderAccountAzureConfig `json:"azureConfig,omitempty"`
}

type CloudProviderAccountAWSConfig struct {
	// Reference to k8s secret which has cloud provider credentials.
	SecretRef *SecretReference `json:"secretRef,omitempty"`
	// Cloud provider account region.
	Region string `json:"region,omitempty"`
}

type CloudProviderAccountAzureConfig struct {
	SecretRef *SecretReference `json:"secretRef,omitempty"`
	Region    string           `json:"region,omitempty"`
}

// SecretReference is a reference to a k8s secret resource in an arbitrary namespace.
type SecretReference struct {
	// Name of the secret.
	Name string `json:"name"`
	// Namespace of the secret.
	Namespace string `json:"namespace"`
	// Key to select in the secret.
	Key string `json:"key"`
}

// AwsAccountCredential is the format of k8s secret for aws provider account.
type AwsAccountCredential struct {
	AccessKeyID     string `json:"accessKeyId,omitempty"`
	AccessKeySecret string `json:"accessKeySecret,omitempty"`
	RoleArn         string `json:"roleArn,omitempty"`
	ExternalID      string `json:"externalId,omitempty"`
}

// AzureAccountCredential is the format of k8s secret for azure provider account.
type AzureAccountCredential struct {
	SubscriptionID string `json:"subscriptionId,omitempty"`
	ClientID       string `json:"clientId,omitempty"`
	TenantID       string `json:"tenantId,omitempty"`
	ClientKey      string `json:"clientKey,omitempty"`
}

// CloudProviderAccountStatus defines the observed state of CloudProviderAccount.
type CloudProviderAccountStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// Error is current error, if any, of the CloudProviderAccount.
	Error string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true

// +kubebuilder:resource:shortName="cpa"
// +kubebuilder:subresource:status
// CloudProviderAccount is the Schema for the cloudprovideraccounts API.
type CloudProviderAccount struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudProviderAccountSpec   `json:"spec,omitempty"`
	Status CloudProviderAccountStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudProviderAccountList contains a list of CloudProviderAccount.
type CloudProviderAccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudProviderAccount `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudProviderAccount{}, &CloudProviderAccountList{})
}
