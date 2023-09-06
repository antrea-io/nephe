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
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	"antrea.io/nephe/pkg/cloudprovider/utils"
	"antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/util/k8s/tags"
)

var azureStateMap = map[string]runtimev1alpha1.VMState{
	"PowerState/running":      runtimev1alpha1.Running,
	"PowerState/deallocated":  runtimev1alpha1.Stopped,
	"PowerState/deallocating": runtimev1alpha1.ShuttingDown,
	"PowerState/stopping":     runtimev1alpha1.Stopping,
	"PowerState/stopped":      runtimev1alpha1.Stopped,
	"PowerState/starting":     runtimev1alpha1.Starting,
	"PowerState/unknown":      runtimev1alpha1.Unknown,
}

// computeInstanceToInternalVirtualMachineObject converts compute instance to VirtualMachine runtime object.
func computeInstanceToInternalVirtualMachineObject(instance *virtualMachineTable,
	vnets map[string]armnetwork.VirtualNetwork, selectorNamespacedName *types.NamespacedName, accountNamespacedName *types.NamespacedName,
	region string) *runtimev1alpha1.VirtualMachine {
	vmTags := make(map[string]string)
	for key, value := range instance.Tags {
		vmTags[key] = *value
	}
	importedTags := tags.ImportTags(vmTags)

	// Network interfaces associated with Virtual machine
	instNetworkInterfaces := instance.NetworkInterfaces
	networkInterfaces := make([]runtimev1alpha1.NetworkInterface, 0, len(instNetworkInterfaces))
	for _, nwInf := range instNetworkInterfaces {
		var ipAddressObjs []runtimev1alpha1.IPAddress
		if len(nwInf.PrivateIps) > 0 {
			for _, ipAddress := range nwInf.PrivateIps {
				ipAddresObj := runtimev1alpha1.IPAddress{
					AddressType: runtimev1alpha1.AddressTypeInternalIP,
					Address:     *ipAddress,
				}
				ipAddressObjs = append(ipAddressObjs, ipAddresObj)
			}
		}
		if len(nwInf.PublicIps) > 0 {
			for _, publicIP := range nwInf.PublicIps {
				ipAddressObj := runtimev1alpha1.IPAddress{
					AddressType: runtimev1alpha1.AddressTypeExternalIP,
					Address:     *publicIP,
				}
				ipAddressObjs = append(ipAddressObjs, ipAddressObj)
			}
		}
		macAddress := ""
		if nwInf.MacAddress != nil {
			macAddress = *nwInf.MacAddress
		}

		var nsgIds []string
		if nwInf.NsgID != nil && *nwInf.NsgID != "" {
			nsgIds = append(nsgIds, *nwInf.NsgID)
		}

		var asgIDs []string
		for _, asgID := range nwInf.ApplicationSecurityGroupIDs {
			if asgID != nil && *asgID != "" {
				asgIDs = append(asgIDs, *asgID)
			}
		}

		networkInterface := runtimev1alpha1.NetworkInterface{
			Name:             *nwInf.ID,
			MAC:              macAddress,
			IPs:              ipAddressObjs,
			SecurityGroupIds: nsgIds,
			GroupIds:         asgIDs,
		}
		networkInterfaces = append(networkInterfaces, networkInterface)
	}

	cloudNetworkID := strings.ToLower(*instance.VnetID)
	cloudID := strings.ToLower(*instance.ID)
	cloudName := strings.ToLower(*instance.Name)
	crdName := utils.GenerateShortResourceIdentifier(cloudID, cloudName)
	var vmUid string
	if instance.Properties != nil && instance.Properties.VMID != nil {
		vmUid = strings.ToLower(*instance.Properties.VMID)
	}

	var vnetUid string
	if value, found := vnets[cloudNetworkID]; found {
		if value.Properties != nil && value.Properties.ResourceGUID != nil {
			vnetUid = strings.ToLower(*value.Properties.ResourceGUID)
		}
	}
	_, _, nwResName, err := extractFieldsFromAzureResourceID(cloudNetworkID)
	if err != nil {
		azurePluginLogger().Error(err, "failed to create VirtualMachine object")
		return nil
	}
	cloudNetworkShortID := utils.GenerateShortResourceIdentifier(cloudNetworkID, nwResName)
	var state runtimev1alpha1.VMState
	if instance.Status != nil {
		state = azureStateMap[*instance.Status]
	} else {
		state = runtimev1alpha1.Unknown
	}

	vmStatus := &runtimev1alpha1.VirtualMachineStatus{
		Provider:          runtimev1alpha1.AzureCloudProvider,
		Tags:              importedTags,
		State:             state,
		NetworkInterfaces: networkInterfaces,
		Region:            strings.ToLower(region),
		Agented:           false,
		CloudId:           strings.ToLower(cloudID),
		CloudName:         strings.ToLower(cloudName),
		CloudVpcId:        strings.ToLower(cloudNetworkID),
		CloudVpcName:      nwResName,
	}

	labelsMap := map[string]string{
		labels.CloudAccountName:       accountNamespacedName.Name,
		labels.CloudAccountNamespace:  accountNamespacedName.Namespace,
		labels.CloudSelectorName:      selectorNamespacedName.Name,
		labels.CloudSelectorNamespace: selectorNamespacedName.Namespace,
		labels.VpcName:                cloudNetworkShortID,
		labels.CloudVmUID:             strings.ToLower(vmUid),
		labels.CloudVpcUID:            strings.ToLower(vnetUid),
	}

	vmObj := &runtimev1alpha1.VirtualMachine{
		TypeMeta: v1.TypeMeta{
			Kind:       internal.VirtualMachineRuntimeObjectKind,
			APIVersion: internal.RuntimeAPIVersion,
		},
		ObjectMeta: v1.ObjectMeta{
			UID:       uuid.NewUUID(),
			Name:      crdName,
			Namespace: selectorNamespacedName.Namespace,
			Labels:    labelsMap,
		},
		Status: *vmStatus,
	}

	return vmObj
}

