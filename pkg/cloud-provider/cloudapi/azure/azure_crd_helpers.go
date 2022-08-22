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

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/cloud-provider/utils"
)

var azureStateMap = map[string]v1alpha1.VMState{
	"PowerState/running":      v1alpha1.Running,
	"PowerState/deallocated":  v1alpha1.Stopped,
	"PowerState/deallocating": v1alpha1.ShuttingDown,
	"PowerState/stopping":     v1alpha1.Stopping,
	"PowerState/stopped":      v1alpha1.Stopped,
	"PowerState/starting":     v1alpha1.Starting,
	"PowerState/unknown":      v1alpha1.Unknown,
}

func computeInstanceToVirtualMachineCRD(instance *virtualMachineTable, namespace string) *v1alpha1.VirtualMachine {
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
	networkInterfaces := make([]v1alpha1.NetworkInterface, 0, len(instNetworkInterfaces))
	for _, nwInf := range instNetworkInterfaces {
		var ipAddressCRDs []v1alpha1.IPAddress
		if len(nwInf.PrivateIps) > 0 {
			for _, ipAddress := range nwInf.PrivateIps {
				ipAddressCRD := v1alpha1.IPAddress{
					AddressType: v1alpha1.AddressTypeInternalIP,
					Address:     *ipAddress,
				}
				ipAddressCRDs = append(ipAddressCRDs, ipAddressCRD)
			}
		}
		if len(nwInf.PublicIps) > 0 {
			for _, publicIP := range nwInf.PublicIps {
				ipAddressCRD := v1alpha1.IPAddress{
					AddressType: v1alpha1.AddressTypeInternalIP,
					Address:     *publicIP,
				}
				ipAddressCRDs = append(ipAddressCRDs, ipAddressCRD)
			}
		}
		macAddress := ""
		if nwInf.MacAddress != nil {
			macAddress = *nwInf.MacAddress
		}

		networkInterface := v1alpha1.NetworkInterface{
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
	var state v1alpha1.VMState
	if instance.Status != nil {
		state = azureStateMap[*instance.Status]
	} else {
		state = v1alpha1.Unknown
	}
	return utils.GenerateVirtualMachineCRD(crdName, strings.ToLower(cloudName), strings.ToLower(cloudID), namespace,
		strings.ToLower(cloudNetworkID), cloudNetworkShortID,
		state, tags, networkInterfaces, providerType)
}
