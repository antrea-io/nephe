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
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/go-autorest/autorest/to"

	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

// networkInterfaces returns network interfaces SDK api client.
func (p *azureServiceSdkConfigProvider) networkInterfaces(subscriptionID string) (azureNwIntfWrapper, error) {
	interfacesClient := network.NewInterfacesClient(subscriptionID)
	interfacesClient.Authorizer = p.authorizer
	return &azureNwIntfWrapperImpl{nwIntfAPIClient: interfacesClient}, nil
}

func updateNetworkInterfaceAsg(nwIntfAPIClient azureNwIntfWrapper, nwIntfObj network.Interface,
	asgObjToAttachOrDetach network.ApplicationSecurityGroup, isAttach bool) error {
	if nwIntfObj.ID == nil {
		return fmt.Errorf("network interface object is empty")
	}

	_, rgName, resName, _ := extractFieldsFromAzureResourceID(*nwIntfObj.ID)

	ipConfigurations := getAsgUpdatedIPConfigurations(&nwIntfObj, asgObjToAttachOrDetach, isAttach)
	nwIntfObj.IPConfigurations = ipConfigurations

	_, err := nwIntfAPIClient.createOrUpdate(context.Background(), rgName, resName, nwIntfObj)
	azurePluginLogger().Info("updated network-interface", "ID", *nwIntfObj.ID, "err", err)
	return err
}

func updateNetworkInterfaceNsg(nwIntfAPIClient azureNwIntfWrapper, nwIntfObj network.Interface,
	nsgObjToAttachOrDetach network.SecurityGroup, asgObjToAttachOrDetach network.ApplicationSecurityGroup,
	isAttach bool, tagKey string) error {
	if nwIntfObj.ID == nil {
		return fmt.Errorf("network interface object is empty")
	}

	_, rgName, resName, _ := extractFieldsFromAzureResourceID(*nwIntfObj.ID)

	nsg, tags := getUpdatedNetworkInterfaceNsgAndTags(&nwIntfObj, nsgObjToAttachOrDetach, isAttach, tagKey)
	ipConfigurations := getAsgUpdatedIPConfigurations(&nwIntfObj, asgObjToAttachOrDetach, isAttach)

	nwIntfObj.IPConfigurations = ipConfigurations
	nwIntfObj.NetworkSecurityGroup = nsg
	nwIntfObj.Tags = tags
	_, err := nwIntfAPIClient.createOrUpdate(context.Background(), rgName, resName, nwIntfObj)
	azurePluginLogger().Info("updated network-interface", "ID", *nwIntfObj.ID, "err", err)

	return err
}

func getNetworkInterfacesGivenIDs(nwIntfAPIClient azureNwIntfWrapper, nwIntfIDSet map[string]struct{}) (map[string]network.Interface,
	error) {
	nwIntfObjs, err := nwIntfAPIClient.listAllComplete(context.Background())
	if err != nil {
		return nil, err
	}
	nwIntfIDToObj := make(map[string]network.Interface)
	for _, nwIntfObj := range nwIntfObjs {
		nwIntfIDLowerCase := strings.ToLower(*nwIntfObj.ID)
		if _, found := nwIntfIDSet[nwIntfIDLowerCase]; found {
			nwIntfIDToObj[nwIntfIDLowerCase] = nwIntfObj
		}
	}

	return nwIntfIDToObj, nil
}

func getAsgUpdatedIPConfigurations(nwIntfObj *network.Interface, asgObjToAttachOrDetach network.ApplicationSecurityGroup,
	isAttach bool) *[]network.InterfaceIPConfiguration {
	var currentNwIntfAsgs []network.ApplicationSecurityGroup
	ipConfigurations := nwIntfObj.IPConfigurations
	for _, ipConfiguration := range *ipConfigurations {
		if !*ipConfiguration.Primary {
			continue
		}
		if ipConfiguration.ApplicationSecurityGroups != nil {
			currentNwIntfAsgs = append(currentNwIntfAsgs, *ipConfiguration.ApplicationSecurityGroups...)
		}
		break
	}

	var newNwIntfAsgs []network.ApplicationSecurityGroup
	if isAttach {
		newNwIntfAsgs = append(newNwIntfAsgs, currentNwIntfAsgs...)
		newNwIntfAsgs = append(newNwIntfAsgs, asgObjToAttachOrDetach)
	} else {
		asgToDetachIDLowercase := strings.ToLower(*asgObjToAttachOrDetach.ID)
		for _, currentNwIntfAsg := range currentNwIntfAsgs {
			if strings.Compare(asgToDetachIDLowercase, strings.ToLower(*currentNwIntfAsg.ID)) == 0 {
				continue
			}
			newNwIntfAsgs = append(newNwIntfAsgs, currentNwIntfAsg)
		}
	}

	for _, ipConfigurations := range *ipConfigurations {
		ipConfigurations.ApplicationSecurityGroups = &newNwIntfAsgs
	}
	return ipConfigurations
}

func getUpdatedNetworkInterfaceNsgAndTags(nwIntfObj *network.Interface, nsgObjToAttachOrDetach network.SecurityGroup, isAttach bool,
	tagKey string) (*network.SecurityGroup, map[string]*string) {
	currentTags := nwIntfObj.Tags
	if isAttach {
		nwIntfObj.NetworkSecurityGroup = &nsgObjToAttachOrDetach
		if currentTags == nil {
			currentTags = make(map[string]*string)
		}
		currentTags[tagKey] = to.StringPtr("true")
	} else {
		delete(currentTags, tagKey)
		if !hasAnyNepheControllerSecurityGroupTags(currentTags) {
			nwIntfObj.NetworkSecurityGroup = nil
		}
	}

	return nwIntfObj.NetworkSecurityGroup, currentTags
}

func hasAnyNepheControllerSecurityGroupTags(tags map[string]*string) bool {
	for key := range tags {
		_, _, isATSG := securitygroup.IsNepheControllerCreatedSG(key)
		if isATSG {
			return true
		}
	}
	return false
}
