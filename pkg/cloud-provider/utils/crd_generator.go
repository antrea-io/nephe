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

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
)

func GenerateVirtualMachineCRD(crdName string, cloudName string, cloudID string, namespace string, cloudNetwork string,
	shortNetworkID string, state cloudv1alpha1.VMState, tags map[string]string, networkInterfaces []cloudv1alpha1.NetworkInterface,
	provider cloudcommon.ProviderType, accountId string) *cloudv1alpha1.VirtualMachine {
	vmStatus := &cloudv1alpha1.VirtualMachineStatus{
		Provider:            cloudv1alpha1.CloudProvider(provider),
		VirtualPrivateCloud: shortNetworkID,
		Tags:                tags,
		State:               state,
		NetworkInterfaces:   networkInterfaces,
		Agented:             false,
	}
	annotationsMap := map[string]string{
		cloudcommon.AnnotationCloudAssignedIDKey:    cloudID,
		cloudcommon.AnnotationCloudAssignedNameKey:  cloudName,
		cloudcommon.AnnotationCloudAssignedVPCIDKey: cloudNetwork,
		cloudcommon.AnnotationCloudAccountIDKey:     accountId,
	}

	vmCrd := &cloudv1alpha1.VirtualMachine{
		TypeMeta: v1.TypeMeta{
			Kind:       cloudcommon.VirtualMachineCRDKind,
			APIVersion: cloudcommon.APIVersion,
		},
		ObjectMeta: v1.ObjectMeta{
			UID:         uuid.NewUUID(),
			Name:        crdName,
			Namespace:   namespace,
			Annotations: annotationsMap,
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
	cloudId string, tags map[string]string, cloudProvider cloudv1alpha1.CloudProvider,
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

func GetCloudResourceCrdName(providerType, name string) string {
	switch providerType {
	case string(cloudv1alpha1.AWSCloudProvider):
		return name
	case string(cloudv1alpha1.AzureCloudProvider):
		tokens := strings.Split(name, "/")
		return GenerateShortResourceIdentifier(name, tokens[len(tokens)-1])
	default:
		return name
	}
}
