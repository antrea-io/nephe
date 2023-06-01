// Copyright 2023 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package indexer

const (
	ByNamespace      = "namespace"
	ByNamespacedName = "namespace-name"

	VpcByAccountNamespacedName = "namespace-cloud-account-name"

	VirtualMachineByCloudId                = "cloud-assigned-id"
	VirtualMachineByCloudResourceID        = "cloud-resource-id"
	VirtualMachineByAccountNamespacedName  = "namespaced-cloud-account-name"
	VirtualMachineBySelectorNamespacedName = "namespaced-cloud-selector-name"
	SecurityGroupByAccountNamespacedName   = "namespaced-cloud-account-name"
)
