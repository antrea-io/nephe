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

package inventory

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/inventory/common"
)

var (
	testVpcID01    = "testVpcID01"
	testVpcName01  = "testVpcName01"
	testVpcID02    = "testVpcID02"
	testVpcName02  = "testVpcName02"
	namespace      = "testNS"
	accountName    = "account01"
	namespacedName = types.NamespacedName{Namespace: namespace, Name: accountName}
	region         = "xyz"
	vpcCacheKey1   = fmt.Sprintf("%s/%s-%s", namespace, accountName, testVpcID01)
	cloudInventory *Inventory
)

var _ = Describe("Validate Vpc Cache", func() {
	BeforeEach(func() {
		cloudInventory = InitInventory()
	})

	It("Build vpc cache", func() {
		vpcList1 := make(map[string]*runtimev1alpha1.Vpc)
		vpcObj1 := new(runtimev1alpha1.Vpc)
		labelsMap := map[string]string{
			common.VpcLabelAccountName: accountName,
			common.VpcLabelRegion:      region,
		}
		vpcObj1.Name = "obj1"
		vpcObj1.Namespace = namespace
		vpcObj1.Labels = labelsMap
		vpcObj1.Info.Id = testVpcID01
		vpcObj1.Info.Name = testVpcName01
		vpcList1[testVpcID01] = vpcObj1

		vpcObj2 := new(runtimev1alpha1.Vpc)
		vpcObj2.Name = "obj2"
		vpcObj2.Namespace = namespace
		vpcObj2.Labels = labelsMap
		vpcObj2.Info.Id = testVpcID02
		vpcObj2.Info.Name = testVpcName02
		vpcList1[testVpcID02] = vpcObj2

		err := cloudInventory.BuildVpcCache(vpcList1, &namespacedName)
		Expect(err).ShouldNot(HaveOccurred())

		allVpcList := cloudInventory.GetAllVpcs()
		Expect(len(vpcList1), Equal(len(allVpcList)))

		vpcListByIndex, err := cloudInventory.GetVpcsFromIndexer(common.VpcIndexerByAccountNameSpacedName, namespacedName.String())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(vpcList1), Equal(len(vpcListByIndex)))

		_, err = cloudInventory.GetVpcsFromIndexer("dummyIndexer", namespacedName.String())
		Expect(err).Should(HaveOccurred())

		// When vpcList doesn't contain an object(vpc id testVpcID02) which is present in vpcCache, it is deleted from cache.
		vpcList2 := make(map[string]*runtimev1alpha1.Vpc)
		vpcObj := new(runtimev1alpha1.Vpc)
		vpcObj.Name = "obj1"
		vpcObj.Namespace = namespace
		vpcObj.Info.Id = testVpcID01
		vpcObj.Info.Name = testVpcName01
		vpcList2[testVpcID01] = vpcObj

		err = cloudInventory.BuildVpcCache(vpcList2, &namespacedName)
		Expect(err).ShouldNot(HaveOccurred())

		vpcListByIndex, err = cloudInventory.GetVpcsFromIndexer(common.VpcIndexerByAccountNameSpacedName, namespacedName.String())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(vpcList2), Equal(len(vpcListByIndex)))

		// Delete vpc cache.
		err = cloudInventory.DeleteVpcCache(&namespacedName)
		Expect(err).ShouldNot(HaveOccurred())
		_, exist, err := cloudInventory.vpcStore.Get(vpcCacheKey1)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(exist).Should(BeFalse())
	})
})
