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
	"strings"

	cpv1beta2 "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	"antrea.io/nephe/pkg/labels"
	k8stemplates "antrea.io/nephe/test/templates"
)

// ConfigANPApplyTo helper function to configure appliedTo in Antrea NetworkPolicy.
func ConfigANPApplyTo(kind, instanceName, vpc, tagKey, tagVal string) *k8stemplates.EntitySelectorParameters {
	ret := &k8stemplates.EntitySelectorParameters{}
	if len(kind) > 0 {
		ret.Kind = labels.ExternalEntityLabelKeyKind + ": " + strings.ToLower(kind)
	}
	if len(vpc) > 0 {
		ret.VPC = labels.ExternalEntityLabelKeyOwnerVmVpc + ": " + strings.ToLower(vpc)
	}
	if len(instanceName) > 0 {
		ret.CloudInstanceName = labels.ExternalEntityLabelKeyOwnerVm + ": " + strings.ToLower(instanceName)
	}
	if len(tagKey) > 0 {
		tagKey = labels.LabelPrefixNephe + labels.ExternalEntityLabelKeyTagPrefix + tagKey
		ret.Tags = map[string]string{tagKey: tagVal}
	}
	return ret
}

// ConfigANPToFrom helper function to configure to and from fields in Antrea NetworkPolicy.
func ConfigANPToFrom(kind, instanceName, vpc, tagKey, tagVal, ipBlock, nsName string, protocol cpv1beta2.Protocol, ports []string,
	denyAll bool) *k8stemplates.ToFromParameters {
	ret := &k8stemplates.ToFromParameters{
		DenyAll: denyAll,
	}
	if len(ipBlock) > 0 {
		ret.IPBlock = ipBlock
	}
	if len(kind) > 0 {
		ret.Entity = &k8stemplates.EntitySelectorParameters{
			Kind: labels.ExternalEntityLabelKeyKind + ": " + strings.ToLower(kind),
		}
		if len(vpc) > 0 {
			ret.Entity.VPC = labels.ExternalEntityLabelKeyOwnerVmVpc + ": " + strings.ToLower(vpc)
		}
		if len(instanceName) > 0 {
			ret.Entity.CloudInstanceName = labels.ExternalEntityLabelKeyOwnerVm + ": " + strings.ToLower(instanceName)
		}
		if len(tagKey) > 0 {
			tagKey = labels.LabelPrefixNephe + labels.ExternalEntityLabelKeyTagPrefix + tagKey
			ret.Entity.Tags = map[string]string{tagKey: tagVal}
		}
	}

	if protocol == cpv1beta2.ProtocolTCP {
		for _, p := range ports {
			ret.Ports = append(ret.Ports, &k8stemplates.PortParameters{Protocol: string(protocol), Port: p})
		}
	} else if protocol == cpv1beta2.ProtocolICMP {
		ret.ICMPProtocol = string(cpv1beta2.ProtocolICMP)
	}
	return ret
}
