// Copyright 2023 Antrea Authors.
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
	"strconv"
	"strings"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
)

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

// IsNepheControllerCreatedSG checks an SG is created by nephe
// and returns if it's an AppliedToGroup/AddressGroup sg and the sg name.
func IsNepheControllerCreatedSG(cloudSgName string) (string, bool, bool) {
	var sgName string
	isNepheControllerCreatedAddressGroup := false
	isNepheControllerCreatedAppliedToGroup := false

	suffix := strings.TrimPrefix(cloudSgName, cloudresource.GetControllerAddressGroupPrefix())
	if len(suffix) < len(cloudSgName) {
		isNepheControllerCreatedAddressGroup = true
		sgName = strings.ToLower(suffix)
	}

	if !isNepheControllerCreatedAddressGroup {
		suffix := strings.TrimPrefix(cloudSgName, cloudresource.GetControllerAppliedToPrefix())
		if len(suffix) < len(cloudSgName) {
			isNepheControllerCreatedAppliedToGroup = true
			sgName = strings.ToLower(suffix)
		}
	}
	return sgName, isNepheControllerCreatedAddressGroup, isNepheControllerCreatedAppliedToGroup
}

func FindResourcesBasedOnKind(cloudResources []*cloudresource.CloudResource) (map[string]struct{}, map[string]struct{}) {
	virtualMachineIDs := make(map[string]struct{})
	networkInterfaceIDs := make(map[string]struct{})

	for _, cloudResource := range cloudResources {
		if strings.Compare(string(cloudResource.Type), string(cloudresource.CloudResourceTypeVM)) == 0 {
			virtualMachineIDs[strings.ToLower(cloudResource.Name)] = struct{}{}
		}
		if strings.Compare(string(cloudResource.Type), string(cloudresource.CloudResourceTypeNIC)) == 0 {
			networkInterfaceIDs[strings.ToLower(cloudResource.Name)] = struct{}{}
		}
	}
	return virtualMachineIDs, networkInterfaceIDs
}

// SplitCloudRulesByDirection splits the given CloudRule slice into two, one for ingress rules and one for egress rules.
func SplitCloudRulesByDirection(rules []*cloudresource.CloudRule) ([]*cloudresource.CloudRule, []*cloudresource.CloudRule) {
	ingressRules := make([]*cloudresource.CloudRule, 0)
	egressRules := make([]*cloudresource.CloudRule, 0)
	for _, rule := range rules {
		switch rule.Rule.(type) {
		case *cloudresource.IngressRule:
			ingressRules = append(ingressRules, rule)
		case *cloudresource.EgressRule:
			egressRules = append(egressRules, rule)
		}
	}
	return ingressRules, egressRules
}

// GenerateCloudDescription generates a CloudRuleDescription object and converts to string.
func GenerateCloudDescription(namespacedName string, priority *float64) (string, error) {
	tokens := strings.Split(namespacedName, "/")
	if len(tokens) != 2 {
		return "", fmt.Errorf("invalid namespacedname %v", namespacedName)
	}
	desc := cloudresource.CloudRuleDescription{
		Name:      tokens[1],
		Namespace: tokens[0],
	}
	if priority != nil {
		desc.Priority = priority
	}
	return desc.String(), nil
}

// ExtractCloudDescription converts a string to a CloudRuleDescription object.
func ExtractCloudDescription(description *string) (*cloudresource.CloudRuleDescription, bool) {
	if description == nil {
		return nil, false
	}
	descMap := map[string]string{}
	tempSlice := strings.Split(*description, ",")
	// each key and value are separated by ":"
	for i := range tempSlice {
		keyValuePair := strings.Split(strings.TrimSpace(tempSlice[i]), ":")
		if len(keyValuePair) == 2 {
			descMap[keyValuePair[0]] = keyValuePair[1]
		}
	}

	// check if any of the fields are empty.
	if descMap[cloudresource.Name] == "" || descMap[cloudresource.Namespace] == "" {
		return nil, false
	}

	desc := &cloudresource.CloudRuleDescription{
		Name:      descMap[cloudresource.Name],
		Namespace: descMap[cloudresource.Namespace],
	}

	if descMap[cloudresource.Priority] != "" {
		priority, _ := strconv.ParseFloat(descMap[cloudresource.Priority], 64)
		desc.Priority = &priority
	}
	return desc, true
}
