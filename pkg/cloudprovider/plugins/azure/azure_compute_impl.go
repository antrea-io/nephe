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

package azure

import (
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"k8s.io/apimachinery/pkg/types"

	nephetypes "antrea.io/nephe/pkg/types"
)

// GetCloudInventory pulls cloud vpc and vm inventory from internal snapshot.
func (c *azureCloud) GetCloudInventory(accountNamespacedName *types.NamespacedName) (*nephetypes.CloudInventory, error) {
	return c.cloudCommon.GetCloudInventory(accountNamespacedName)
}

func (c *azureCloud) GetSecurityGroups(accountNamespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.SecurityGroup, error) {
	return c.cloudCommon.GetSecurityGroups(accountNamespacedName)
}
