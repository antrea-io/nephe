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
	"errors"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
)

// securityGroups returns security-groups apiClient.
func (p *azureServiceSdkConfigProvider) securityGroups(subscriptionID string) (azureNsgWrapper, error) {
	securityGroupsClient, err := armnetwork.NewSecurityGroupsClient(subscriptionID, p.cred, nil)
	if err != nil {
		return nil, err
	}
	return &azureNsgWrapperImpl{nsgAPIClient: *securityGroupsClient}, nil
}

func createOrGetNetworkSecurityGroup(nsgAPIClient azureNsgWrapper, location string, rgName string,
	cloudSgName string) (string, error) {
	var respErr *azcore.ResponseError
	nsg, err := nsgAPIClient.get(context.Background(), rgName, cloudSgName, "")
	if err != nil {
		if errors.As(err, &respErr) {
			if respErr.StatusCode != http.StatusNotFound {
				return "", err
			}
		}
	}

	if nsg.ID == nil {
		securityGroupParams := armnetwork.SecurityGroup{
			Location: &location,
		}
		nsg, err = nsgAPIClient.createOrUpdate(context.Background(), rgName, cloudSgName, securityGroupParams)
		if err != nil {
			return "", err
		}
	}

	return strings.ToLower(*nsg.ID), nil
}

func updateNetworkSecurityGroupRules(nsgAPIClient azureNsgWrapper, location string, rgName string, cloudSgName string,
	rules []*armnetwork.SecurityRule) error {
	securityGroupParams := armnetwork.SecurityGroup{
		Properties: &armnetwork.SecurityGroupPropertiesFormat{
			SecurityRules: rules,
		},
		Name:     &cloudSgName,
		Location: &location,
	}
	_, err := nsgAPIClient.createOrUpdate(context.Background(), rgName, cloudSgName, securityGroupParams)
	if err != nil {
		return err
	}

	return nil
}
