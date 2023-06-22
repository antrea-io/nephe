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

package aws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	"antrea.io/nephe/pkg/util"
)

type awsAccountConfig struct {
	crdv1alpha1.AwsAccountCredential
	region   string
	endpoint string
}

// setAccountCredentials sets account credentials.
func setAccountCredentials(client client.Client, credentials interface{}) (interface{}, error) {
	awsProviderConfig := credentials.(*crdv1alpha1.CloudProviderAccountAWSConfig)
	awsConfig := &awsAccountConfig{
		region:   strings.TrimSpace(awsProviderConfig.Region[0]),
		endpoint: strings.TrimSpace(awsProviderConfig.Endpoint),
	}
	accCred, err := extractSecret(client, awsProviderConfig.SecretRef)
	if err != nil {
		accCred.AccessKeyID = internal.AccountCredentialsDefault
		accCred.AccessKeySecret = internal.AccountCredentialsDefault
		accCred.SessionToken = internal.AccountCredentialsDefault
		accCred.RoleArn = internal.AccountCredentialsDefault
		accCred.ExternalID = internal.AccountCredentialsDefault
	}

	// As only single region is supported right now, use 0th index in awsProviderConfig.Region as the configured region.
	awsConfig.AwsAccountCredential = *accCred
	return awsConfig, err
}

func compareAccountCredentials(accountName string, existing interface{}, new interface{}) bool {
	existingConfig := existing.(*awsAccountConfig)
	newConfig := new.(*awsAccountConfig)

	credsChanged := false
	if strings.Compare(existingConfig.AccessKeyID, newConfig.AccessKeyID) != 0 {
		credsChanged = true
		awsPluginLogger().Info("Account access key ID updated", "account", accountName)
	}
	if strings.Compare(existingConfig.AccessKeySecret, newConfig.AccessKeySecret) != 0 {
		credsChanged = true
		awsPluginLogger().Info("Account access key secret updated", "account", accountName)
	}
	if strings.Compare(existingConfig.SessionToken, newConfig.SessionToken) != 0 {
		credsChanged = true
		awsPluginLogger().Info("Account session token updated", "account", accountName)
	}
	if strings.Compare(existingConfig.RoleArn, newConfig.RoleArn) != 0 {
		credsChanged = true
		awsPluginLogger().Info("Account IAM role updated", "account", accountName)
	}
	if strings.Compare(existingConfig.ExternalID, newConfig.ExternalID) != 0 {
		credsChanged = true
		awsPluginLogger().Info("Account IAM external id updated", "account", accountName)
	}
	if strings.Compare(existingConfig.region, newConfig.region) != 0 {
		credsChanged = true
		awsPluginLogger().Info("Account region updated", "account", accountName)
	}
	if strings.Compare(existingConfig.endpoint, newConfig.endpoint) != 0 {
		credsChanged = true
		awsPluginLogger().Info("Endpoint url updated", "account", accountName)
	}
	return credsChanged
}

// extractSecret extracts credentials from a Kubernetes secret.
func extractSecret(c client.Client, s *crdv1alpha1.SecretReference) (*crdv1alpha1.AwsAccountCredential, error) {
	cred := &crdv1alpha1.AwsAccountCredential{}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: s.Namespace, Name: s.Name}, u); err != nil {
		return cred, fmt.Errorf("%v, failed to get Secret object: %v/%v", util.ErrorMsgSecretReference, s.Namespace, s.Name)
	}

	data, ok := u.Object["data"].(map[string]interface{})
	if !ok {
		return cred, fmt.Errorf("%v, failed to get Secret data: %v/%v", util.ErrorMsgSecretReference, s.Namespace, s.Name)
	}

	key, ok := data[s.Key].(string)
	if !ok {
		return cred, fmt.Errorf("%v, failed to get Secret key: %v/%v, key: %v", util.ErrorMsgSecretReference, s.Namespace, s.Name, s.Key)
	}

	decode, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return cred, fmt.Errorf("%v, failed to decode Secret key: %v/%v", util.ErrorMsgSecretReference, s.Namespace, s.Name)
	}

	if err = json.Unmarshal(decode, cred); err != nil {
		return cred, fmt.Errorf("error unmarshalling credentials: %v/%v", s.Namespace, s.Name)
	}

	if (cred.AccessKeyID == "" || cred.AccessKeySecret == "") && cred.RoleArn == "" {
		return cred, fmt.Errorf("%v, Secret credentials cannot be empty: %v/%v", util.ErrorMsgSecretReference, s.Namespace, s.Name)
	}

	return cred, nil
}
