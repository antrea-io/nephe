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

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	k8stemplates "antrea.io/nephe/test/templates"
	"antrea.io/nephe/test/utils"
)

var (
	configANPApplyTo func(kind, instanceName, vpc, tagKey, tagVal string) *k8stemplates.EntitySelectorParameters
	configANPToFrom  func(kind, instanceName, vpc, tagKey, tagVal, ipBlock, nsName string, ports []string,
		denyAll bool) *k8stemplates.ToFromParameters
)

var _ = Describe(fmt.Sprintf("%s,%s: NetworkPolicy On Cloud Resources", focusAws, focusCloud), func() {
	const (
		apachePort = "8080"
	)
	var (
		namespace      *v1.Namespace
		otherNamespace *v1.Namespace
		anpParams      k8stemplates.ANPParameters
		anpSetupParams k8stemplates.ANPParameters

		importAfterANP bool
		abbreviated    bool
	)

	BeforeEach(func() {
		if preserveSetupOnFail {
			preserveSetup = true
		}
		importAfterANP = false
		abbreviated = false
	})

	AfterEach(func() {
		var err error
		bundleCollected := false
		result := CurrentGinkgoTestDescription()
		if result.Failed {
			if len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for test failure.")
				fileName := utils.GenerateNameFromText(result.FullTestText, testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName))
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
				fileName := utils.GenerateNameFromText(result.FullTestText, testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName))
			}

			nss := []*v1.Namespace{namespace, otherNamespace}
			for _, ns := range nss {
				if ns == nil {
					continue
				}
				logf.Log.V(1).Info("Delete namespace", "ns", ns.Name)
				err := k8sClient.Delete(context.TODO(), ns)
				Expect(err).ToNot(HaveOccurred())
			}
			preserveSetup = false
		}()

		// Explicitly remove ANP before delete namespace.
		err = utils.ConfigureK8s(kubeCtl, anpSetupParams, k8stemplates.CloudAntreaNetworkPolicy, true)
		Expect(err).ToNot(HaveOccurred())
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, true)
		Expect(err).ToNot(HaveOccurred())
		err = utils.CheckCloudResourceNetworkPolicies(k8sClient, reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), namespace.Name,
			cloudVPC.GetVMs(), nil)
		Expect(err).ToNot(HaveOccurred())

	})

	configANPApplyTo = func(kind, instanceName, vpc, tagKey, tagVal string) *k8stemplates.EntitySelectorParameters {
		ret := &k8stemplates.EntitySelectorParameters{
			Kind:              strings.ToLower(kind),
			VPC:               strings.ToLower(vpc),
			CloudInstanceName: strings.ToLower(instanceName),
		}
		if len(tagKey) > 0 {
			ret.Tags = map[string]string{strings.ToLower(tagKey): strings.ToLower(tagVal)}
		}
		return ret
	}

	configANPToFrom = func(kind, instanceName, vpc, tagKey, tagVal, ipBlock, nsName string, ports []string,
		denyAll bool) *k8stemplates.ToFromParameters {
		ret := &k8stemplates.ToFromParameters{
			IPBlock: ipBlock,
			DenyAll: denyAll,
		}
		if len(kind) > 0 {
			ret.Entity = &k8stemplates.EntitySelectorParameters{
				Kind:              strings.ToLower(kind),
				VPC:               strings.ToLower(vpc),
				CloudInstanceName: strings.ToLower(instanceName),
			}

			if len(tagKey) > 0 {
				ret.Entity.Tags = map[string]string{strings.ToLower(tagKey): strings.ToLower(tagVal)}
			}
		}

		for _, p := range ports {
			ret.Ports = append(ret.Ports, &k8stemplates.PortParameters{Protocol: string(v1.ProtocolTCP), Port: p})
		}

		if len(nsName) > 0 && nsName != namespace.Name {
			ret.Namespace = &k8stemplates.NamespaceParameters{
				Labels: map[string]string{"name": nsName},
			}
		}
		return ret
	}

	setup := func(kind string, num int, diffNS bool, allowedPorts []string, denyEgress bool) {
		namespace = &v1.Namespace{}
		otherNamespace = &v1.Namespace{}
		if !diffNS {
			otherNamespace = nil
		}

		nss := []*v1.Namespace{namespace, otherNamespace}
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
			accountParams := cloudVPC.GetCloudAccountParameters("test-cloud-account"+ns.Name, ns.Name, cloudCluster)
			err = utils.AddCloudAccount(kubeCtl, accountParams)
			Expect(err).ToNot(HaveOccurred())

			if !importAfterANP {
				entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+ns.Name, ns.Name, kind)
				err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, num, ns.Name, false)
				Expect(err).ToNot(HaveOccurred())
			}
		}

		anpParams = k8stemplates.ANPParameters{
			Name:      "test-cloud-anp",
			Namespace: namespace.Name,
		}

		// Setup policies for all VMs
		anpSetupParams = k8stemplates.ANPParameters{
			Name:      "test-cloud-setup-anp",
			Namespace: namespace.Name,
		}
		vmKind := reflect.TypeOf(v1alpha1.VirtualMachine{}).Name()
		anpSetupParams.AppliedTo = configANPApplyTo(vmKind, "", "", "", "")
		anpSetupParams.From = configANPToFrom("", "", "", "", "",
			"0.0.0.0/0", "", allowedPorts, false)
		if denyEgress {
			anpSetupParams.To = configANPToFrom("", "", "", "", "", "", "", nil, denyEgress)
		} else {
			anpSetupParams.To = configANPToFrom("", "", "", "", "", "0.0.0.0/0", "", nil, false)

		}
		err := utils.ConfigureK8s(kubeCtl, anpSetupParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		if importAfterANP {
			for _, ns := range nss {
				if ns == nil {
					continue
				}
				_ = cloudVPC.GetCloudAccountParameters("test-cloud-account"+ns.Name, ns.Name, cloudCluster)
				entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+ns.Name, ns.Name, kind)
				err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, num, ns.Name, false)
				Expect(err).ToNot(HaveOccurred())
			}
		}
		err = utils.CheckCloudResourceNetworkPolicies(k8sClient, vmKind, namespace.Name, cloudVPC.GetVMs(), []string{anpSetupParams.Name})
		Expect(err).ToNot(HaveOccurred())
		oks := make([]bool, len(cloudVPC.GetVMs()))
		for i := range oks {
			oks[i] = true
		}
		err = utils.ExecuteCmds(cloudVPC, nil, cloudVPC.GetVMs(), "", [][]string{{"ls"}}, oks, 30)
		Expect(err).ToNot(HaveOccurred())
	}

	verifyAppliedTo := func(kind string, ids, ips []string, srcVM, srcIP string, applied []bool) {
		// Applied ANP and check configuration.
		err := utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		for i, ok := range applied {
			var np []string
			if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
				np = append(np, anpSetupParams.Name)
			}
			if ok {
				np = append(np, anpParams.Name)
			}
			err = utils.CheckCloudResourceNetworkPolicies(k8sClient, kind, namespace.Name, []string{ids[i]}, np)
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

	verifyEgress := func(kind, id, srcVM string, dstIPs []string, oks []bool) {
		// Applied ANP and check configuration.
		err := utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		var np []string
		if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
			np = append(np, anpSetupParams.Name)
		}
		np = append(np, anpParams.Name)
		err = utils.CheckCloudResourceNetworkPolicies(k8sClient, kind, namespace.Name, []string{id}, np)
		Expect(err).ToNot(HaveOccurred())

		err = utils.ExecuteCurlCmds(cloudVPC, nil, []string{srcVM}, "", dstIPs, apachePort, oks, 30)
		Expect(err).ToNot(HaveOccurred())
	}

	verifyIngress := func(kind, id, ip string, srcVMs []string, oks []bool, verifyOnly bool) {
		// Applied ANP and check configuration.
		var err error
		if !verifyOnly {
			err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
			Expect(err).ToNot(HaveOccurred())
		}
		var np []string
		if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
			np = append(np, anpSetupParams.Name)
		}
		np = append(np, anpParams.Name)
		err = utils.CheckCloudResourceNetworkPolicies(k8sClient, kind, namespace.Name, []string{id}, np)
		Expect(err).ToNot(HaveOccurred())

		err = utils.ExecuteCurlCmds(cloudVPC, nil, srcVMs, "", []string{ip}, apachePort, oks, 30)
		Expect(err).ToNot(HaveOccurred())
	}

	testAppliedTo := func(kind string, diffNS bool) {
		var ids []string
		var ips []string
		tagTest := true
		if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}
		srcVM := cloudVPC.GetVMs()[0]
		srcIP := cloudVPC.GetVMPrivateIPs()[0]

		setup(kind, len(ids), diffNS, []string{"22"}, false)
		dstNsName := namespace.Name
		if diffNS {
			dstNsName = otherNamespace.Name
		}

		appliedIdx := len(ids) - 1
		anpParams.From = configANPToFrom(kind, "", "", "", "", "", dstNsName, []string{apachePort}, false)

		By(fmt.Sprintf("Applied NetworkPolicy to %v by kind label selector", kind))
		applied := make([]bool, len(ids))
		for i := range applied {
			applied[i] = true
		}
		anpParams.AppliedTo = configANPApplyTo(kind, "", "", "", "")
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

		By(fmt.Sprintf("Applied NetworkPolicy to %v by name label selector", kind))
		applied = make([]bool, len(ids))
		applied[appliedIdx] = true
		anpParams.AppliedTo = configANPApplyTo(kind, ids[appliedIdx], "", "", "")
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

		if abbreviated {
			return
		}

		By(fmt.Sprintf("Applied NetworkPolicy to %v by VPC label selector", kind))
		for i := range applied {
			applied[i] = true
		}
		anpParams.AppliedTo = configANPApplyTo(kind, "", cloudVPC.GetCRDVPCID(), "", "")
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

		if tagTest {
			By(fmt.Sprintf("Applied NetworkPolicy to %v by tag label selector", kind))
			applied = make([]bool, len(ids))
			applied[appliedIdx] = true
			anpParams.AppliedTo = configANPApplyTo(kind, "", "", "name", cloudVPC.GetTags()[appliedIdx]["Name"])
			verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)
		}
	}

	testEgress := func(kind string, diffNS bool) {
		var ids []string
		var ips []string
		testTag := true
		if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}

		setup(kind, len(ids), diffNS, []string{"22", "8080"}, true)
		dstNsName := namespace.Name
		if diffNS {
			dstNsName = otherNamespace.Name
		}

		appliedIdx := len(ids) - 1
		srcVM := cloudVPC.GetVMs()[appliedIdx]
		anpParams.AppliedTo = configANPApplyTo(kind, ids[appliedIdx], "", "", "")

		By(fmt.Sprintf("Egress NetworkPolicy on %v by kind label selector", kind))
		oks := make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		anpParams.To = configANPToFrom(kind, "", "", "", "", "", dstNsName,
			[]string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks)

		By(fmt.Sprintf("Egress NetworkPolicy on %v by name label selector", kind))
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.To = configANPToFrom(kind, ids[0], "", "", "", "", dstNsName,
			[]string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks)

		if abbreviated {
			return
		}

		By(fmt.Sprintf("Egress NetworkPolicy on %v by VPC label selector", kind))
		for i := range oks {
			oks[i] = true
		}
		anpParams.To = configANPToFrom(kind, "", cloudVPC.GetCRDVPCID(), "", "", "", dstNsName,
			[]string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks)

		if testTag {
			By(fmt.Sprintf("Egress NetworkPolicy on %v by tag label selector", kind))
			oks = make([]bool, len(ids)-1)
			oks[0] = true
			anpParams.To = configANPToFrom(kind, "", "", "name", cloudVPC.GetTags()[0]["Name"], "", dstNsName,
				[]string{apachePort}, false)
			verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks)
		}
		By(fmt.Sprintf("Egress NetworkPolicy on %v by IPBlock", kind))
		oks = make([]bool, len(ids)-1)
		oks[1] = true
		anpParams.To = configANPToFrom("", "", "", "", "", ips[1]+"/32", dstNsName,
			[]string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks)
	}

	testIngress := func(kind string, diffNS bool) {
		var ids []string
		var ips []string
		testTag := true
		if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}

		setup(kind, len(ids), diffNS, []string{"22"}, false)
		dstNsName := namespace.Name
		if diffNS {
			dstNsName = otherNamespace.Name
		}

		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]
		anpParams.AppliedTo = configANPApplyTo(kind, ids[appliedIdx], "", "", "")

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by kind label selector", kind))
		oks := make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = configANPToFrom(kind, "", "", "", "", "", dstNsName,
			[]string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false)

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by name label selector", kind))
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.From = configANPToFrom(kind, ids[0], "", "", "", "", dstNsName,
			[]string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false)

		if abbreviated {
			return
		}

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by VPC label selector", kind))
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = configANPToFrom(kind, "", cloudVPC.GetCRDVPCID(), "", "", "", dstNsName,
			[]string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false)

		if testTag {
			By(fmt.Sprintf("Ingress NetworkPolicy on %v by tag label selector", kind))
			oks = make([]bool, len(ids)-1)
			oks[0] = true
			anpParams.From = configANPToFrom(kind, "", "", "name", cloudVPC.GetTags()[0]["Name"], "", dstNsName,
				[]string{apachePort}, false)
			verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false)
		}

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by IPBlock", kind))
		oks = make([]bool, len(ids)-1)
		oks[1] = true
		anpParams.From = configANPToFrom("", "", "", "", "", ips[1]+"/32", dstNsName,
			[]string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false)
	}

	table.DescribeTable("AppliedTo",
		func(kind string, diffNS bool) {
			testAppliedTo(kind, diffNS)
		},
		table.Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAzure),
			reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), false),
		table.Entry("VM In Different Namespaces",
			reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), true),
	)

	table.DescribeTable("Egress",
		func(kind string, diffNS bool) {
			testEgress(kind, diffNS)
		},
		table.Entry("VM In Same Namespace",
			reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), false),
		table.Entry("VM In Different Namespaces",
			reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), true),
	)

	table.DescribeTable("Ingress",
		func(kind string, diffNS bool) {
			testIngress(kind, diffNS)
		},
		table.Entry(fmt.Sprintf("%s: VM In Same Namespaces", focusAzure),
			reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), false),
		table.Entry("VM In Different Namespaces",
			reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), true),
	)

	Context("Enforce Before Import", func() {
		JustBeforeEach(func() {
			importAfterANP = true
			abbreviated = true
		})
		table.DescribeTable("AppliedTo",
			func(kind string, diffNS bool) {
				testAppliedTo(kind, diffNS)
			},
			table.Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAzure),
				reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), false),
			table.Entry("VM In Different Namespaces",
				reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), true),
		)

		table.DescribeTable("Egress",
			func(kind string, diffNS bool) {
				testEgress(kind, diffNS)
			},
			table.Entry("VM In Same Namespace",
				reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), false),
			table.Entry("VM In Different Namespaces",
				reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), true),
		)

		table.DescribeTable("Ingress",
			func(kind string, diffNS bool) {
				testIngress(kind, diffNS)
			},
			table.Entry(fmt.Sprintf("%s: VM In Same Namespaces", focusAzure),
				reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), false),
			table.Entry("VM In Different Namespaces",
				reflect.TypeOf(v1alpha1.VirtualMachine{}).Name(), true),
		)
	})

	It("Reconcile with cloud", func() {
		ids := cloudVPC.GetVMs()
		ips := cloudVPC.GetVMPrivateIPs()
		kind := reflect.TypeOf(v1alpha1.VirtualMachine{}).Name()
		setup(kind, len(ids), false, []string{"22"}, false)
		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]

		var replicas int32
		var err error

		By("New NetworkPolicy")
		replicas, err = utils.StopDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		anpParams.AppliedTo = configANPApplyTo(kind, ids[appliedIdx], "", "", "")
		oks := make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.From = configANPToFrom(kind, ids[0], "", "", "", "", namespace.Name,
			[]string{apachePort}, false)
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		err = utils.StartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", replicas, time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		// wait for aggregated api server to ready.
		err = utils.WaitApiServer(k8sClient, time.Second*60)
		Expect(err).NotTo(HaveOccurred())
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true)

		By("Changed NetworkPolicy")
		replicas, err = utils.StopDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		oks = make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = configANPToFrom(kind, "", "", "", "", "", namespace.Name,
			[]string{apachePort}, false)
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		err = utils.StartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", replicas, time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		err = utils.WaitApiServer(k8sClient, time.Second*60)
		Expect(err).NotTo(HaveOccurred())
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true)

		By("Stale NetworkPolicy")
		replicas, err = utils.StopDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		oks = make([]bool, len(ids)-1)
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, true)
		Expect(err).ToNot(HaveOccurred())
		err = utils.StartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", replicas, time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		err = utils.WaitApiServer(k8sClient, time.Second*60)
		Expect(err).NotTo(HaveOccurred())
		err = utils.CheckCloudResourceNetworkPolicies(k8sClient, kind, namespace.Name, ids, []string{anpSetupParams.Name})
		Expect(err).ToNot(HaveOccurred())
		err = utils.ExecuteCurlCmds(cloudVPC, nil, srcVMs, "", []string{ids[appliedIdx]}, apachePort, oks, 30)
		Expect(err).ToNot(HaveOccurred())
		// Add ANP back to satisfy AfterEach
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Controllers Restart", func() {
		ids := cloudVPC.GetVMs()
		ips := cloudVPC.GetVMPrivateIPs()

		kind := reflect.TypeOf(v1alpha1.VirtualMachine{}).Name()
		setup(kind, len(ids), false, []string{"22"}, false)
		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]

		oks := make([]bool, len(ids)-1)
		var err error
		By(fmt.Sprintf("Ingress NetworkPolicy on %v by kind label selector while restarting Antrea controller", kind))
		anpParams.AppliedTo = configANPApplyTo(kind, ids[appliedIdx], "", "", "")
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = configANPToFrom(kind, "", "", "", "", "", namespace.Name,
			[]string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false)
		err = utils.RestartOrWaitDeployment(k8sClient, "antrea-controller", "kube-system", time.Second*200, true)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(time.Second * 30)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true)

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by name label selector while restarting Nephe controller", kind))
		anpParams.AppliedTo = configANPApplyTo(kind, ids[appliedIdx], "", "", "")
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.From = configANPToFrom(kind, ids[0], "", "", "", "", namespace.Name,
			[]string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false)
		By("Restarting controllers now...")
		err = utils.RestartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*200, true)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(time.Second * 30)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true)
	})
})
