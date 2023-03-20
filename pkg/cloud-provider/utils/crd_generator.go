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

package utils

import (
	"fmt"
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/controllers/config"
)

// GenerateVirtualMachineCRD constructs a VirtualMachine CR based on parameters.
func GenerateVirtualMachineCRD(crdName, cloudName, cloudID, region, namespace, cloudNetwork, shortNetworkID string,
	state runtimev1alpha1.VMState, tags map[string]string, networkInterfaces []runtimev1alpha1.NetworkInterface,
	provider cloudcommon.ProviderType, accountId string) *runtimev1alpha1.VirtualMachine {
	vmStatus := &runtimev1alpha1.VirtualMachineStatus{
		Provider:            runtimev1alpha1.CloudProvider(provider),
		VirtualPrivateCloud: shortNetworkID,
		Tags:                tags,
		State:               state,
		NetworkInterfaces:   networkInterfaces,
		Region:              region,
		Agented:             false,
	}
	labelsMap := map[string]string{
		config.LabelCloudAssignedID:    cloudID,
		config.LabelCloudAssignedName:  cloudName,
		config.LabelCloudAssignedVPCID: cloudNetwork,
		config.LabelCloudAccountID:     accountId,
	}

	vmCrd := &runtimev1alpha1.VirtualMachine{
		TypeMeta: v1.TypeMeta{
			Kind:       cloudcommon.VirtualMachineCRDKind,
			APIVersion: cloudcommon.RuntimeAPIVersion,
		},
		ObjectMeta: v1.ObjectMeta{
			UID:       uuid.NewUUID(),
			Name:      crdName,
			Namespace: namespace,
			Labels:    labelsMap,
		},
		Status: *vmStatus,
	}

	return vmCrd
}

func GenerateShortResourceIdentifier(id string, prefixToAdd string) string {
	idTrim := strings.Trim(id, " ")
	if len(idTrim) == 0 {
		return ""
	}

	// Ascii value of the characters will be added to generate unique name
	var sum uint32 = 0
	for _, value := range strings.ToLower(idTrim) {
		sum += uint32(value)
	}

	str := fmt.Sprintf("%v-%v", strings.ToLower(prefixToAdd), sum)
	return str
}

// GenerateInternalVpcObject generates runtimev1alpha1 vpc object using the input parameters.
func GenerateInternalVpcObject(name string, namespace string, labels map[string]string, cloudName string,
	cloudId string, tags map[string]string, cloudProvider runtimev1alpha1.CloudProvider,
	region string, cidrs []string, managed bool) *runtimev1alpha1.Vpc {
	status := &runtimev1alpha1.VpcStatus{
		Name:     cloudName,
		Id:       cloudId,
		Provider: cloudProvider,
		Region:   region,
		Tags:     tags,
		Cidrs:    cidrs,
		Managed:  managed,
	}

	vpc := &runtimev1alpha1.Vpc{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: *status,
	}

	return vpc
}

// GetCloudResourceCRName gets corresponding cr name from cloud resource id based on cloud type.
func GetCloudResourceCRName(providerType, name string) string {
	switch providerType {
	case string(runtimev1alpha1.AWSCloudProvider):
		return name
	case string(runtimev1alpha1.AzureCloudProvider):
		tokens := strings.Split(name, "/")
		return GenerateShortResourceIdentifier(name, tokens[len(tokens)-1])
	default:
		return name
	}
}