// ComputeVpcToInternalVpcObject converts vnet object from cloud format(network.VirtualNetwork) to vpc runtime object.
func ComputeVpcToInternalVpcObject(vnet *armnetwork.VirtualNetwork, accountNamespace, accountName,
	region string, managed bool) *runtimev1alpha1.Vpc {
	crdName := utils.GenerateShortResourceIdentifier(*vnet.ID, *vnet.Name)
	tags := make(map[string]string, 0)
	if len(vnet.Tags) != 0 {
		for k, v := range vnet.Tags {
			tags[k] = *v
		}
	}
	cidrs := make([]string, 0)
	var uid string
	properties := vnet.Properties
	if properties != nil {
		if properties.AddressSpace != nil && len(properties.AddressSpace.AddressPrefixes) > 0 {
			for _, cidr := range vnet.Properties.AddressSpace.AddressPrefixes {
				cidrs = append(cidrs, *cidr)
			}
		}
		if properties.ResourceGUID != nil {
			uid = strings.ToLower(*properties.ResourceGUID)
		}
	}

	status := &runtimev1alpha1.VpcStatus{
		CloudName: strings.ToLower(*vnet.Name),
		CloudId:   strings.ToLower(*vnet.ID),
		Provider:  runtimev1alpha1.AzureCloudProvider,
		Region:    region,
		Tags:      tags,
		Cidrs:     cidrs,
		Managed:   managed,
	}

	labelsMap := map[string]string{
		labels.CloudAccountNamespace: accountNamespace,
		labels.CloudAccountName:      accountName,
		labels.CloudVpcUID:           uid,
	}

	vpcObj := &runtimev1alpha1.Vpc{
		ObjectMeta: v1.ObjectMeta{
			Name:      crdName,
			Namespace: accountNamespace,
			Labels:    labelsMap,
		},
		Status: *status,
	}

	return vpcObj
}

