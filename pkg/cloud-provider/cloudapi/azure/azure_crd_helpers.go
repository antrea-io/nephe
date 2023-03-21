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

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/cloud-provider/utils"
	"antrea.io/nephe/pkg/controllers/config"
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

func computeInstanceToVirtualMachineCRD(instance *virtualMachineTable, namespace string, accountId string,
	region string) *runtimev1alpha1.VirtualMachine {
	tags := make(map[string]string)

	vmTags := instance.Tags
	for key, value := range vmTags {
		// skip any tags added by nephe for internal processing
		_, hasAGPrefix, hasATPrefix := securitygroup.IsNepheControllerCreatedSG(key)
		if hasAGPrefix || hasATPrefix {
			continue
		}
		tags[key] = *value
	}

	// Network interfaces associated with Virtual machine
	instNetworkInterfaces := instance.NetworkInterfaces
	networkInterfaces := make([]runtimev1alpha1.NetworkInterface, 0, len(instNetworkInterfaces))
	for _, nwInf := range instNetworkInterfaces {
		var ipAddressCRDs []runtimev1alpha1.IPAddress
		if len(nwInf.PrivateIps) > 0 {
			for _, ipAddress := range nwInf.PrivateIps {
				ipAddressCRD := runtimev1alpha1.IPAddress{
					AddressType: runtimev1alpha1.AddressTypeInternalIP,
					Address:     *ipAddress,
				}
				ipAddressCRDs = append(ipAddressCRDs, ipAddressCRD)
			}
		}
		if len(nwInf.PublicIps) > 0 {
			for _, publicIP := range nwInf.PublicIps {
				ipAddressCRD := runtimev1alpha1.IPAddress{
					AddressType: runtimev1alpha1.AddressTypeExternalIP,
					Address:     *publicIP,
				}
				ipAddressCRDs = append(ipAddressCRDs, ipAddressCRD)
			}
		}
		macAddress := ""
		if nwInf.MacAddress != nil {
			macAddress = *nwInf.MacAddress
		}

		networkInterface := runtimev1alpha1.NetworkInterface{
			Name: *nwInf.ID,
			MAC:  macAddress,
			IPs:  ipAddressCRDs,
		}
		networkInterfaces = append(networkInterfaces, networkInterface)
	}

	cloudNetworkID := strings.ToLower(*instance.VnetID)
	cloudID := strings.ToLower(*instance.ID)
	cloudName := strings.ToLower(*instance.Name)
	crdName := utils.GenerateShortResourceIdentifier(cloudID, cloudName)

	_, _, nwResName, err := extractFieldsFromAzureResourceID(cloudNetworkID)
	if err != nil {
		azurePluginLogger().Error(err, "failed to create VirtualMachine CRD")
		return nil
	}
	cloudNetworkShortID := utils.GenerateShortResourceIdentifier(cloudNetworkID, nwResName)
	var state runtimev1alpha1.VMState
	if instance.Status != nil {
		state = azureStateMap[*instance.Status]
	} else {
		state = runtimev1alpha1.Unknown
	}
	return utils.GenerateVirtualMachineCRD(crdName, strings.ToLower(cloudName), strings.ToLower(cloudID), strings.ToLower(region),
		namespace, strings.ToLower(cloudNetworkID), cloudNetworkShortID, state, tags, networkInterfaces, providerType, accountId)
}

// ComputeVpcToInternalVpcObject converts vnet object from cloud format(network.VirtualNetwork) to vpc runtime object.
func ComputeVpcToInternalVpcObject(vnet *armnetwork.VirtualNetwork, namespace, nameSpacedAccountName,
	region string, managed bool) *runtimev1alpha1.Vpc {
	crdName := utils.GenerateShortResourceIdentifier(*vnet.ID, *vnet.Name)
	tags := make(map[string]string, 0)
	if len(vnet.Tags) != 0 {
		for k, v := range vnet.Tags {
			tags[k] = *v
		}
	}
	cidrs := make([]string, 0)
	properties := vnet.Properties
	if properties != nil && properties.AddressSpace != nil && len(properties.AddressSpace.AddressPrefixes) > 0 {
		for _, cidr := range vnet.Properties.AddressSpace.AddressPrefixes {
			cidrs = append(cidrs, *cidr)
		}
	}
	labelsMap := map[string]string{
		config.LabelCloudNamespacedAccountName: nameSpacedAccountName,
		config.LabelCloudRegion:                region,
	}
	return utils.GenerateInternalVpcObject(crdName, namespace, labelsMap, strings.ToLower(*vnet.Name),
		strings.ToLower(*vnet.ID), tags, runtimev1alpha1.AzureCloudProvider, region, cidrs, managed)
}
