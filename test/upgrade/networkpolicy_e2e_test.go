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

package upgrade

import (
	"context"
	"fmt"
	"math/rand"
	"path"
	"reflect"
	"strings"
	"time"

	k8stemplates "antrea.io/nephe/test/templates"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: NetworkPolicy upgrade test", focusAws, focusAzure), func() {
	const (
		apachePort = "8080"
	)
	var (
		namespace            *v1.Namespace
		accountParams        k8stemplates.CloudAccountParameters
		anpParams            k8stemplates.ANPParameters
		anpSetupParams       k8stemplates.ANPParameters
		defaultANPParameters k8stemplates.DefaultANPParameters
		defaultDenyANP       bool

		anpPriority        = 10
		defaultAnpPriority = 20
	)

	BeforeEach(func() {
		defaultDenyANP = false
		if preserveSetupOnFail {
			preserveSetup = true
		}
		// Apply Helm Chart.
		logf.Log.Info("Installing Helm Chart", "version", fromVersion)
		err := utils.InstallHelmChart(k8sClient, fromVersion)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		var err error
		bundleCollected := false
		result := CurrentSpecReport()
		if result.Failed() {
			if len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for test failure.")
				fileName := utils.GenerateNameFromText(result.FullText(), testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName), cloudVPC, false, false)
				bundleCollected = true
			}
			if preserveSetupOnFail {
				logf.Log.V(1).Info("Preserve setup on failure")
				return
			}
		}

		defer func() {
			if !bundleCollected && err != nil && len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for clean-up failure.")
				fileName := utils.GenerateNameFromText(result.FullText(), testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName), cloudVPC, false, false)
			}

			// Don't delete the chart if preserveSetup is true.
			if preserveSetup == false {
				logf.Log.Info("Deleting Helm Chart")
				err = utils.DeleteHelmChart()
				Expect(err).ToNot(HaveOccurred())
			}
		}()

		// Explicitly remove ANP before delete namespace.
		err = utils.ConfigureK8s(kubeCtl, anpSetupParams, k8stemplates.CloudAntreaNetworkPolicy, true)
		Expect(err).ToNot(HaveOccurred())
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, true)
		Expect(err).ToNot(HaveOccurred())
		if defaultDenyANP {
			err = utils.ConfigureK8s(kubeCtl, defaultANPParameters, k8stemplates.DefaultANPSetup, true)
			Expect(err).ToNot(HaveOccurred())
		}
		err = utils.CheckCloudResourceNetworkPolicies(kubeCtl, k8sClient, reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), namespace.Name,
			cloudVPC.GetVMs(), nil, false)
		Expect(err).ToNot(HaveOccurred())

		// Remove account and secret.
		err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, true)
		Expect(err).ToNot(HaveOccurred())

		// Namespace deletion will remove cloud accounts.
		err = k8sClient.Delete(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(time.Second * 60)

		// Check for Controller restart
		err = utils.CheckRestart(kubeCtl)
		Expect(err).ToNot(HaveOccurred(), "controller restart detected.")

		preserveSetup = false
	})

	setup := func(kind string, num int, allowedPorts []string, denyEgress bool) {
		namespace = &v1.Namespace{}
		nss := []*v1.Namespace{namespace}
		for _, ns := range nss {
			if ns == nil {
				continue
			}
			var err error
			if len(ns.Status.Phase) == 0 {
				// Create ns if not already present.
				ns.Name = "test-cloud-e2e-" + fmt.Sprintf("%x", rand.Int())
				ns.Labels = map[string]string{"name": ns.Name}
				err = k8sClient.Create(context.TODO(), ns)
				Expect(err).ToNot(HaveOccurred())
				logf.Log.V(1).Info("Created namespace", "ns", ns.Name)
			}
			accountParams = cloudVPC.GetCloudAccountParameters("test-cloud-account"+ns.Name, ns.Name, false)
			err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, false)
			Expect(err).ToNot(HaveOccurred())

			entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+ns.Name, ns.Name, kind, nil)
			entityParams.Agented = false
			err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, num, ns.Name, false)
			Expect(err).ToNot(HaveOccurred())
		}

		anpParams = k8stemplates.ANPParameters{
			Name:      "test-cloud-anp",
			Namespace: namespace.Name,
			Priority:  anpPriority,
		}

		// Setup policies for all VMs
		anpSetupParams = k8stemplates.ANPParameters{
			Name:      "test-cloud-setup-anp",
			Namespace: namespace.Name,
			Priority:  anpPriority,
		}
		vmKind := reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()
		anpSetupParams.AppliedTo = utils.ConfigANPApplyTo(vmKind, "", "", "", "")
		anpSetupParams.From = utils.ConfigANPToFrom("", "", "", "", "",
			"0.0.0.0/0", "", allowedPorts, false)
		if denyEgress {
			anpSetupParams.To = utils.ConfigANPToFrom("", "", "", "", "", "", "", nil, denyEgress)
		} else {
			anpSetupParams.To = utils.ConfigANPToFrom("", "", "", "", "", "0.0.0.0/0", "", nil, false)

		}
		err := utils.ConfigureK8s(kubeCtl, anpSetupParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		err = utils.CheckCloudResourceNetworkPolicies(
			kubeCtl, k8sClient, vmKind, namespace.Name, cloudVPC.GetVMs(), []string{anpSetupParams.Name}, false,
		)
		Expect(err).ToNot(HaveOccurred())
		oks := make([]bool, len(cloudVPC.GetVMs()))
		for i := range oks {
			oks[i] = true
		}
		err = utils.ExecuteCmds(cloudVPC, nil, cloudVPC.GetVMs(), "", [][]string{{"ls"}}, oks, 30)
		Expect(err).ToNot(HaveOccurred())
	}

	verifyAppliedTo := func(kind string, ids, ips []string, srcVM, srcIP string, applied []bool) {
		// Apply ANP and check configuration.
		err := utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		for i, ok := range applied {
			var np []string
			if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
				np = append(np, anpSetupParams.Name)
				if defaultDenyANP == true {
					np = append(np, defaultANPParameters.Name)
				}
			}
			if ok {
				np = append(np, anpParams.Name)
			}
			err = utils.CheckCloudResourceNetworkPolicies(kubeCtl, k8sClient, kind, namespace.Name, []string{ids[i]}, np, false)
			Expect(err).ToNot(HaveOccurred())
		}
		appliedIPs := make([]string, 0)
		notAppliedIPs := make([]string, 0)
		oks := make([]bool, 0)
		for i, a := range applied {
			if ips[i] == srcIP {
				continue
			}
			if a {
				appliedIPs = append(appliedIPs, ips[i])
				oks = append(oks, true)
			} else {
				notAppliedIPs = append(notAppliedIPs, ips[i])
			}
		}
		if len(appliedIPs) > 0 {
			err = utils.ExecuteCurlCmds(cloudVPC, nil, []string{srcVM}, "", appliedIPs, apachePort, oks, 30)
			Expect(err).ToNot(HaveOccurred())
		}
		if len(notAppliedIPs) > 0 {
			oks = make([]bool, len(notAppliedIPs))
			err = utils.ExecuteCurlCmds(cloudVPC, nil, []string{srcVM}, "", notAppliedIPs, apachePort, oks, 30)
			Expect(err).ToNot(HaveOccurred())
		}
	}

	testAppliedTo := func(kind string) {
		ids := cloudVPC.GetVMs()
		ips := cloudVPC.GetVMPrivateIPs()
		setup(kind, len(ids), []string{"22"}, false)

		// Add explicit deny-all rule.
		if defaultDenyANP {
			defaultANPParameters = k8stemplates.DefaultANPParameters{
				Namespace: namespace.Name,
				Name:      "deny-all",
				Priority:  defaultAnpPriority,
				Entity: &k8stemplates.EntitySelectorParameters{
					Kind: labels.ExternalEntityLabelKeyKind + ": " + strings.ToLower(reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
				},
			}
			err := utils.ConfigureK8s(kubeCtl, defaultANPParameters, k8stemplates.DefaultANPSetup, false)
			Expect(err).ToNot(HaveOccurred(), "failed to add default deny ANP rule")
		}

		srcVM := cloudVPC.GetVMs()[0]
		srcIP := cloudVPC.GetVMPrivateIPs()[0]
		dstNsName := namespace.Name

		appliedIdx := len(ids) - 1
		anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", dstNsName, []string{apachePort}, false)

		By(fmt.Sprintf("Applied NetworkPolicy to %v by kind label selector", kind))
		applied := make([]bool, len(ids))
		for i := range applied {
			applied[i] = true
		}
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, "", "", "", "")
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

		By(fmt.Sprintf("Applied NetworkPolicy to %v by name label selector", kind))
		applied = make([]bool, len(ids))
		applied[appliedIdx] = true
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

		By(fmt.Sprintf("Applied NetworkPolicy to %v by VPC label selector", kind))
		for i := range applied {
			applied[i] = true
		}
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, "", cloudVPC.GetCRDVPCID(), "", "")
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

		By(fmt.Sprintf("Applied NetworkPolicy to %v by tag label selector", kind))
		applied = make([]bool, len(ids))
		applied[appliedIdx] = true
		key := "Name"
		if value, found := cloudVPC.GetTags()[appliedIdx][key]; found {
			anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, "", "", key, value)
			verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)
		}
	}

	DescribeTable("AppliedTo Before Upgrade",
		func(kind string) {
			testAppliedTo(kind)
		},
		Entry("VM In Same Namespace",
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("AppliedTo After Upgrade",
		func(kind string) {
			By(fmt.Sprintf("Upgrading Helm Chart to %s.", toVersion))
			err := utils.UpgradeHelmChart(k8sClient, toVersion, chartDir)
			Expect(err).ToNot(HaveOccurred())

			if cloudProvider == "Azure" {
				defaultDenyANP = true
			}
			By("Test AppliedTo after upgrade.")
			testAppliedTo(kind)
		},
		Entry("VM In Same Namespace",
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("AppliedTo Upgrade with ANPs configured",
		func(kind string) {
			ids := cloudVPC.GetVMs()
			ips := cloudVPC.GetVMPrivateIPs()
			setup(kind, len(ids), []string{"22"}, false)

			srcVM := cloudVPC.GetVMs()[0]
			srcIP := cloudVPC.GetVMPrivateIPs()[0]
			dstNsName := namespace.Name
			anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", dstNsName, []string{apachePort}, false)

			By(fmt.Sprintf("Applied NetworkPolicy to %v by kind label selector", kind))
			applied := make([]bool, len(ids))
			for i := range applied {
				applied[i] = true
			}
			anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, "", "", "", "")
			verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

			By(fmt.Sprintf("Upgrading Helm Chart to %s.", toVersion))
			err := utils.UpgradeHelmChart(k8sClient, toVersion, chartDir)
			Expect(err).ToNot(HaveOccurred())

			if cloudProvider == "Azure" {
				defaultANPParameters = k8stemplates.DefaultANPParameters{
					Namespace: namespace.Name,
					Name:      "deny-all",
					Priority:  defaultAnpPriority,
					Entity: &k8stemplates.EntitySelectorParameters{
						Kind: labels.ExternalEntityLabelKeyKind + ": " + strings.ToLower(reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
					},
				}
				err := utils.ConfigureK8s(kubeCtl, defaultANPParameters, k8stemplates.DefaultANPSetup, false)
				Expect(err).ToNot(HaveOccurred(), "failed to add default deny ANP rule")
				defaultDenyANP = true
			}
			verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)
		},
		Entry("VM In Same Namespace",
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)
})
