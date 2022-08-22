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
	"strings"

	"github.com/Azure/go-autorest/autorest"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
)

// securityGroups returns security-groups apiClient.
func (p *azureServiceSdkConfigProvider) securityGroups(subscriptionID string) (azureNsgWrapper, error) {
	securityGroupsClient := network.NewSecurityGroupsClient(subscriptionID)
	securityGroupsClient.Authorizer = p.authorizer
	return &azureNsgWrapperImpl{nsgAPIClient: securityGroupsClient}, nil
}

func createOrGetNetworkSecurityGroup(nsgAPIClient azureNsgWrapper, location string, rgName string,
	cloudSgName string) (string, error) {
	nsg, err := nsgAPIClient.get(context.Background(), rgName, cloudSgName, "")
	if err != nil {
		detailError := err.(autorest.DetailedError)
		if detailError.StatusCode != 404 {
			return "", err
		}
	}

	if nsg.ID == nil {
		securityGroupParams := network.SecurityGroup{
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
	rules []network.SecurityRule) error {
	securityGroupParams := network.SecurityGroup{
		SecurityGroupPropertiesFormat: &network.SecurityGroupPropertiesFormat{
			SecurityRules: &rules,
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
