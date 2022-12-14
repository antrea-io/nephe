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

package webhook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/controllers/utils"
)

const MinPollInterval = 30

var (
	errorMsgSecretNotConfigured  = "unable to get secret"
	errorMsgMinPollInterval      = "pollIntervalInSeconds should be >= 30. If not specified, defaults to 60"
	errorMsgMissingCredential    = "must specify either credentials or role arn, cannot both be empty"
	errorMsgMissingRegion        = "region cannot be blank or empty"
	errorMsgInvalidRegion        = "not in supported regions"
	errorMsgJsonUnmarshalFail    = "unable to unmarshal the json"
	errorMsgMissingClientDetails = "client id and client key cannot be blank or empty"
	errorMsgMissingTenantID      = "tenant id cannot be blank or empty"
	errorMsgMissingSubscritionID = "subscription id cannot be blank or empty"
	errorMsgInvalidRequest       = "invalid admission webhook request"
	errorMsgDecodeFail           = "unable to decode the secret"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// nolint:lll
// +kubebuilder:webhook:path=/mutate-crd-cloud-antrea-io-v1alpha1-cloudprovideraccount,mutating=true,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudprovideraccounts,verbs=create,versions=v1alpha1,name=mcloudprovideraccount.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// CPAMutator is used to mutate CPA object.
type CPAMutator struct {
	Client  k8sclient.Client
	Log     logr.Logger
	decoder *admission.Decoder
}

// Handle handles mutator admission requests for CPA.
func (m *CPAMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	account := &v1alpha1.CloudProviderAccount{}
	err := m.decoder.Decode(req, account)
	if err != nil {
		m.Log.Error(err, "Failed to decode CloudProviderAccount", "CPAMutator", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	m.Log.V(1).Info("CPA Mutator", "name", account.Name)
	if account.Spec.PollIntervalInSeconds == nil {
		var defaultIntv uint = 60
		account.Spec.PollIntervalInSeconds = &defaultIntv
	}

	marshaledAccount, err := json.Marshal(account)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledAccount)
}

// InjectDecoder injects the decoder.
func (m *CPAMutator) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}

// TODO(user): change verbs to :"verbs=create;update;delete" if you want to enable deletion validation.
// nolint:lll
// +kubebuilder:webhook:verbs=create;update,path=/validate-crd-cloud-antrea-io-v1alpha1-cloudprovideraccount,mutating=false,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudprovideraccounts,versions=v1alpha1,name=vcloudprovideraccount.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// CPAValidator is used to validate CPA object.
type CPAValidator struct {
	Client  k8sclient.Client
	Log     logr.Logger
	decoder *admission.Decoder
}

// Handle handles validator admission requests for CPA.
func (v *CPAValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	v.Log.V(1).Info("Received CPA admission webhook request", "Name", req.Name, "Operation", req.Operation)
	switch req.Operation {
	case admissionv1.Create:
		return v.validateCreate(req)
	case admissionv1.Update:
		return v.validateUpdate(req)
	case admissionv1.Delete:
		return v.validateDelete(req)
	default:
		return admission.Errored(http.StatusBadRequest, fmt.Errorf(errorMsgInvalidRequest))
	}
}

// InjectDecoder injects the decoder.
func (v *CPAValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// ValidateCreate implements webhook validations for CPA add operation.
func (v *CPAValidator) validateCreate(req admission.Request) admission.Response {
	cpa := &v1alpha1.CloudProviderAccount{}
	err := v.decoder.Decode(req, cpa)
	if err != nil {
		v.Log.Error(err, "Failed to decode CloudProviderAccount", "CPAValidator", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	cloudProviderType, err := utils.GetAccountProviderType(cpa)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	switch cloudProviderType {
	case v1alpha1.AWSCloudProvider:
		if err := v.validateAWSAccount(cpa); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	case v1alpha1.AzureCloudProvider:
		if err := v.validateAzureAccount(cpa); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	if *cpa.Spec.PollIntervalInSeconds < MinPollInterval {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf(errorMsgMinPollInterval))
	}

	return admission.Allowed("")
}

// ValidateUpdate implements webhook validations for CPA update operation.
func (v *CPAValidator) validateUpdate(req admission.Request) admission.Response {
	newCpa := &v1alpha1.CloudProviderAccount{}
	err := v.decoder.Decode(req, newCpa)
	if err != nil {
		v.Log.Error(err, "Failed to decode CloudProviderAccount", "CPAValidator", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}
	oldCpa := &v1alpha1.CloudProviderAccount{}
	if req.OldObject.Raw != nil {
		if err := json.Unmarshal(req.OldObject.Raw, &oldCpa); err != nil {
			v.Log.Error(err, "Failed to decode old CloudProviderAccount", "CPAValidator", req.Name)
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	cloudProviderType, err := utils.GetAccountProviderType(newCpa)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	switch cloudProviderType {
	case v1alpha1.AWSCloudProvider:
		if err := v.validateAWSAccount(newCpa); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	case v1alpha1.AzureCloudProvider:
		if err := v.validateAzureAccount(newCpa); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	if *newCpa.Spec.PollIntervalInSeconds < MinPollInterval {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf(errorMsgMinPollInterval))
	}

	return admission.Allowed("")
}

// ValidateDelete implements webhook validations for CPA delete.
func (v *CPAValidator) validateDelete(req admission.Request) admission.Response { //nolint:unparam
	// TODO(user): fill in your validation logic upon object deletion.
	return admission.Allowed("")
}

// validateAWSAccount validates parameters in CPA AWS account credentials.
func (v *CPAValidator) validateAWSAccount(account *v1alpha1.CloudProviderAccount) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})

	awsConfig := account.Spec.AWSConfig

	err := v.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: awsConfig.SecretRef.Namespace,
		Name:      awsConfig.SecretRef.Name}, u)
	if err != nil {
		return fmt.Errorf("%s: %s", errorMsgSecretNotConfigured, err.Error())
	}
	data := u.Object["data"].(map[string]interface{})
	decode, err := base64.StdEncoding.DecodeString(data[awsConfig.SecretRef.Key].(string))
	if err != nil {
		return fmt.Errorf("%s: %s", errorMsgDecodeFail, err.Error())
	}

	awsCredential := &v1alpha1.AwsAccountCredential{}
	if err = json.Unmarshal(decode, awsCredential); err != nil {
		return fmt.Errorf("%s: %s", errorMsgJsonUnmarshalFail, err.Error())
	}
	// validate roleArn or A
	if len(strings.TrimSpace(awsCredential.RoleArn)) != 0 {
		v.Log.Info("Role ARN configured will be used for cloud-account access")
	} else if len(strings.TrimSpace(awsCredential.AccessKeyID)) == 0 ||
		len(strings.TrimSpace(awsCredential.AccessKeySecret)) == 0 {
		return fmt.Errorf(errorMsgMissingCredential)
	}

	if len(strings.TrimSpace(awsConfig.Region)) == 0 {
		return fmt.Errorf(errorMsgMissingRegion)
	}

	// NOTE: currently only AWS standard partition regions supported (aws-cn, aws-us-gov etc are not
	// supported). As we add support for other partitions, validation needs to be updated.
	regions := endpoints.AwsPartition().Regions()
	_, found := regions[awsConfig.Region]
	if !found {
		var supportedRegions []string
		for key := range regions {
			supportedRegions = append(supportedRegions, key)
		}
		return fmt.Errorf("%v %s [%v]", awsConfig.Region, errorMsgInvalidRegion, supportedRegions)
	}

	return nil
}

// validateAzureAccount validates parameters in CPA Azure account credentials.
func (v *CPAValidator) validateAzureAccount(account *v1alpha1.CloudProviderAccount) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})

	azureConfig := account.Spec.AzureConfig

	err := v.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: azureConfig.SecretRef.Namespace,
		Name:      azureConfig.SecretRef.Name}, u)
	if err != nil {
		return fmt.Errorf("%s: %s", errorMsgSecretNotConfigured, err.Error())
	}
	data := u.Object["data"].(map[string]interface{})
	decode, err := base64.StdEncoding.DecodeString(data[azureConfig.SecretRef.Key].(string))
	if err != nil {
		return fmt.Errorf("%s: %s", errorMsgDecodeFail, err.Error())
	}

	azureCredential := &v1alpha1.AzureAccountCredential{}
	if err = json.Unmarshal(decode, azureCredential); err != nil {
		return fmt.Errorf("%s: %s", errorMsgJsonUnmarshalFail, err.Error())
	}

	// validate subscription ID
	if len(strings.TrimSpace(azureCredential.SubscriptionID)) == 0 {
		return fmt.Errorf(errorMsgMissingSubscritionID)
	}
	// validate tenant ID
	if len(strings.TrimSpace(azureCredential.TenantID)) == 0 {
		return fmt.Errorf(errorMsgMissingTenantID)
	}
	// validate credentials
	if len(strings.TrimSpace(azureCredential.ClientID)) == 0 || len(strings.TrimSpace(azureCredential.ClientKey)) == 0 {
		return fmt.Errorf(errorMsgMissingClientDetails)
	}

	// validate region
	if len(strings.TrimSpace(azureConfig.Region)) == 0 {
		return fmt.Errorf(errorMsgMissingRegion)
	}

	return nil
}
