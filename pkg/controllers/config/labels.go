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

package config

const (
	// Well known labels on ExternalEntities so that they can be selected by Antrea NetworkPolicies.
	ExternalEntityLabelKeyTagPrefix    = "tag-"
	ExternalEntityLabelKeyNamespace    = LabelPrefixNephe + "namespace"
	ExternalEntityLabelKeyKind         = LabelPrefixNephe + "kind"
	ExternalEntityLabelKeyOwnerVm      = LabelPrefixNephe + "owner-vm"
	ExternalEntityLabelKeyOwnerVmVpc   = LabelPrefixNephe + "owner-vm-vpc"
	ExternalEntityLabelKeyCloudRegion  = LabelPrefixNephe + LabelPrefixCloud + "region"
	ExternalEntityLabelKeyCloudVpcUID  = LabelPrefixNephe + LabelPrefixCloud + "vpc-uid"
	ExternalEntityLabelKeyCloudVpcName = LabelPrefixNephe + LabelPrefixCloud + "vpc-name"
	ExternalEntityLabelKeyCloudVmUID   = LabelPrefixNephe + LabelPrefixCloud + "vm-uid"
	ExternalEntityLabelKeyCloudVmName  = LabelPrefixNephe + LabelPrefixCloud + "vm-name"
)

const (
	// TODO: Prefix with VirtualMachine? or move to inventory package?
	LabelPrefixNephe           = "nephe.io/"
	LabelPrefixCloud           = "cloud-"
	LabelCloudAccountName      = LabelPrefixNephe + "cpa-name"
	LabelCloudAccountNamespace = LabelPrefixNephe + "cpa-namespace"
	LabelVpcName               = LabelPrefixNephe + "vpc-name"
	LabelCloudRegion           = LabelPrefixNephe + LabelPrefixCloud + "region"
	LabelCloudVpcUID           = LabelPrefixNephe + LabelPrefixCloud + "vpc-uid"
	LabelCloudVmUID            = LabelPrefixNephe + LabelPrefixCloud + "vm-uid"
)
