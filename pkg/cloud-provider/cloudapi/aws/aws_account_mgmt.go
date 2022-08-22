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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
)

type awsAccountConfig struct {
	v1alpha1.AwsAccountCredential
	region string
}

// setAccountCredentials sets account credentials.
func setAccountCredentials(client client.Client, credentials interface{}) (interface{}, error) {
	awsProviderConfig := credentials.(*v1alpha1.CloudProviderAccountAWSConfig)
	accCred, err := extractSecret(client, awsProviderConfig.SecretRef)
	if err != nil {
		return nil, err
	}

	awsConfig := &awsAccountConfig{
		AwsAccountCredential: *accCred,
		region:               strings.TrimSpace(awsProviderConfig.Region),
	}

	return awsConfig, nil
}

func compareAccountCredentials(accountName string, existing interface{}, new interface{}) bool {
	existingConfig := existing.(*awsAccountConfig)
	newConfig := new.(*awsAccountConfig)

	credsChanged := false
	if strings.Compare(existingConfig.AccessKeyID, newConfig.AccessKeyID) != 0 {
		credsChanged = true
		awsPluginLogger().Info("account access key ID updated", "account", accountName)
	}
	if strings.Compare(existingConfig.AccessKeySecret, newConfig.AccessKeySecret) != 0 {
		credsChanged = true
		awsPluginLogger().Info("account access key secret updated", "account", accountName)
	}
	if strings.Compare(existingConfig.region, newConfig.region) != 0 {
		credsChanged = true
		awsPluginLogger().Info("account region updated", "account", accountName)
	}
	return credsChanged
}

// extractSecret extracts credentials from a Kubernetes secret.
func extractSecret(c client.Client, s *v1alpha1.SecretReference) (*v1alpha1.AwsAccountCredential, error) {
	if s == nil {
		return nil, fmt.Errorf("secret reference not found")
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Secret",
		Version: "v1",
	})
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: s.Namespace, Name: s.Name}, u); err != nil {
		return nil, err
	}

	data := u.Object["data"].(map[string]interface{})
	decode, err := base64.StdEncoding.DecodeString(data[s.Key].(string))
	if err != nil {
		return nil, err
	}

	cred := &v1alpha1.AwsAccountCredential{}
	if err = json.Unmarshal(decode, cred); err != nil {
		return nil, err
	}

	return cred, nil
}

// getVpcAccount returns first found account config to which this vpc id belongs.
func (c *awsCloud) getVpcAccount(vpcID string) internal.CloudAccountInterface {
	accCfgs := c.cloudCommon.GetCloudAccounts()
	if len(accCfgs) == 0 {
		return nil
	}

	for _, accCfg := range accCfgs {
		ec2ServiceCfg, err := accCfg.GetServiceConfigByName(awsComputeServiceNameEC2)
		if err != nil {
			awsPluginLogger().Error(err, "get ec2 service config failed", "vpcID", vpcID, "account", accCfg.GetNamespacedName())
			continue
		}
		accVpcIDs := ec2ServiceCfg.(*ec2ServiceConfig).getCachedVpcIDs()
		if len(accVpcIDs) == 0 {
			awsPluginLogger().Info("no vpc found for account", "vpcID", vpcID, "account", accCfg.GetNamespacedName())
			continue
		}
		if _, found := accVpcIDs[strings.ToLower(vpcID)]; found {
			return accCfg
		}
		awsPluginLogger().Info("vpcID not found in cache", "vpcID", vpcID, "account", accCfg.GetNamespacedName())
	}
	return nil
}
