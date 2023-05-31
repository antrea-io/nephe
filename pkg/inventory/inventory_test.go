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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory/indexer"
	"antrea.io/nephe/pkg/labels"
)

func TestInventory(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Inventory Suite")
}

var (
	testVpcID01           = "testVpcID01"
	testVpcName01         = "testVpcName01"
	testVpcID02           = "testVpcID02"
	testVpcName02         = "testVpcName02"
	namespace             = "testNS"
	accountName           = "account01"
	namespacedAccountName = types.NamespacedName{Namespace: namespace, Name: accountName}
	vpcCacheKey1          = fmt.Sprintf("%s-%s", namespacedAccountName, testVpcID01)
	testVmID01            = "testVmID01"
	testVmName01          = "testVmName01"
	testVmID02            = "testVmID02"
	vmCacheKey1           = fmt.Sprintf("%s/%s", namespace, testVmID01)
	vmCacheKey2           = fmt.Sprintf("%s/%s", namespace, testVmID02)
	networkInterfaceID    = "networkInterface01"
	macAddress            = "00-01-02-03-04-05"
	ipAddress             = "10.10.10.10"
	ipAddressCRDs         []runtimev1alpha1.IPAddress
	cloudInventory        *Inventory
)

var _ = Describe("Validate VPC and Virtual Machine Inventory", func() {
	BeforeEach(func() {
		cloudInventory = InitInventory()
	})

	Context("VPC Inventory Test", func() {
		vpcLabelsMap := map[string]string{
			labels.CloudAccountName:      namespacedAccountName.Name,
			labels.CloudAccountNamespace: namespacedAccountName.Namespace,
		}
		vpcList1 := make(map[string]*runtimev1alpha1.Vpc)
		vpcObj1 := new(runtimev1alpha1.Vpc)
		vpcObj1.Name = "obj1"
		vpcObj1.Namespace = namespace
		vpcObj1.Labels = vpcLabelsMap
		vpcObj1.Status.CloudId = testVpcID01
		vpcObj1.Status.CloudName = testVpcName01
		vpcList1[testVpcID01] = vpcObj1

		vpcObj2 := new(runtimev1alpha1.Vpc)
		vpcObj2.Name = "obj2"
		vpcObj2.Namespace = namespace
		vpcObj2.Labels = vpcLabelsMap
		vpcObj2.Status.CloudId = testVpcID02
		vpcObj2.Status.CloudName = testVpcName02
		vpcList1[testVpcID02] = vpcObj2

		It("Add VPCs to VPC inventory", func() {
			err := cloudInventory.BuildVpcCache(vpcList1, &namespacedAccountName)
			Expect(err).ShouldNot(HaveOccurred())

			allVpcList := cloudInventory.GetAllVpcs()
			Expect(allVpcList).Should(HaveLen(len(vpcList1)))

			vpcListByIndex, err := cloudInventory.GetVpcsFromIndexer(indexer.VpcByNamespacedAccountName,
				namespacedAccountName.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vpcListByIndex).Should(HaveLen(len(vpcList1)))

			_, err = cloudInventory.GetVpcsFromIndexer("dummyIndexer", namespacedAccountName.String())
			Expect(err).Should(HaveOccurred())
		})
		It("Delete a VPC from VPC Cache", func() {
			err := cloudInventory.BuildVpcCache(vpcList1, &namespacedAccountName)
			Expect(err).ShouldNot(HaveOccurred())

			// When vpcList doesn't contain an object(vpc id testVpcID02) which is present in vpcCache, it is deleted from cache.
			vpcList2 := make(map[string]*runtimev1alpha1.Vpc)
			vpcObj := new(runtimev1alpha1.Vpc)
			vpcObj.Name = "obj1"
			vpcObj.Namespace = namespace
			vpcObj.Status.CloudId = testVpcID01
			vpcObj.Status.CloudName = testVpcName01
			vpcList2[testVpcID01] = vpcObj

			err = cloudInventory.BuildVpcCache(vpcList2, &namespacedAccountName)
			Expect(err).ShouldNot(HaveOccurred())

			vpcListByIndex, err := cloudInventory.GetVpcsFromIndexer(indexer.VpcByNamespacedAccountName,
				namespacedAccountName.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vpcListByIndex).Should(HaveLen(len(vpcList2)))
		})
		It("Delete VPC inventory", func() {
			err := cloudInventory.BuildVpcCache(vpcList1, &namespacedAccountName)
			Expect(err).ShouldNot(HaveOccurred())

			// Delete vpc cache.
			err = cloudInventory.DeleteVpcsFromCache(&namespacedAccountName)
			Expect(err).ShouldNot(HaveOccurred())
			_, exist, err := cloudInventory.vpcStore.Get(vpcCacheKey1)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(exist).Should(BeFalse())
		})
	})
	Context("VM Inventory Test", func() {
		vmLabelsMap := map[string]string{
			labels.CloudAccountName:      namespacedAccountName.Name,
			labels.CloudAccountNamespace: namespacedAccountName.Namespace,
			labels.CloudVmUID:            testVmID01,
			labels.CloudVpcUID:           testVpcID01,
			labels.VpcName:               testVpcName01,
		}

		ipAddressCRD := runtimev1alpha1.IPAddress{
			AddressType: runtimev1alpha1.AddressTypeInternalIP,
			Address:     ipAddress,
		}
		ipAddressCRDs = append(ipAddressCRDs, ipAddressCRD)
		networkInterfaces := make([]runtimev1alpha1.NetworkInterface, 0, 1)
		networkInterface := runtimev1alpha1.NetworkInterface{
			Name: networkInterfaceID,
			MAC:  macAddress,
			IPs:  ipAddressCRDs,
		}
		networkInterfaces = append(networkInterfaces, networkInterface)

		tags := make(map[string]string)
		tags["name"] = testVmID01

		vmStatus := &runtimev1alpha1.VirtualMachineStatus{
			Provider:          runtimev1alpha1.AWSCloudProvider,
			Tags:              tags,
			State:             runtimev1alpha1.Running,
			NetworkInterfaces: networkInterfaces,
			Agented:           false,
			CloudId:           testVmID01,
			CloudName:         testVmName01,
			CloudVpcId:        testVpcID01,
			CloudVpcName:      testVpcName01,
		}

		vmList := make(map[string]*runtimev1alpha1.VirtualMachine)
		vmObj := new(runtimev1alpha1.VirtualMachine)
		vmObj.Name = testVmID01
		vmObj.Namespace = namespace
		vmObj.Labels = vmLabelsMap
		vmObj.Status = *vmStatus
		vmList[testVmID01] = vmObj

		It("Add VMs to VM inventory", func() {
			cloudInventory.BuildVmCache(vmList, &namespacedAccountName)
			allVmList := cloudInventory.GetAllVms()
			Expect(allVmList).Should(HaveLen(len(vmList)))

			vmListByIndex, err := cloudInventory.GetVmFromIndexer(indexer.VirtualMachineByNamespacedAccountName,
				namespacedAccountName.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vmListByIndex).Should(HaveLen(len(vmList)))
			for _, i := range vmListByIndex {
				vm := i.(*runtimev1alpha1.VirtualMachine)
				Expect(vm.Name).To(Equal(testVmID01))
			}
		})
		It("Delete a VM from VM inventory", func() {
			cloudInventory.BuildVmCache(vmList, &namespacedAccountName)

			vmListByIndex, err := cloudInventory.GetVmFromIndexer(indexer.VirtualMachineByNamespacedAccountName,
				namespacedAccountName.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vmListByIndex).Should(HaveLen(len(vmList)))
			for _, i := range vmListByIndex {
				vm := i.(*runtimev1alpha1.VirtualMachine)
				Expect(vm.Name).To(Equal(testVmID01))
			}

			// Delete vms from the inventory which are not found in latest vm list.
			vmList2 := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmObj := new(runtimev1alpha1.VirtualMachine)
			vmObj.Name = testVmID02
			vmObj.Namespace = namespace
			vmObj.Labels = vmLabelsMap
			vmObj.Status = *vmStatus
			vmList2[testVmID02] = vmObj
			cloudInventory.BuildVmCache(vmList2, &namespacedAccountName)

			_, exist := cloudInventory.GetVmByKey(vmCacheKey2)
			Expect(exist).Should(BeTrue())

			_, exist = cloudInventory.GetVmByKey(vmCacheKey1)
			Expect(exist).Should(BeFalse())
		})
		It("Update Agented field in Status and add to VM inventory", func() {
			cloudInventory.BuildVmCache(vmList, &namespacedAccountName)

			vm, exist := cloudInventory.GetVmByKey(vmCacheKey1)
			Expect(exist).Should(BeTrue())
			Expect(vm.Status.Agented).To(BeFalse())

			// Update Agented field in Status from false to true.
			vmStatusUpdate := &runtimev1alpha1.VirtualMachineStatus{
				Provider:          runtimev1alpha1.AWSCloudProvider,
				Tags:              tags,
				State:             runtimev1alpha1.Running,
				NetworkInterfaces: networkInterfaces,
				Agented:           true,
			}
			vmListUpdate := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmObjUpdate := new(runtimev1alpha1.VirtualMachine)
			vmObjUpdate.Name = testVmID01
			vmObjUpdate.Namespace = namespace
			vmObjUpdate.Labels = vmLabelsMap
			vmObjUpdate.Status = *vmStatusUpdate
			vmListUpdate[testVmID01] = vmObjUpdate
			cloudInventory.BuildVmCache(vmListUpdate, &namespacedAccountName)

			// Vm object should be updated the latest Status field.
			vm, exist = cloudInventory.GetVmByKey(vmCacheKey1)
			Expect(exist).Should(BeTrue())
			Expect(vm.Status.Agented).To(BeTrue())
		})
		It("Update State field in Status and add to VM inventory", func() {
			cloudInventory.BuildVmCache(vmList, &namespacedAccountName)

			vm, exist := cloudInventory.GetVmByKey(vmCacheKey1)
			Expect(exist).Should(BeTrue())
			Expect(vm.Status.State).To(Equal(runtimev1alpha1.Running))

			// Update State field in Status from running to stopped.
			vmStatusUpdate := &runtimev1alpha1.VirtualMachineStatus{
				Provider:          runtimev1alpha1.AWSCloudProvider,
				Tags:              tags,
				State:             runtimev1alpha1.Stopped,
				NetworkInterfaces: networkInterfaces,
				Agented:           false,
			}
			vmListUpdate := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmObjUpdate := new(runtimev1alpha1.VirtualMachine)
			vmObjUpdate.Name = testVmID01
			vmObjUpdate.Namespace = namespace
			vmObjUpdate.Labels = vmLabelsMap
			vmObjUpdate.Status = *vmStatusUpdate
			vmListUpdate[testVmID01] = vmObjUpdate
			cloudInventory.BuildVmCache(vmListUpdate, &namespacedAccountName)

			// Vm object should be updated with the latest Status field.
			vm, exist = cloudInventory.GetVmByKey(vmCacheKey1)
			Expect(exist).Should(BeTrue())
			Expect(vm.Status.State).To(Equal(runtimev1alpha1.Stopped))
		})
		It("Delete VM inventory", func() {
			cloudInventory.BuildVmCache(vmList, &namespacedAccountName)

			vmListByIndex, err := cloudInventory.GetVmFromIndexer(indexer.VirtualMachineByNamespacedAccountName,
				namespacedAccountName.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vmListByIndex).Should(HaveLen(len(vmList)))

			// Delete vm cache.
			err = cloudInventory.DeleteVmsFromCache(&namespacedAccountName)
			Expect(err).ShouldNot(HaveOccurred())
			_, exist := cloudInventory.GetVmByKey(vmCacheKey1)
			Expect(exist).Should(BeFalse())
		})
	})
})
