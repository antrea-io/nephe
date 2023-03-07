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

	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

// applicationSecurityGroups returns application-security-groups apiClient.
func (p *azureServiceSdkConfigProvider) applicationSecurityGroups(subscriptionID string) (azureAsgWrapper, error) {
	applicationSecurityGroupsClient, err := armnetwork.NewApplicationSecurityGroupsClient(subscriptionID, p.cred, nil)
	if err != nil {
		return nil, err
	}
	return &azureAsgWrapperImpl{asgAPIClient: *applicationSecurityGroupsClient}, nil
}

func createOrGetApplicationSecurityGroup(asgAPIClient azureAsgWrapper, location string, rgName string,
	cloudAsgName string) (string, error) {
	var respErr *azcore.ResponseError
	asg, err := asgAPIClient.get(context.Background(), rgName, cloudAsgName)
	if err != nil {
		if errors.As(err, &respErr) {
			if respErr.StatusCode != http.StatusNotFound {
				return "", err
			}
		}
	}

	if asg.ID == nil {
		appSecurityGroupParams := armnetwork.ApplicationSecurityGroup{
			Location: &location,
		}
		asg, err = asgAPIClient.createOrUpdate(context.Background(), rgName, cloudAsgName, appSecurityGroupParams)
		if err != nil {
			return "", err
		}
	}

	return strings.ToLower(*asg.ID), nil
}

// getNepheControllerCreatedAsgByNameForResourceGroup returns AT and AG ASGs from a resource group.
func getNepheControllerCreatedAsgByNameForResourceGroup(asgAPIClient azureAsgWrapper,
	rgName string) (map[string]armnetwork.ApplicationSecurityGroup, map[string]armnetwork.ApplicationSecurityGroup, error) {
	applicationSecurityGroups, err := asgAPIClient.listComplete(context.Background(), rgName)
	if err != nil {
		return nil, nil, err
	}

	atAsgByNepheControllerName := make(map[string]armnetwork.ApplicationSecurityGroup)
	agAsgByNepheControllerName := make(map[string]armnetwork.ApplicationSecurityGroup)
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