// computeSgToInternalSgObject converts nsg object from cloud format(nsgTable) to security group runtime object.
func computeSgToInternalSgObject(nsg *nsgTable, selectorNamespacedName,
	accountNamespacedName *types.NamespacedName, region string) *runtimev1alpha1.SecurityGroup {
	status := &runtimev1alpha1.SecurityGroupStatus{
		CloudName: strings.ToLower(*nsg.Name),
		CloudId:   strings.ToLower(*nsg.ID),
		Provider:  runtimev1alpha1.AzureCloudProvider,
		Region:    region,
	}

	if nsg.Properties != nil {
		for _, securityRule := range nsg.Properties.SecurityRules {
			rule := parseSecurityRules(securityRule)
			status.Rules = append(status.Rules, *rule)
		}
		for _, securityRule := range nsg.Properties.DefaultSecurityRules {
			rule := parseSecurityRules(securityRule)
			status.Rules = append(status.Rules, *rule)
		}
	}

	var vpcName string
	if nsg.VnetID != nil {
		_, _, resourceName, err := extractFieldsFromAzureResourceID(strings.ToLower(*nsg.VnetID))
		if err != nil {
			azurePluginLogger().Error(err, "failed to create SecurityGroup object")
			return nil
		}
		vpcName = utils.GenerateShortResourceIdentifier(*nsg.VnetID, resourceName)
	}

	var nsgName string
	if nsg.ID != nil {
		_, _, resourceName, err := extractFieldsFromAzureResourceID(strings.ToLower(*nsg.ID))
		if err != nil {
			azurePluginLogger().Error(err, "failed to create SecurityGroup object")
			return nil
		}
		nsgName = utils.GenerateShortResourceIdentifier(*nsg.ID, resourceName)
	}

	labels := map[string]string{
		labels.CloudAccountNamespace:  accountNamespacedName.Namespace,
		labels.CloudAccountName:       accountNamespacedName.Name,
		labels.CloudSelectorNamespace: selectorNamespacedName.Namespace,
		labels.CloudSelectorName:      selectorNamespacedName.Name,
		labels.VpcName:                vpcName,
	}

	sgObj := &runtimev1alpha1.SecurityGroup{
		ObjectMeta: v1.ObjectMeta{
			Name:      nsgName,
			Namespace: selectorNamespacedName.Namespace,
			Labels:    labels,
		},
		Status: *status,
	}
	return sgObj
}

// parseSecurityRules helper function to parse cloud security rules and convert into runtime security group object.
func parseSecurityRules(securityRule *armnetwork.SecurityRule) *runtimev1alpha1.Rule {
	rule := runtimev1alpha1.Rule{}
	if securityRule.Name != nil {
		rule.Name = *securityRule.Name
	}
	if securityRule.Properties.Priority != nil {
		rule.Priority = *securityRule.Properties.Priority
	}
	if securityRule.Properties.Protocol != nil {
		if *securityRule.Properties.Protocol == "*" {
			rule.Protocol = "any"
		} else {
			rule.Protocol = strings.ToLower(string(*securityRule.Properties.Protocol))
		}
	}
	if securityRule.Properties.Access != nil {
		rule.Action = string(*securityRule.Properties.Access)
	}
	if *securityRule.Properties.Direction == "Inbound" {
		rule.Ingress = true
		if securityRule.Properties.DestinationPortRange != nil {
			if *securityRule.Properties.DestinationPortRange == "*" {
				rule.Port = "any"
			} else {
				rule.Port = *securityRule.Properties.DestinationPortRange
			}
		}
	} else {
		rule.Ingress = false
		if securityRule.Properties.SourcePortRange != nil {
			if *securityRule.Properties.SourcePortRange == "*" {
				rule.Port = "any"
			} else {
				rule.Port = *securityRule.Properties.SourcePortRange
			}
		}
	}
	if securityRule.Properties.Description != nil {
		rule.Description = *securityRule.Properties.Description
	}

	if securityRule.Properties.SourceAddressPrefix != nil {
		if *securityRule.Properties.SourceAddressPrefix == "*" {
			rule.Source = append(rule.Source, "any")
		} else {
			rule.Source = append(rule.Source, *securityRule.Properties.SourceAddressPrefix)
		}
	}
	if securityRule.Properties.DestinationAddressPrefix != nil {
		if *securityRule.Properties.DestinationAddressPrefix == "*" {
			rule.Destination = append(rule.Destination, "any")
		} else {
			rule.Destination = append(rule.Destination, *securityRule.Properties.DestinationAddressPrefix)
		}
	}
	for index := range securityRule.Properties.SourceAddressPrefixes {
		rule.Source = append(rule.Source, *securityRule.Properties.SourceAddressPrefixes[index])
	}
	for index := range securityRule.Properties.DestinationAddressPrefixes {
		rule.Destination = append(rule.Destination, *securityRule.Properties.DestinationAddressPrefixes[index])
	}

	if len(rule.Source) == 0 {
		for index := range securityRule.Properties.SourceApplicationSecurityGroups {
			if asgID := securityRule.Properties.SourceApplicationSecurityGroups[index].ID; asgID != nil {
				rule.Source = append(rule.Source, strings.ToLower(*asgID))
			}
		}
	}
	if len(rule.Destination) == 0 {
		for index := range securityRule.Properties.DestinationApplicationSecurityGroups {
			if asgID := securityRule.Properties.DestinationApplicationSecurityGroups[index].ID; asgID != nil {
				rule.Destination = append(rule.Destination, strings.ToLower(*asgID))
			}
		}
	}
	return &rule
}
