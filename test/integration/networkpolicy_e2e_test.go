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

	cpv1beta2 "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cpautils "antrea.io/nephe/pkg/cloudprovider/utils"
	"antrea.io/nephe/pkg/labels"
	k8stemplates "antrea.io/nephe/test/templates"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: NetworkPolicy On Cloud Resources", focusAws, focusCloud), func() {
	const (
		apachePort = "8080"
	)
	var (
		namespace            *v1.Namespace
		accountParams        k8stemplates.CloudAccountParameters
		anpParams            k8stemplates.ANPParameters
		anpSetupParams       k8stemplates.ANPParameters
		defaultANPParameters k8stemplates.DefaultANPParameters

		importAfterANP bool
		abbreviated    bool

		anpPriority        = 10
		defaultAnpPriority = 20
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
		result := CurrentSpecReport()
		if result.Failed() {
			if len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for test failure.")
				fileName := utils.GenerateNameFromText(result.FullText(), testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName), cloudVPC, withAgent, withWindows)
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
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName), cloudVPC, withAgent, withWindows)
			}

			if !withAgent {
				nss := []*v1.Namespace{namespace}
				for _, ns := range nss {
					if ns == nil {
						continue
					}
					logf.Log.V(1).Info("Delete namespace", "ns", ns.Name)
					err := k8sClient.Delete(context.TODO(), ns)
					Expect(err).ToNot(HaveOccurred())
				}
			}
			preserveSetup = false
		}()

		// Explicitly remove ANP before delete namespace.
		err = utils.ConfigureK8s(kubeCtl, anpSetupParams, k8stemplates.CloudAntreaNetworkPolicy, true)
		Expect(err).ToNot(HaveOccurred())
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, true)
		Expect(err).ToNot(HaveOccurred())
		if !withAgent && cloudProviders == "Azure" {
			err = utils.ConfigureK8s(kubeCtl, defaultANPParameters, k8stemplates.DefaultANPSetup, true)
			Expect(err).ToNot(HaveOccurred())
		}
		err = utils.CheckCloudResourceNetworkPolicies(kubeCtl, k8sClient, reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), namespace.Name,
			cloudVPC.GetVMs(), nil, withAgent)
		Expect(err).ToNot(HaveOccurred())
		err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, true)
		Expect(err).ToNot(HaveOccurred())
	})

	setup := func(kind string, num int, allowedPorts []string, denyEgress bool) {
		namespace = &v1.Namespace{}
		if withAgent {
			namespace = staticVMNS
		}

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

			if !importAfterANP {
				entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+ns.Name, ns.Name, kind, nil)
				entityParams.Agented = withAgent
				err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, num, ns.Name, false)
				Expect(err).ToNot(HaveOccurred())
			}
		}

		if withAgent {
			// To Keep tests happy, add default ingress/egress deny rule
			// TODO: Update the tests later to remove this rule
			defaultANPParameters = k8stemplates.DefaultANPParameters{
				Namespace: staticVMNS.Name,
				Name:      "deny-8080",
				Entity: &k8stemplates.EntitySelectorParameters{
					Kind: labels.ExternalEntityLabelKeyKind + ": " + strings.ToLower(reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
				},
			}
			err := utils.ConfigureK8s(kubeCtl, defaultANPParameters, k8stemplates.DefaultANPAgentedSetup, false)
			Expect(err).ToNot(HaveOccurred(), "failed to add default ANP rule")
		} else {
			if cloudProviders == "Azure" {
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
			"0.0.0.0/0", "", cpv1beta2.ProtocolTCP, allowedPorts, false)
		if denyEgress {
			anpSetupParams.To = utils.ConfigANPToFrom("", "", "", "", "", "", "", cpv1beta2.ProtocolTCP, nil, denyEgress)
		} else {
			anpSetupParams.To = utils.ConfigANPToFrom("", "", "", "", "", "0.0.0.0/0", "", cpv1beta2.ProtocolTCP, nil, false)

		}
		err := utils.ConfigureK8s(kubeCtl, anpSetupParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		if importAfterANP {
			for _, ns := range nss {
				if ns == nil {
					continue
				}
				_ = cloudVPC.GetCloudAccountParameters("test-cloud-account"+ns.Name, ns.Name, false)
				entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+ns.Name, ns.Name, kind, nil)
				entityParams.Agented = withAgent
				err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, num, ns.Name, false)
				Expect(err).ToNot(HaveOccurred())
			}
		}

		verifyAntreaAgentConnection := func(vm string) {
			err := wait.Poll(time.Second*20, time.Second*600, func() (bool, error) {
				cmd := fmt.Sprintf("get aai virtualmachine-%s -o json -o=jsonpath={.agentConditions[0].status}", vm)
				out, err := kubeCtl.Cmd(cmd)
				if err != nil {
					logf.Log.V(1).Info("Failed to get AntreaAgentInfo", "err", err)
					return false, nil
				}
				if strings.Compare(out, "True") != 0 {
					logf.Log.V(1).Info("Waiting for antrea-agent connection update", "vm", vm)
					return false, nil
				}
				return true, nil
			})
			Expect(err).ToNot(HaveOccurred(), "timeout waiting for antrea-agent connection")
		}

		// Check antrea-agent has connected to antrea-controller.
		if withAgent {
			for _, vpc := range cloudVPCs {
				for i, vm := range vpc.GetVMIDs() {
					if cloudProviders == "Azure" {
						vmName := cpautils.GenerateShortResourceIdentifier(strings.ToLower(vm), vpc.GetVMNames()[i])
						vm = vmName
					}
					verifyAntreaAgentConnection(vm)
				}
			}
		}

		np := []string{anpSetupParams.Name}
		if !withAgent && cloudProviders == "Azure" {
			np = append(np, defaultANPParameters.Name)
		}
		err = utils.CheckCloudResourceNetworkPolicies(
			kubeCtl, k8sClient, vmKind, namespace.Name, cloudVPC.GetVMs(), np, withAgent,
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
				if !withAgent && cloudProviders == "Azure" {
					np = append(np, defaultANPParameters.Name)
				}
			}
			if ok {
				np = append(np, anpParams.Name)
			}
			err = utils.CheckCloudResourceNetworkPolicies(kubeCtl, k8sClient, kind, namespace.Name, []string{ids[i]}, np, withAgent)
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

	verifyEgress := func(kind, id, srcVM string, dstIPs []string, oks []bool, ping bool) {
		// Applied ANP and check configuration.
		err := utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		var np []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			np = append(np, anpSetupParams.Name)
			if !withAgent && cloudProviders == "Azure" {
				np = append(np, defaultANPParameters.Name)
			}
		}
		np = append(np, anpParams.Name)
		err = utils.CheckCloudResourceNetworkPolicies(kubeCtl, k8sClient, kind, namespace.Name, []string{id}, np, withAgent)
		Expect(err).ToNot(HaveOccurred())

		if !ping {
			err = utils.ExecuteCurlCmds(cloudVPC, nil, []string{srcVM}, "", dstIPs, apachePort, oks, 30)
		} else {
			err = utils.ExecutePingCmds(cloudVPC, nil, []string{srcVM}, "", dstIPs, oks, 10)
		}
		Expect(err).ToNot(HaveOccurred())
	}

	verifyIngress := func(kind, id, ip string, srcVMs []string, oks []bool, verifyOnly bool, ping bool) {
		// Applied ANP and check configuration.
		var err error
		if !verifyOnly {
			err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
			Expect(err).ToNot(HaveOccurred())
		}
		var np []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			np = append(np, anpSetupParams.Name)
			if !withAgent && cloudProviders == "Azure" {
				np = append(np, defaultANPParameters.Name)
			}
		}
		np = append(np, anpParams.Name)
		err = utils.CheckCloudResourceNetworkPolicies(kubeCtl, k8sClient, kind, namespace.Name, []string{id}, np, withAgent)
		Expect(err).ToNot(HaveOccurred())

		if !ping {
			err = utils.ExecuteCurlCmds(cloudVPC, nil, srcVMs, "", []string{ip}, apachePort, oks, 30)
		} else {
			err = utils.ExecutePingCmds(cloudVPC, nil, srcVMs, "", []string{ip}, oks, 10)
		}
		Expect(err).ToNot(HaveOccurred())
	}

	testAppliedTo := func(kind string) {
		var ids []string
		var ips []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}
		srcVM := cloudVPC.GetVMs()[0]
		srcIP := cloudVPC.GetVMPrivateIPs()[0]

		setup(kind, len(ids), []string{"22"}, false)
		dstNsName := namespace.Name

		appliedIdx := len(ids) - 1
		anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", dstNsName, cpv1beta2.ProtocolTCP, []string{apachePort}, false)

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

		if abbreviated {
			return
		}

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

	testAppliedToUsingGroup := func(kind string, rule bool) {
		var ids []string
		var ips []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}

		srcVM := cloudVPC.GetVMs()[0]
		srcIP := cloudVPC.GetVMPrivateIPs()[0]

		setup(kind, len(ids), []string{"22"}, false)
		nsName := namespace.Name

		// Configure Group.
		groupParameters := k8stemplates.GroupParameters{
			Namespace: nsName,
			Name:      "test-group",
		}
		By(fmt.Sprintf("Applied NetworkPolicy to %v by kind label selector using group", kind))
		groupParameters.Entity = &k8stemplates.EntitySelectorParameters{
			Kind: labels.ExternalEntityLabelKeyKind + ": " + strings.ToLower(kind),
		}
		err := utils.ConfigureK8s(kubeCtl, groupParameters, k8stemplates.CloudAntreaGroup, false)
		Expect(err).ToNot(HaveOccurred())

		anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", nsName, cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		if rule {
			anpParams.RuleAppliedToGroup = &groupParameters
		} else {
			anpParams.AppliedToGroup = &groupParameters
		}
		applied := make([]bool, len(ids))
		for i := range applied {
			applied[i] = true
		}
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)

		By(fmt.Sprintf("Applied NetworkPolicy to %v by name label selector using group", kind))
		appliedIdx := len(ids) - 1
		applied = make([]bool, len(ids))
		applied[appliedIdx] = true

		// Update Group.
		groupParameters.Entity = &k8stemplates.EntitySelectorParameters{
			CloudInstanceName: labels.ExternalEntityLabelKeyOwnerVm + ": " + strings.ToLower(ids[appliedIdx]),
		}
		err = utils.ConfigureK8s(kubeCtl, groupParameters, k8stemplates.CloudAntreaGroup, false)
		Expect(err).ToNot(HaveOccurred())
		verifyAppliedTo(kind, ids, ips, srcVM, srcIP, applied)
	}

	testEgress := func(kind string) {
		var ids []string
		var ips []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}

		setup(kind, len(ids), []string{"22", "8080"}, true)
		dstNsName := namespace.Name

		appliedIdx := len(ids) - 1
		srcVM := cloudVPC.GetVMs()[appliedIdx]
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")

		By(fmt.Sprintf("Egress NetworkPolicy on %v by kind label selector", kind))
		oks := make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		anpParams.To = utils.ConfigANPToFrom(kind, "", "", "", "", "", dstNsName,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks, false)

		By(fmt.Sprintf("Egress NetworkPolicy on %v by name label selector", kind))
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.To = utils.ConfigANPToFrom(kind, ids[0], "", "", "", "", dstNsName,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks, false)

		if abbreviated {
			return
		}

		By(fmt.Sprintf("Egress NetworkPolicy on %v by VPC label selector", kind))
		for i := range oks {
			oks[i] = true
		}
		anpParams.To = utils.ConfigANPToFrom(kind, "", cloudVPC.GetCRDVPCID(), "", "", "", dstNsName,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks, false)

		By(fmt.Sprintf("Egress NetworkPolicy on %v by tag label selector", kind))
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		key := "Name"
		if value, found := cloudVPC.GetTags()[0][key]; found {
			anpParams.To = utils.ConfigANPToFrom(kind, "", "", key, value, "", dstNsName,
				cpv1beta2.ProtocolTCP, []string{apachePort}, false)
			verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks, false)
		}

		By(fmt.Sprintf("Egress NetworkPolicy on %v by IPBlock", kind))
		oks = make([]bool, len(ids)-1)
		oks[1] = true
		anpParams.To = utils.ConfigANPToFrom("", "", "", "", "", ips[1]+"/32", dstNsName,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, ips[:len(ips)-1], oks, false)
	}

	testEgressWithPing := func(kind string) {
		var ids []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
		} else {
			Fail("Unsupported type")
		}
		setup(kind, len(ids), []string{"22", "8080"}, true)
		dstNsName := namespace.Name

		appliedIdx := len(ids) - 1
		srcVM := cloudVPC.GetVMs()[appliedIdx]
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")

		By(fmt.Sprintf("Egress NetworkPolicy on %v by kind label selector", kind))
		oks := []bool{true}
		anpParams.To = utils.ConfigANPToFrom("", "", "", "", "", "", dstNsName,
			cpv1beta2.ProtocolICMP, []string{}, false)
		verifyEgress(kind, ids[appliedIdx], srcVM, []string{"8.8.8.8"}, oks, true)
	}

	testIngress := func(kind string) {
		var ids []string
		var ips []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}

		setup(kind, len(ids), []string{"22"}, false)
		dstNsName := namespace.Name

		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by kind label selector", kind))
		oks := make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", dstNsName,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by name label selector", kind))
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.From = utils.ConfigANPToFrom(kind, ids[0], "", "", "", "", dstNsName,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)

		if abbreviated {
			return
		}

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by VPC label selector", kind))
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = utils.ConfigANPToFrom(kind, "", cloudVPC.GetCRDVPCID(), "", "", "",
			dstNsName, cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by tag label selector", kind))
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		key := "Name"
		if value, found := cloudVPC.GetTags()[0][key]; found {
			anpParams.From = utils.ConfigANPToFrom(kind, "", "", key, value, "", dstNsName,
				cpv1beta2.ProtocolTCP, []string{apachePort}, false)
			verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)
		}

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by IPBlock", kind))
		oks = make([]bool, len(ids)-1)
		oks[1] = true
		anpParams.From = utils.ConfigANPToFrom("", "", "", "", "", ips[1]+"/32", dstNsName,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)
	}

	testIngressAllowAll := func(kind string) {
		var ids []string
		var ips []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}
		setup(kind, len(ids), []string{"22"}, false)

		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")

		By("Ingress AllowAll NetworkPolicy")
		oks := make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		// wildcard ipblock and port.
		anpParams.From = utils.ConfigANPToFrom("", "", "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolTCP, []string{}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)
	}

	testIngressWithPing := func(kind string) {
		var ids []string
		var ips []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}
		setup(kind, len(ids), []string{"22"}, false)

		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")
		oks := make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = utils.ConfigANPToFrom("", "", "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolICMP, []string{}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, true)
	}

	testIngressDenyAll := func(kind string) {
		var ids []string
		var ips []string
		if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
			ids = cloudVPC.GetVMs()
			ips = cloudVPC.GetVMPrivateIPs()
		} else {
			Fail("Unsupported type")
		}
		setup(kind, len(ids), []string{"22"}, false)

		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")

		By("Ingress AllowAll NetworkPolicy")
		oks := make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}

		// wildcard ipblock and port.
		anpParams.From = utils.ConfigANPToFrom("", "", "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolTCP, []string{}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)

		By("Make default rule higher priority")
		defaultANPParameters.Priority = 5
		err := utils.ConfigureK8s(kubeCtl, defaultANPParameters, k8stemplates.DefaultANPSetup, false)
		Expect(err).ToNot(HaveOccurred(), "failed to update default deny ANP rule")
		for i := range oks {
			oks[i] = false
		}
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)
	}

	DescribeTable("AppliedTo",
		func(kind string) {
			testAppliedTo(kind)
		},
		Entry(fmt.Sprintf("%s %s: VM In Same Namespace", focusAzure, focusAgent),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("AppliedTo using Group",
		func(kind string) {
			testAppliedToUsingGroup(kind, false)
		},
		Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAzure),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("AppliedTo at Rule using Group",
		func(kind string) {
			testAppliedToUsingGroup(kind, true)
		},
		Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAzure),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("Egress",
		func(kind string) {
			testEgress(kind)
		},
		Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAgent),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("Egress With Ping",
		func(kind string) {
			testEgressWithPing(kind)
		},
		Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAzure),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("Ingress",
		func(kind string) {
			testIngress(kind)
		},
		Entry(fmt.Sprintf("%s %s: VM In Same Namespace", focusAzure, focusAgent),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	DescribeTable("Ingress AllowAll",
		func(kind string) {
			testIngressAllowAll(kind)
		},
		Entry(fmt.Sprintf("%s %s: VM In Same Namespace", focusAzure, focusAgent),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	if cloudProviders == "Azure" {
		DescribeTable("Ingress DenyAll",
			func(kind string) {
				testIngressDenyAll(kind)
			},
			Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAzure),
				reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
		)
	}

	DescribeTable("Ingress Test WithPing",
		func(kind string) {
			testIngressWithPing(kind)
		},
		Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAzure),
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
	)

	Context("Enforce Before Import", func() {
		JustBeforeEach(func() {
			importAfterANP = true
			abbreviated = true
		})
		DescribeTable("AppliedTo",
			func(kind string) {
				testAppliedTo(kind)
			},
			Entry(fmt.Sprintf("%s %s: VM In Same Namespace", focusAzure, focusAgent),
				reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
		)

		DescribeTable("Egress",
			func(kind string) {
				testEgress(kind)
			},
			Entry(fmt.Sprintf("%s: VM In Same Namespace", focusAgent),
				reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
		)

		DescribeTable("Ingress",
			func(kind string) {
				testIngress(kind)
			},
			Entry(fmt.Sprintf("%s %s: VM In Same Namespace", focusAzure, focusAgent),
				reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()),
		)
	})

	It("Reconcile with cloud", func() {
		ids := cloudVPC.GetVMs()
		ips := cloudVPC.GetVMPrivateIPs()
		kind := reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()
		setup(kind, len(ids), []string{"22"}, false)
		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]

		var replicas int32
		var err error

		By("New NetworkPolicy")
		replicas, err = utils.StopDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")
		oks := make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.From = utils.ConfigANPToFrom(kind, ids[0], "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		err = utils.StartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", replicas, time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		// wait for aggregated api server to ready.
		err = utils.WaitApiServer(k8sClient, time.Second*60)
		Expect(err).NotTo(HaveOccurred())
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true, false)

		By("Changed NetworkPolicy")
		replicas, err = utils.StopDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		oks = make([]bool, len(ids)-1)
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		err = utils.StartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", replicas, time.Second*120)
		Expect(err).NotTo(HaveOccurred())
		err = utils.WaitApiServer(k8sClient, time.Second*60)
		Expect(err).NotTo(HaveOccurred())
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true, false)

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

		np := []string{anpSetupParams.Name}
		if !withAgent && cloudProviders == "Azure" {
			np = append(np, defaultANPParameters.Name)
		}
		err = utils.CheckCloudResourceNetworkPolicies(kubeCtl, k8sClient, kind, namespace.Name, ids, np, withAgent)
		Expect(err).ToNot(HaveOccurred())
		err = utils.ExecuteCurlCmds(cloudVPC, nil, srcVMs, "", []string{ids[appliedIdx]}, apachePort, oks, 30)
		Expect(err).ToNot(HaveOccurred())
		// Add ANP back to satisfy AfterEach
		err = utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Controllers RestartPoller", func() {
		ids := cloudVPC.GetVMs()
		ips := cloudVPC.GetVMPrivateIPs()

		kind := reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()
		setup(kind, len(ids), []string{"22"}, false)
		appliedIdx := len(ids) - 1
		srcVMs := cloudVPC.GetVMs()[:appliedIdx]

		oks := make([]bool, len(ids)-1)
		var err error
		By(fmt.Sprintf("Ingress NetworkPolicy on %v by kind label selector while restarting Antrea controller", kind))
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")
		for i := range oks {
			oks[i] = true
		}
		anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)
		err = utils.RestartOrWaitDeployment(k8sClient, "antrea-controller", "kube-system", time.Second*200, true)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(time.Second * 30)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true, false)

		By(fmt.Sprintf("Ingress NetworkPolicy on %v by name label selector while restarting Nephe controller", kind))
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, ids[appliedIdx], "", "", "")
		oks = make([]bool, len(ids)-1)
		oks[0] = true
		anpParams.From = utils.ConfigANPToFrom(kind, ids[0], "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, false, false)
		By("Restarting controllers now...")
		err = utils.RestartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*200, true)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(time.Second * 30)
		verifyIngress(kind, ids[appliedIdx], ips[appliedIdx], srcVMs, oks, true, false)
	})

	It("Remove Stale Member", func() {
		vmCount := 1
		ids := cloudVPC.GetVMIDs()
		kind := reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()
		setup(kind, len(ids), []string{"22"}, false)
		anpParams.AppliedTo = utils.ConfigANPApplyTo(kind, "", cloudVPC.GetCRDVPCID(), "", "")
		anpParams.From = utils.ConfigANPToFrom(kind, "", "", "", "", "", namespace.Name,
			cpv1beta2.ProtocolTCP, []string{apachePort}, false)
		err := utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.CloudAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())

		np := []string{anpSetupParams.Name, anpParams.Name}
		if !withAgent && cloudProviders == "Azure" {
			np = append(np, defaultANPParameters.Name)
		}
		err = utils.CheckCloudResourceNetworkPolicies(
			kubeCtl, k8sClient, kind, namespace.Name, cloudVPC.GetVMs(), np, withAgent,
		)
		Expect(err).ToNot(HaveOccurred())

		By("Change CES selector to import only a subset of VMs")
		entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector"+namespace.Name, namespace.Name, kind, ids[:vmCount])
		err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, vmCount, namespace.Name, false)
		Expect(err).ToNot(HaveOccurred())

		By("Check VMPs after importing subset of VMs")
		err = utils.CheckVirtualMachinePolicies(k8sClient, namespace.Name, vmCount)
		Expect(err).ToNot(HaveOccurred())

		By("Revert CES selector to import all VMS")
		entityParams = cloudVPC.GetEntitySelectorParameters("test-entity-selector"+namespace.Name, namespace.Name, kind, nil)
		err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams, kind, len(cloudVPC.GetVMs()), namespace.Name, false)
		Expect(err).ToNot(HaveOccurred())
	})
})
