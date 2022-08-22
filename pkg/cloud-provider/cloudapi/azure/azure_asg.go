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

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/go-autorest/autorest"

	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

// applicationSecurityGroups returns application-security-groups apiClient.
func (p *azureServiceSdkConfigProvider) applicationSecurityGroups(subscriptionID string) (azureAsgWrapper, error) {
	applicationSecurityGroupsClient := network.NewApplicationSecurityGroupsClient(subscriptionID)
	applicationSecurityGroupsClient.Authorizer = p.authorizer
	return &azureAsgWrapperImpl{asgAPIClient: applicationSecurityGroupsClient}, nil
}

func createOrGetApplicationSecurityGroup(asgAPIClient azureAsgWrapper, location string, rgName string,
	cloudAsgName string) (string, error) {
	asg, err := asgAPIClient.get(context.Background(), rgName, cloudAsgName)
	if err != nil {
		detailError := err.(autorest.DetailedError)
		if detailError.StatusCode != 404 {
			return "", err
		}
	}

	if asg.ID == nil {
		appSecurityGroupParams := network.ApplicationSecurityGroup{
			Location: &location,
		}
		asg, err = asgAPIClient.createOrUpdate(context.Background(), rgName,
			cloudAsgName, appSecurityGroupParams)
		if err != nil {
			return "", err
		}
	}

	return strings.ToLower(*asg.ID), nil
}

func getNepheControllerCreatedAsgByNameForResourceGroup(asgAPIClient azureAsgWrapper,
	rgName string) (map[string]network.ApplicationSecurityGroup, map[string]network.ApplicationSecurityGroup, error) {
	applicationSecurityGroups, err := asgAPIClient.listComplete(context.Background(), rgName)
	if err != nil {
		return nil, nil, err
	}

	atAsgByNepheControllerName := make(map[string]network.ApplicationSecurityGroup)
	agAsgByNepheControllerName := make(map[string]network.ApplicationSecurityGroup)
	for _, applicationSecurityGroup := range applicationSecurityGroups {
		asgName := applicationSecurityGroup.Name
		if asgName == nil {
			continue
		}
		sgName, isNepheControllerCreatedAG, isNepheControllerCreatedAT := securitygroup.IsNepheControllerCreatedSG(*asgName)
		if isNepheControllerCreatedAT {
			atAsgByNepheControllerName[strings.ToLower(sgName)] = applicationSecurityGroup
		} else if isNepheControllerCreatedAG {
			agAsgByNepheControllerName[strings.ToLower(sgName)] = applicationSecurityGroup
		}
	}

	return agAsgByNepheControllerName, atAsgByNepheControllerName, nil
}
