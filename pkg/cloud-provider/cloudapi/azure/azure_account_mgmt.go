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

package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
)

type azureAccountConfig struct {
	v1alpha1.AzureAccountCredential
	region string
}

// setAccountCredentials sets account credentials.
func setAccountCredentials(client client.Client, credentials interface{}) (interface{}, error) {
	azureProviderConfig := credentials.(*v1alpha1.CloudProviderAccountAzureConfig)
	accCred, err := extractSecret(client, azureProviderConfig.SecretRef)
	if err != nil {
		return nil, err
	}

	azureConfig := &azureAccountConfig{
		AzureAccountCredential: *accCred,
		region:                 strings.TrimSpace(azureProviderConfig.Region),
	}

	return azureConfig, nil
}

func compareAccountCredentials(accountName string, existing interface{}, new interface{}) bool {
	existingConfig := existing.(*azureAccountConfig)
	newConfig := new.(*azureAccountConfig)

	credsChanged := false
	if strings.Compare(existingConfig.SubscriptionID, newConfig.SubscriptionID) != 0 {
		credsChanged = true
		azurePluginLogger().Info("subscription ID updated", "account", accountName)
	}
	if strings.Compare(existingConfig.ClientID, newConfig.ClientID) != 0 {
		credsChanged = true
		azurePluginLogger().Info("client ID updated", "account", accountName)
	}
	if strings.Compare(existingConfig.TenantID, newConfig.TenantID) != 0 {
		credsChanged = true
		azurePluginLogger().Info("account tenant ID updated", "account", accountName)
	}
	if strings.Compare(existingConfig.ClientKey, newConfig.ClientKey) != 0 {
		credsChanged = true
		azurePluginLogger().Info("account client key updated", "account", accountName)
	}
	if strings.Compare(existingConfig.region, newConfig.region) != 0 {
		credsChanged = true
		azurePluginLogger().Info("account region updated", "account", accountName)
	}
	return credsChanged
}

// extractSecret extracts credentials from a Kubernetes secret.
func extractSecret(c client.Client, s *v1alpha1.SecretReference) (*v1alpha1.AzureAccountCredential, error) {
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

	cred := &v1alpha1.AzureAccountCredential{}
	if err = json.Unmarshal(decode, cred); err != nil {
		return nil, err
	}

	return cred, nil
}

// getVnetAccount returns first found account config to which this vnet id belongs.
func (c *azureCloud) getVnetAccount(vpcID string) internal.CloudAccountInterface {
	accCfgs := c.cloudCommon.GetCloudAccounts()
	if len(accCfgs) == 0 {
		return nil
	}

	for _, accCfg := range accCfgs {
		ec2ServiceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
		if err != nil {
			continue
		}
		accVpcIDs := ec2ServiceCfg.(*computeServiceConfig).getCachedVnetIDs()
		if len(accVpcIDs) == 0 {
			continue
		}
		if _, found := accVpcIDs[strings.ToLower(vpcID)]; found {
			return accCfg
		}
	}
	return nil
}
