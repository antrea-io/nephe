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
	"time"

	"antrea.io/nephe/apis/crd/v1alpha1"
	k8stemplates "antrea.io/nephe/test/templates"
)

type CloudVPC interface {
	GetVPCID() string
	GetVPCName() string
	GetCRDVPCID() string
	GetVMs() []string
	GetVMIDs() []string
	GetVMNames() []string
	GetVMIPs() []string
	GetVMPrivateIPs() []string
	GetNICs() []string
	GetTags() []map[string]string
	IsConfigured() bool

	VMCmd(vm string, vmCmd []string, timeout time.Duration) (string, error)
	Delete(duration time.Duration) error
	Reapply(duration time.Duration) error
	GetCloudAccountParameters(name, namespace string, cloudCluster bool) k8stemplates.CloudAccountParameters
	GetEntitySelectorParameters(name, namespace, kind string) k8stemplates.CloudEntitySelectorParameters
}

func NewCloudVPC(provider v1alpha1.CloudProvider) (CloudVPC, error) {
	var vpc CloudVPC
	switch provider {
	case v1alpha1.AWSCloudProvider:
		vpc = newAWSVPC()
	case v1alpha1.AzureCloudProvider:
		vpc = newAzureVPC()
	default:
		return nil, fmt.Errorf("unsupported cloud provider: %v", provider)
	}
	if vpc.IsConfigured() {
		// VPC can be cleanup using ~/terraform/aws-tf|azure-tf destroy.
		return nil, fmt.Errorf("cannot reuse existing vpc")
	}
	return vpc, nil
}

func getListFromOutput(output map[string]interface{}, key string) []string {
	v := output[key]
	vv := v.(map[string]interface{})
	vlist := vv["value"].([]interface{})
	ret := make([]string, 0)
	for _, i := range vlist {
		ret = append(ret, i.(string))
	}
	return ret
}

func getMapFromOutput(output map[string]interface{}, key string) []map[string]string {
	v := output[key]
	vv := v.(map[string]interface{})
	vlist := vv["value"].([]interface{})
	ret := make([]map[string]string, 0)
	for _, i := range vlist {
		mapo := make(map[string]string)
		for k, v := range i.(map[string]interface{}) {
			mapo[k] = v.(string)
		}
		ret = append(ret, mapo)
	}
	return ret
}
