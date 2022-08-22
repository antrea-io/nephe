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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"k8s.io/apimachinery/pkg/types"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var (
	cloudprovideraccountlog = logf.Log.WithName("cloudprovideraccount-resource")
	clientK8s               k8sclient.Client
)

const MinPollInterval = 30

func (r *CloudProviderAccount) SetupWebhookWithManager(mgr ctrl.Manager) error {
	clientK8s = mgr.GetClient()
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// nolint:lll
// +kubebuilder:webhook:path=/mutate-crd-cloud-antrea-io-v1alpha1-cloudprovideraccount,mutating=true,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudprovideraccounts,verbs=create,versions=v1alpha1,name=mcloudprovideraccount.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Defaulter = &CloudProviderAccount{}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *CloudProviderAccount) Default() {
	cloudprovideraccountlog.Info("default", "name", r.Name)

	if r.Spec.PollIntervalInSeconds == nil {
		var defaultIntv uint = 60
		r.Spec.PollIntervalInSeconds = &defaultIntv
	}
}

// TODO(user): change verbs to :"verbs=create;update;delete" if you want to enable deletion validation.
// nolint:lll
// +kubebuilder:webhook:verbs=create;update,path=/validate-crd-cloud-antrea-io-v1alpha1-cloudprovideraccount,mutating=false,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudprovideraccounts,versions=v1alpha1,name=vcloudprovideraccount.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Validator = &CloudProviderAccount{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudProviderAccount) ValidateCreate() error {
	cloudprovideraccountlog.Info("validate create", "name", r.Name)

	cloudProviderType, err := r.GetAccountProviderType()
	if err != nil {
		return err
	}

	switch cloudProviderType {
	case AWSCloudProvider:
		if err := r.validateAWSAccount(); err != nil {
			return err
		}
	case AzureCloudProvider:
		if err := r.validateAzureAccount(); err != nil {
			return err
		}
	}

	if *r.Spec.PollIntervalInSeconds < MinPollInterval {
		return fmt.Errorf("pollIntervalInSeconds should be >= 30. If not specified, defaults to 60")
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudProviderAccount) ValidateUpdate(old runtime.Object) error {
	cloudprovideraccountlog.Info("validate update", "name", r.Name)

	cloudProviderType, err := r.GetAccountProviderType()
	if err != nil {
		return err
	}

	switch cloudProviderType {
	case AWSCloudProvider:
		if err := r.validateAWSAccount(); err != nil {
			return err
		}
	case AzureCloudProvider:
		if err := r.validateAzureAccount(); err != nil {
			return err
		}
	}

	if *r.Spec.PollIntervalInSeconds < MinPollInterval {
		return fmt.Errorf("pollIntervalInSeconds should be >= 30. If not specified, defaults to 60")
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudProviderAccount) ValidateDelete() error {
	cloudprovideraccountlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

func (r *CloudProviderAccount) GetAccountProviderType() (CloudProvider, error) {
	if r.Spec.AWSConfig != nil {
		return AWSCloudProvider, nil
	} else if r.Spec.AzureConfig != nil {
		return AzureCloudProvider, nil
	} else {
		return "", fmt.Errorf("missing cloud provider config. Please add AWS or Azure Config")
	}
}

func (r *CloudProviderAccount) validateAWSAccount() error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})

	awsConfig := r.Spec.AWSConfig

	err := clientK8s.Get(context.TODO(), types.NamespacedName{
		Namespace: awsConfig.SecretRef.Namespace,
		Name:      awsConfig.SecretRef.Name}, u)
	if err != nil {
		return fmt.Errorf("unable to get secret: %s", err.Error())
	}
	data := u.Object["data"].(map[string]interface{})
	decode, err := base64.StdEncoding.DecodeString(data[awsConfig.SecretRef.Key].(string))
	if err != nil {
		return fmt.Errorf("unable to decode the secret: %s", err.Error())
	}

	awsCredential := &AwsAccountCredential{}
	if err = json.Unmarshal(decode, awsCredential); err != nil {
		return fmt.Errorf("unable to unmarshal the json: %s", err.Error())
	}
	// validate roleArn or A
	if len(strings.TrimSpace(awsCredential.RoleArn)) != 0 {
		cloudprovideraccountlog.Info("Role ARN configured will be used for cloud-account access")
	} else if len(strings.TrimSpace(awsCredential.AccessKeyID)) == 0 || len(strings.TrimSpace(awsCredential.AccessKeySecret)) == 0 {
		return fmt.Errorf("must specify either credentials or role arn, cannot both be empty")
	}

	if len(strings.TrimSpace(awsConfig.Region)) == 0 {
		return fmt.Errorf("region cannot be blank or empty")
	}

	// NOTE: currently only AWS standard partition regions supported (aws-cn, aws-us-gov etc are not
	// supported). As we add support for other partitions, validation needs to be updated
	regions := endpoints.AwsPartition().Regions()
	_, found := regions[awsConfig.Region]
	if !found {
		var supportedRegions []string
		for key := range regions {
			supportedRegions = append(supportedRegions, key)
		}
		return fmt.Errorf("%v not in supported regions [%v]", awsConfig.Region, supportedRegions)
	}

	return nil
}

func (r *CloudProviderAccount) validateAzureAccount() error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})

	azureConfig := r.Spec.AzureConfig

	err := clientK8s.Get(context.TODO(), types.NamespacedName{
		Namespace: azureConfig.SecretRef.Namespace,
		Name:      azureConfig.SecretRef.Name}, u)
	if err != nil {
		return fmt.Errorf("unable to get secret: %s", err.Error())
	}
	data := u.Object["data"].(map[string]interface{})
	decode, err := base64.StdEncoding.DecodeString(data[azureConfig.SecretRef.Key].(string))
	if err != nil {
		return fmt.Errorf("unable to decode the secret: %s", err.Error())
	}

	azureCredential := &AzureAccountCredential{}
	if err = json.Unmarshal(decode, azureCredential); err != nil {
		return fmt.Errorf("unable to unmarshal the json: %s", err.Error())
	}

	// validate subscription ID
	if len(strings.TrimSpace(azureCredential.SubscriptionID)) == 0 {
		return fmt.Errorf("subscription id cannot be blank or empty")
	}
	// validate tenant ID
	if len(strings.TrimSpace(azureCredential.TenantID)) == 0 {
		return fmt.Errorf("tenant id cannot be blank or empty")
	}
	// validate credentials
	if len(strings.TrimSpace(azureCredential.ClientID)) == 0 || len(strings.TrimSpace(azureCredential.ClientKey)) == 0 {
		return fmt.Errorf("must specify either credentials or managed identity client id, cannot both be empty")
	}

	// validate region
	if len(strings.TrimSpace(azureConfig.Region)) == 0 {
		return fmt.Errorf("region cannot be blank or empty")
	}

	return nil
}
