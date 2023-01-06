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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/wait"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: Cloud Resource VPC", focusAwsDebug, focusAzureDebug), func() {

	var (
		ns = staticVMNS
	)

	BeforeEach(func() {
		logf.Log.Info("BeforeEach", "namespace", ns)
	})

	AfterEach(func() {
		result := CurrentGinkgoTestDescription()
		if result.Failed {
			if len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for test failure.")
				fileName := utils.GenerateNameFromText(result.FullTestText, testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName), cloudVPC, withAgent)
			}
			if preserveSetupOnFail {
				logf.Log.V(1).Info("Preserve setup on failure")
				return
			}
		}

		// cleanup the test configuration by deleting the namespace.
		if ns == nil {
			return
		}
		logf.Log.V(1).Info("Delete namespace", "ns", ns.Name)
		err := k8sClient.Delete(context.TODO(), ns)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Basic Test for VPC", func() {
		vmIDs := cloudVPC.GetVMs()
		vpcID := cloudVPC.GetVPCID()
		cloudProviders
		vpcName := cloudVPC.GetVPCName()
		kind := reflect.TypeOf(v1alpha1.VirtualMachine{}).Name()
		logf.Log.Info("Debug:", "vmIDs", vmIDs, "vcpID", vpcID, "vpcName", vpcName)
		// Create a Namespace
		if len(ns.Status.Phase) == 0 {
			// Create ns if not already present.
			ns.Name = "test-cloud-e2e-" + fmt.Sprintf("%x", rand.Int())
			ns.Labels = map[string]string{"name": ns.Name}
			err := k8sClient.Create(context.TODO(), ns)
			Expect(err).ToNot(HaveOccurred())
			logf.Log.V(1).Info("Created namespace", "ns", ns.Name)
		}

		logf.Log.V(1).Info("Configure and create CPA")
		accountParams := cloudVPC.GetCloudAccountParameters("test-cloud-account"+ns.Name, ns.Name, cloudCluster)
		err := utils.AddCloudAccount(kubeCtl, accountParams)
		Expect(err).ToNot(HaveOccurred())

		logf.Log.V(1).Info("Get vpcID of VM Cluster", "vpcID", vpcID)
		err = wait.Poll(time.Second*5, time.Second*30, func() (bool, error) {
			// Fixme: Azure Generate hash for resource ID and update VPCId
			cmd := fmt.Sprintf("get vpc -A -o json")
			out, err := kubeCtl.Cmd(cmd)
			logf.Log.V(1).Info("VPC list", "vpcs", out)
			cmd = fmt.Sprintf("get vpc %v -n %v -o json -o=jsonpath={.spec.Id}", vpcID, ns.Name)
			out, err = kubeCtl.Cmd(cmd)
			logf.Log.V(1).Info("Got VPC", "vpcId", out)
			if err != nil {
				logf.Log.V(1).Info("Failed to get vpc", "vpcID", vpcID, "err", err)
				return false, nil
			}
			Expect(out).To(Equal(vpcID))
			return true, nil
		})
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to retrieve vpc")

		// Configure CES.
		entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+ns.Name, ns.Name, kind)
		logf.Log.V(1).Info("Create CES and verify VM inventory", "ces", entityParams.Name)
		err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, len(vmIDs), ns.Name, false)
		Expect(err).ToNot(HaveOccurred())

		logf.Log.V(1).Info("Delete CES and verify VM inventory should be deleted", "ces", entityParams.Name)
		err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, 0, ns.Name, true)
		Expect(err).ToNot(HaveOccurred())

		logf.Log.V(1).Info("Get vpcID of VM Cluster after CES deletion", "vpcID", vpcID)
		err = wait.Poll(time.Second*5, time.Second*30, func() (bool, error) {
			cmd := fmt.Sprintf("get vpc %v -n %v -o json -o=jsonpath={.spec.Id}", vpcID, ns.Name)
			out, err := kubeCtl.Cmd(cmd)
			if err != nil {
				logf.Log.V(1).Info("Failed to get vpc", "vpcID", vpcID, "err", err)
				return false, nil
			}
			Expect(out).To(Equal(vpcID))
			return true, nil
		})
		Expect(err).ToNot(HaveOccurred(), "timeout waiting to retrieve vpc")
	})

})
