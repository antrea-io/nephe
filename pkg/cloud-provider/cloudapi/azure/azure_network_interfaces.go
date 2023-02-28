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

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/go-autorest/autorest/to"

	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

// networkInterfaces returns network interfaces SDK api client.
func (p *azureServiceSdkConfigProvider) networkInterfaces(subscriptionID string) (azureNwIntfWrapper, error) {
	interfacesClient, _ := armnetwork.NewInterfacesClient(subscriptionID, p.cred, nil)
	return &azureNwIntfWrapperImpl{nwIntfAPIClient: *interfacesClient}, nil
}

// updateNetworkInterfaceAsg updates network interface on cloud with the new set of ASGs.
func updateNetworkInterfaceAsg(nwIntfAPIClient azureNwIntfWrapper, nwIntfObj armnetwork.Interface,
	asgObjToAttachOrDetach armnetwork.ApplicationSecurityGroup, isAttach bool) error {
	if nwIntfObj.ID == nil {
		return fmt.Errorf("network interface object is empty")
	}

	_, rgName, resName, _ := extractFieldsFromAzureResourceID(*nwIntfObj.ID)

	if nwIntfObj.Properties == nil {
		return fmt.Errorf("network interface Properties is empty, cannot update network interface with asg")
	}
	ipConfigurations := getAsgUpdatedIPConfigurations(&nwIntfObj, asgObjToAttachOrDetach, isAttach)
	nwIntfObj.Properties.IPConfigurations = ipConfigurations

	_, err := nwIntfAPIClient.createOrUpdate(context.Background(), rgName, resName, nwIntfObj)
	azurePluginLogger().Info("updated network-interface", "ID", *nwIntfObj.ID, "err", err)
	return err
}

// updateNetworkInterfaceNsg updates network interface on cloud with new set of NSGs.
func updateNetworkInterfaceNsg(nwIntfAPIClient azureNwIntfWrapper, nwIntfObj armnetwork.Interface,
	nsgObjToAttachOrDetach armnetwork.SecurityGroup, asgObjToAttachOrDetach armnetwork.ApplicationSecurityGroup,
	isAttach bool, tagKey string) error {
	if nwIntfObj.ID == nil {
		return fmt.Errorf("network interface object is empty")
	}

	if nwIntfObj.Properties == nil {
		return fmt.Errorf("network interface Properties is empty,  cannot update network interface with nsg")
	}

	_, rgName, resName, _ := extractFieldsFromAzureResourceID(*nwIntfObj.ID)

	nsg, tags := getUpdatedNetworkInterfaceNsgAndTags(&nwIntfObj, nsgObjToAttachOrDetach, isAttach, tagKey)
	ipConfigurations := getAsgUpdatedIPConfigurations(&nwIntfObj, asgObjToAttachOrDetach, isAttach)

	nwIntfObj.Properties.IPConfigurations = ipConfigurations
	nwIntfObj.Properties.NetworkSecurityGroup = nsg
	nwIntfObj.Tags = tags
	_, err := nwIntfAPIClient.createOrUpdate(context.Background(), rgName, resName, nwIntfObj)
	azurePluginLogger().Info("updated network-interface", "ID", *nwIntfObj.ID, "err", err)

	return err
}

// getNetworkInterfacesGivenIDs returns the network interface objects from cloud matching the interface IDS in nwIntfIDSet.
func getNetworkInterfacesGivenIDs(nwIntfAPIClient azureNwIntfWrapper, nwIntfIDSet map[string]struct{}) (map[string]armnetwork.Interface,
	error) {
	nwIntfObjs, err := nwIntfAPIClient.listAllComplete(context.Background())
	if err != nil {
		return nil, err
	}
	nwIntfIDToObj := make(map[string]armnetwork.Interface)
	for _, nwIntfObj := range nwIntfObjs {
		nwIntfIDLowerCase := strings.ToLower(*nwIntfObj.ID)
		if _, found := nwIntfIDSet[nwIntfIDLowerCase]; found {
			nwIntfIDToObj[nwIntfIDLowerCase] = nwIntfObj
		}
	}

	return nwIntfIDToObj, nil
}

// getAsgUpdatedIPConfigurations adds/deletes the ASG from the list of ASGs attached to an interface object.
func getAsgUpdatedIPConfigurations(nwIntfObj *armnetwork.Interface, asgObjToAttachOrDetach armnetwork.ApplicationSecurityGroup,
	isAttach bool) []*armnetwork.InterfaceIPConfiguration {
	var currentNwIntfAsgs []armnetwork.ApplicationSecurityGroup
	ipConfigurations := nwIntfObj.Properties.IPConfigurations

	for _, ipConfiguration := range ipConfigurations {
		if ipConfiguration.Properties == nil {
			continue
		}
		if !*ipConfiguration.Properties.Primary {
			continue
		}
		if len(ipConfiguration.Properties.ApplicationSecurityGroups) > 0 {
			for _, asg := range ipConfiguration.Properties.ApplicationSecurityGroups {
				currentNwIntfAsgs = append(currentNwIntfAsgs, *asg)
			}
		}
		break
	}

	var newNwIntfAsgs []*armnetwork.ApplicationSecurityGroup
	if isAttach {
		for ind := range currentNwIntfAsgs {
			newNwIntfAsgs = append(newNwIntfAsgs, &currentNwIntfAsgs[ind])
		}
		newNwIntfAsgs = append(newNwIntfAsgs, &asgObjToAttachOrDetach)
	} else {
		asgToDetachIDLowercase := strings.ToLower(*asgObjToAttachOrDetach.ID)
		for ind := range currentNwIntfAsgs {
			if strings.Compare(asgToDetachIDLowercase, strings.ToLower(*currentNwIntfAsgs[ind].ID)) == 0 {
				continue
			}
			newNwIntfAsgs = append(newNwIntfAsgs, &currentNwIntfAsgs[ind])
		}
	}

	for _, ipConfigurations := range ipConfigurations {
		if ipConfigurations.Properties == nil {
			continue
		}
		ipConfigurations.Properties.ApplicationSecurityGroups = newNwIntfAsgs
	}
	return ipConfigurations
}

// getUpdatedNetworkInterfaceNsgAndTags adds/deletes NSG from network interface object bssed on isAttach parameter.
func getUpdatedNetworkInterfaceNsgAndTags(nwIntfObj *armnetwork.Interface, nsgObjToAttachOrDetach armnetwork.SecurityGroup,
	isAttach bool, tagKey string) (*armnetwork.SecurityGroup, map[string]*string) {
	currentTags := nwIntfObj.Tags

	if isAttach {
		nwIntfObj.Properties.NetworkSecurityGroup = &nsgObjToAttachOrDetach
		if currentTags == nil {
			currentTags = make(map[string]*string)
		}
		currentTags[tagKey] = to.StringPtr("true")
	} else {
		delete(currentTags, tagKey)
		if !hasAnyNepheControllerSecurityGroupTags(currentTags) {
			nwIntfObj.Properties.NetworkSecurityGroup = nil
		}
	}

	return nwIntfObj.Properties.NetworkSecurityGroup, currentTags
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
