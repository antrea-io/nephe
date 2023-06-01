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

package integration

import (
	"context"
	"fmt"
	"math/rand"
	"path"
	"reflect"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cloudutils "antrea.io/nephe/pkg/cloudprovider/utils"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: VPC Inventory", focusAws, focusAzure), func() {
	var (
		namespace     *v1.Namespace
		vmIDs         []string
		vpcID         string
		vpcName       string
		cloudProvider string
		kind          string
		vpcObjectName string
	)

	createNS := func() {
		logf.Log.Info("Create", "namespace", namespace.Name)
		err := k8sClient.Create(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
	}

	deleteNS := func() {
		Expect(namespace).ToNot(BeNil())
		logf.Log.Info("Delete", "namespace", namespace.Name)
		err := k8sClient.Delete(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
	}

	// verifyInventory provides an option to get VPC by VPC CR Name or get all VPCs or get all VMs.
	verifyInventory := func(vpcObjectName string, getAllVpcs, getAllVms, expectedEmptyOutput bool, expectedOutput string) error {
		err := wait.Poll(time.Second*5, time.Second*30, func() (bool, error) {
			var cmd string
			if getAllVpcs {
				cmd = fmt.Sprintf("get vpc -n %v -o json -o json -o=jsonpath={.items}", namespace.Name)
			} else if getAllVms {
				cmd = fmt.Sprintf("get vm -n %v -o json -o json -o=jsonpath={.items}", namespace.Name)
			} else {
				// Find if expected vpc id is present in the vpc inventory.
				cmd = fmt.Sprintf("get vpc %v -n %v -o json -o=jsonpath={.status.cloudId}", vpcObjectName, namespace.Name)
			}

			out, err := kubeCtl.Cmd(cmd)
			if err != nil {
				logf.Log.V(1).Info("Failed to get", "namespace", namespace.Name, "err", err)
				return false, nil
			}
			if expectedEmptyOutput {
				Expect(out).To(ContainSubstring("[]"))
			} else {
				Expect(out).To(Equal(expectedOutput))
			}
			return true, nil
		})
		return err
	}

	verifyCpaStatus := func(accountName string, expectError bool) error {
		err := wait.Poll(time.Second*5, time.Second*30, func() (bool, error) {
			cmd := fmt.Sprintf("get cpa %v -n %v -o json -o=jsonpath={.status.error}", accountName, namespace.Name)
			errorMsg, err := kubeCtl.Cmd(cmd)
			if err != nil {
				logf.Log.V(1).Info("Failed to get CloudProviderAccount", "namespace", namespace.Name, "err", err)
				return false, nil
			}
			if expectError {
				Expect(len(errorMsg)).ToNot(BeZero())
			}
			return true, nil
		})
		return err
	}

	BeforeEach(func() {
		if preserveSetupOnFail {
			preserveSetup = true
		}

		vmIDs = cloudVPC.GetVMs()
		vpcID = strings.ToLower(cloudVPC.GetVPCID())
		vpcName = cloudVPC.GetVPCName()
		cloudProvider = strings.Split(cloudProviders, ",")[0]
		kind = reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()

		namespace = &v1.Namespace{}
		namespace.Name = "vpc-test" + "-" + fmt.Sprintf("%x", rand.Int())
		createNS()

		// Generate kubernetes object name for vpc object.
		switch cloudProvider {
		case string(runtimev1alpha1.AWSCloudProvider):
			vpcObjectName = vpcID
		case string(runtimev1alpha1.AzureCloudProvider):
			vpcObjectName = cloudutils.GenerateShortResourceIdentifier(vpcID, vpcName)
		default:
			logf.Log.Error(nil, "invalid provider", "Provider", cloudProvider)
		}
	})

	AfterEach(func() {
		result := CurrentSpecReport()
		if result.Failed() {
			if len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for test failure.")
				fileName := utils.GenerateNameFromText(result.FullText(), testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName), cloudVPC, withAgent, withWindows)
			}
			if preserveSetupOnFail {
				logf.Log.V(1).Info("Preserve setup on failure")
				return
			}
		}
		preserveSetup = false
		// cleanup the test configuration by deleting the namespace.
		deleteNS()
	})

	It("CPA Add/Delete", func() {
		By("Configure CPA and verify VPC inventory")
		accountParams := cloudVPC.GetCloudAccountParameters("test-cloud-account"+namespace.Name, namespace.Name, false)
		err := utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, false)
		Expect(err).ToNot(HaveOccurred())

		logf.Log.V(1).Info("Get VpcID from VPC inventory", "VpcID", vpcID)
		err = verifyInventory(vpcObjectName, false, false, false, vpcID)
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to retrieve a VPC")
		logf.Log.V(1).Info("Verify VMs are not imported", "namespace", namespace.Name)
		err = verifyInventory("", false, true, true, "")
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to get vm list")

		By("Delete CPA and verify VPC inventory is not imported")
		err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, true)
		Expect(err).ToNot(HaveOccurred())
		err = verifyInventory("", true, false, true, "")
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to get VPC list")
	})
	It("CPA Add invalid/valid credentials", func() {
		By("Add Secret with invalid credentials")
		accountParams := cloudVPC.GetCloudAccountParameters("test-cloud-account"+namespace.Name, namespace.Name, true)
		// Add CPA account.
		err := utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, false)
		Expect(err).ToNot(HaveOccurred())

		// Verify error is captured in CPA status field.
		err = verifyCpaStatus(accountParams.Name, true)
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to get CPA status")

		By("Update Secret with valid credentials")
		// Update CPA account.
		accountParams = cloudVPC.GetCloudAccountParameters("test-cloud-account"+namespace.Name, namespace.Name, false)
		err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, false)
		Expect(err).ToNot(HaveOccurred())
		logf.Log.V(1).Info("Get VpcID from VPC inventory", "VpcID", vpcID)
		err = verifyInventory(vpcObjectName, false, false, false, vpcID)
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to retrieve a VPC")

		By("Delete CPA and verify VPC inventory is not imported")
		err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, true)
		Expect(err).ToNot(HaveOccurred())
		err = verifyInventory("", true, false, true, "")
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to get VPC list")
	})
	It("CES Add/Delete", func() {
		accountParams := cloudVPC.GetCloudAccountParameters("test-cloud-account"+namespace.Name, namespace.Name, false)
		err := utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, false)
		Expect(err).ToNot(HaveOccurred())

		By("Configure CES and verify VM inventory")
		entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+namespace.Name, namespace.Name, kind, nil)
		logf.Log.V(1).Info("Create CES and verify VM inventory is imported", "ces", entityParams.Name)
		err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, len(vmIDs), namespace.Name, false)
		Expect(err).ToNot(HaveOccurred())

		By("Delete CES and verify cloud inventory")
		logf.Log.V(1).Info("Delete CES and verify VM inventory is deleted", "ces", entityParams.Name)
		err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, 0, namespace.Name, true)
		Expect(err).ToNot(HaveOccurred())

		logf.Log.V(1).Info("Verify VPC inventory exists after CES delete", "VpcID", vpcID)
		err = verifyInventory(vpcObjectName, false, false, false, vpcID)
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to retrieve a VPC")
		err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, true)
		Expect(err).ToNot(HaveOccurred())
	})
	It("Restart Controller", func() {
		accountParams := cloudVPC.GetCloudAccountParameters("test-cloud-account"+namespace.Name, namespace.Name, false)
		err := utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, false)
		Expect(err).ToNot(HaveOccurred())

		By("RestartPoller Controller and verify VPC Inventory populates after restart")
		err = utils.RestartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*200, true)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(time.Second * 45)
		logf.Log.V(1).Info("Get vpcID from VPC inventory", "VpcID", vpcID)
		err = verifyInventory(vpcObjectName, false, false, false, vpcID)
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to retrieve a VPC")
		err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, true)
		Expect(err).ToNot(HaveOccurred())
	})
})
