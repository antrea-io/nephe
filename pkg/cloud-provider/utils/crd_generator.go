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
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
)

func GenerateVirtualMachineCRD(crdName string, cloudName string, cloudID string, namespace string, cloudNetwork string,
	shortNetworkID string, state cloudv1alpha1.VMState, tags map[string]string, networkInterfaces []cloudv1alpha1.NetworkInterface,
	provider cloudcommon.ProviderType) *cloudv1alpha1.VirtualMachine {
	vmStatus := &cloudv1alpha1.VirtualMachineStatus{
		Provider:            cloudv1alpha1.CloudProvider(provider),
		VirtualPrivateCloud: shortNetworkID,
		Tags:                tags,
		State:               state,
		NetworkInterfaces:   networkInterfaces,
	}
	annotationsMap := map[string]string{
		cloudcommon.AnnotationCloudAssignedIDKey:    cloudID,
		cloudcommon.AnnotationCloudAssignedNameKey:  cloudName,
		cloudcommon.AnnotationCloudAssignedVPCIDKey: cloudNetwork,
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
