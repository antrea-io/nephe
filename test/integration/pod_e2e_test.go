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
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/labels"
	k8stemplates "antrea.io/nephe/test/templates"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: NetworkPolicy On Pods", focusAws, focusAzure), func() {
	const (
		httpServiceName    = "httpbin"
		curlDeploymentName = "curl"
	)
	var (
		namespace     *v1.Namespace
		vmNamespace   *v1.Namespace
		anpParams     k8stemplates.PodANPParameters
		curlParams    k8stemplates.DeploymentParameters
		httpBinParams k8stemplates.ServiceParameters
	)

	BeforeEach(func() {
		if preserveSetupOnFail {
			preserveSetup = true
		}

		namespace = &v1.Namespace{}
		namespace.Name = "test-pod-e2e-" + fmt.Sprintf("%x", rand.Int())
		namespace.Labels = map[string]string{"name": namespace.Name}
		err := k8sClient.Create(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
		anpParams = k8stemplates.PodANPParameters{
			Name:        "test-pod-anp",
			Namespace:   namespace.Name,
			PodSelector: curlDeploymentName,
			Action:      "Allow",
		}
		curlParams = k8stemplates.DeploymentParameters{
			Name:      curlDeploymentName,
			Namespace: namespace.Name,
			Replicas:  1,
		}
		err = utils.ConfigureK8s(kubeCtl, curlParams, k8stemplates.CurlDeployment, false)
		Expect(err).ToNot(HaveOccurred())
		err = utils.RestartOrWaitDeployment(k8sClient, curlDeploymentName, namespace.Name, time.Second*120, false)
		Expect(err).ToNot(HaveOccurred())
		httpBinParams = k8stemplates.ServiceParameters{
			DeploymentParameters: k8stemplates.DeploymentParameters{
				Name:      httpServiceName,
				Namespace: namespace.Name,
				Replicas:  1,
			},
			Port: "8000",
		}
		err = utils.ConfigureK8s(kubeCtl, httpBinParams, k8stemplates.HTTPBinService, false)
		Expect(err).ToNot(HaveOccurred())
		err = utils.RestartOrWaitDeployment(k8sClient, httpServiceName, namespace.Name, time.Second*120, false)
		Expect(err).ToNot(HaveOccurred())
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

		err := k8sClient.Delete(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
		if vmNamespace.Name != namespace.Name {
			err := k8sClient.Delete(context.TODO(), vmNamespace)
			Expect(err).ToNot(HaveOccurred())
		}
		preserveSetup = false
	})

	podANPVerify := func(kind, instanceName, vpc, tagKey, tagVal string, oks []bool, pod string, importing, diffNS bool) {
		if len(kind) > 0 {
			anpParams.To = &k8stemplates.ToFromParameters{
				Entity: &k8stemplates.EntitySelectorParameters{
					Kind: labels.ExternalEntityLabelKeyKind + ": " + strings.ToLower(kind),
				},
			}
			if len(vpc) > 0 {
				anpParams.To.Entity.VPC = labels.ExternalEntityLabelKeyOwnerVmVpc + ": " + strings.ToLower(vpc)
			}
			if len(instanceName) > 0 {
				anpParams.To.Entity.CloudInstanceName = labels.ExternalEntityLabelKeyOwnerVm + ": " + strings.ToLower(instanceName)
			}
			if len(tagKey) > 0 {
				tagKey = labels.LabelPrefixNephe + labels.ExternalEntityLabelKeyTagPrefix + tagKey
				anpParams.To.Entity.Tags = map[string]string{tagKey: tagVal}
			}
			if diffNS {
				anpParams.To.Namespace = &k8stemplates.NamespaceParameters{
					Labels: map[string]string{"name": vmNamespace.Name},
				}
			}
		}
		err := utils.ConfigureK8s(kubeCtl, anpParams, k8stemplates.PodAntreaNetworkPolicy, false)
		Expect(err).ToNot(HaveOccurred())
		// Allow time for ANP change to propagate.
		time.Sleep(time.Second * 2)

		if importing {
			entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector", vmNamespace.Name,
				reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), nil)
			err = utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams,
				reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(),
				len(cloudVPC.GetVMs()), vmNamespace.Name, false)
			Expect(err).ToNot(HaveOccurred())
		}

		Expect(len(cloudVPC.GetVMIPs())).To(Equal(len(oks)))
		// Increased retry count to allow command execution total timeout to 120 seconds.
		err = utils.ExecuteCurlCmds(nil, kubeCtl, []string{pod}, namespace.Name, cloudVPC.GetVMIPs(), "80", oks, 24)
		Expect(err).ToNot(HaveOccurred())
	}

	DescribeTable("Egress",
		func(kind string, importFirst, diffNS bool) {
			if !diffNS {
				vmNamespace = namespace
			} else {
				vmNamespace = &v1.Namespace{}
				vmNamespace.Name = "test-pod-e2e-" + fmt.Sprintf("%x", rand.Int())
				vmNamespace.Labels = map[string]string{"name": vmNamespace.Name}
				err := k8sClient.Create(context.TODO(), vmNamespace)
				Expect(err).ToNot(HaveOccurred())
			}
			accountParams := cloudVPC.GetCloudAccountParameters("test-cloud-account", vmNamespace.Name, false)
			err := utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, false)
			Expect(err).ToNot(HaveOccurred())

			if importFirst {
				entityParams := cloudVPC.GetEntitySelectorParameters("test-entity-selector", vmNamespace.Name,
					reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), nil)
				err := utils.ConfigureEntitySelectorAndWait(kubeCtl, k8sClient, entityParams,
					reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(),
					len(cloudVPC.GetVMs()), vmNamespace.Name, false)
				Expect(err).ToNot(HaveOccurred())
			}

			pods, err := utils.GetPodsFromDeployment(k8sClient, curlDeploymentName, namespace.Name)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(pods)).To(Equal(curlParams.Replicas))
			Expect(len(cloudVPC.GetVMIPs())).To(BeNumerically(">", 0))

			By(kind + " not reachable by default")
			oks := make([]bool, len(cloudVPC.GetVMs()))
			podANPVerify("", "", "", "", "", oks, pods[0], false, diffNS)

			By(kind + " reachable via kind label selector")
			for i := range oks {
				oks[i] = true
			}
			podANPVerify(kind, "", "", "", "", oks, pods[0], !importFirst, diffNS)

			By(kind + " reachable via name label selector")
			for i := range oks {
				oks[i] = false
			}
			oks[0] = true
			name := cloudVPC.GetVMs()[0]
			if kind != reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
				name = cloudVPC.GetNICs()[0]
			}
			podANPVerify(kind, name, "", "", "", oks, pods[0], false, diffNS)

			By(kind + " reachable via VPC label selector")
			for i := range oks {
				oks[i] = true
			}
			podANPVerify(kind, "", cloudVPC.GetCRDVPCID(), "", "", oks, pods[0], false, diffNS)

			if kind == reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name() {
				By(kind + " reachable via tag label selector")
				for i := range oks {
					oks[i] = false
				}
				oks[1] = true
				key := "Name"
				if value, found := cloudVPC.GetTags()[1][key]; found {
					podANPVerify(kind, "", "", key, value, oks, pods[0], false, diffNS)
				}

				By("K8s service reachable")
				ip, port, err := utils.GetServiceClusterIPPort(k8sClient, httpServiceName, namespace.Name)
				Expect(err).ToNot(HaveOccurred())
				err = utils.ExecuteCurlCmds(nil, kubeCtl, []string{pods[0]}, namespace.Name, []string{ip}, fmt.Sprint(port), []bool{true}, 2)
				Expect(err).ToNot(HaveOccurred())
			}
			err = utils.AddOrRemoveCloudAccount(kubeCtl, accountParams, true)
			Expect(err).ToNot(HaveOccurred())
		},
		Entry("To VM In Same Namespace",
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), true, false),
		Entry("To VM In Different Namespace",
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), true, true),
		Entry("To VM In Same Namespace Before Import",
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), false, false),
		Entry("To VM In Different Namespace Before Import",
			reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name(), false, true),
	)
})
