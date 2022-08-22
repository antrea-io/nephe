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
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/pkg/converter/target"
	"antrea.io/nephe/pkg/testing"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: ExternalEntity", focusAws, focusAzure), func() {

	var (
		ipAddresses = []string{"1.1.1.1", "2.2.2.2"}
		namedPorts  = []antreatypes.NamedPort{
			{Name: "http", Protocol: v1.ProtocolTCP, Port: 80},
			{Name: "https", Protocol: v1.ProtocolTCP, Port: 443},
		}
		externalEntitySources map[string]target.ExternalEntitySource
		namespace             *v1.Namespace
		expectedEndpoints     []antreatypes.Endpoint
		restartController     bool
	)

	BeforeEach(func() {
		if preserveSetupOnFail {
			preserveSetup = true
		}

		namespace = &v1.Namespace{}
		namespace.Name = "test-externalentity-" + fmt.Sprintf("%x", rand.Int())
		err := k8sClient.Create(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
		logf.Log.Info("Created namespace", "namespace", namespace.Name)
		externalEntitySources = testing.SetupExternalEntitySources(ipAddresses, namespace.Name)
		restartController = false

		expectedEndpoints = nil
		for _, ip := range ipAddresses {
			expectedEndpoints = append(expectedEndpoints,
				antreatypes.Endpoint{IP: ip})
		}
	})

	AfterEach(func() {
		result := CurrentGinkgoTestDescription()
		if result.Failed {
			if len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for test failure.")
				fileName := utils.GenerateNameFromText(result.FullTestText, testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName))
			}
			if preserveSetupOnFail {
				logf.Log.V(1).Info("Preserve setup on failure")
				return
			}
		}
		preserveSetup = false
		err := k8sClient.Delete(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	checkEndpoints := func(key client.ObjectKey, spec *antreatypes.ExternalEntitySpec) {
		Eventually(func() *antreatypes.ExternalEntitySpec {
			ee := &antreatypes.ExternalEntity{}
			err := k8sClient.Get(context.TODO(), key, ee)
			if err != nil {
				return nil
			}
			return &ee.Spec
		}, 30, 1).Should(Equal(spec))
	}
	checkRemoved := func(key client.ObjectKey, ee *antreatypes.ExternalEntity) {
		Eventually(func() bool {
			err := k8sClient.Get(context.TODO(), key, ee)
			if err != nil && errors.IsNotFound(err) {
				return true
			}
			return false
		}, 30, 1).Should(BeTrue())
	}
	tester := func(kind string, epNum int, hasNic, hasPort bool) {
		// TODO: remove set resource version and copy status in another way
		externalEntitySource := externalEntitySources[kind].EmbedType()
		externalEntitySourceCopy := externalEntitySources[kind].Copy().EmbedType()
		ctx := context.Background()
		By("ExternalEntity can be created")
		err := k8sClient.Create(ctx, externalEntitySourceCopy)
		Expect(err).ToNot(HaveOccurred())
		externalEntitySource.SetResourceVersion(externalEntitySourceCopy.GetResourceVersion())
		err = k8sClient.Status().Update(ctx, externalEntitySource)
		Expect(err).ToNot(HaveOccurred())

		eeFetchKey := client.ObjectKey{Name: strings.ToLower(kind) + "-" + externalEntitySource.(metav1.Object).GetName(),
			Namespace: externalEntitySource.(metav1.Object).GetNamespace()}
		externalEntity := &antreatypes.ExternalEntity{}
		var expectPorts []antreatypes.NamedPort
		if hasPort {
			expectPorts = namedPorts
		}

		spec := &antreatypes.ExternalEntitySpec{
			Endpoints:    expectedEndpoints[:epNum],
			Ports:        expectPorts,
			ExternalNode: config.ANPNepheController,
		}
		checkEndpoints(eeFetchKey, spec)

		if restartController {
			err = utils.RestartOrWaitDeployment(k8sClient, "nephe-controller", "nephe-system", time.Second*120, true)
			Expect(err).ToNot(HaveOccurred())
			// After restart controller, test need to permit time to allow ExternalEntitySources to be re-learnt.
			time.Sleep(time.Second * 10)
		}

		if hasNic {
			By("ExternalEntity can be changed when source CRD changes")
			source := testing.SetNetworkInterfaceIP(kind, externalEntitySource, "nic0", "")
			err = k8sClient.Status().Update(ctx, source)
			Expect(err).ToNot(HaveOccurred())
			spec := &antreatypes.ExternalEntitySpec{
				Endpoints:    expectedEndpoints[1:],
				Ports:        expectPorts,
				ExternalNode: config.ANPNepheController,
			}
			checkEndpoints(eeFetchKey, spec)

			By("ExternalEntity can be removed when source has no IP")
			source = testing.SetNetworkInterfaceIP(kind, externalEntitySource, "nic1", "")
			err = k8sClient.Status().Update(ctx, source)
			Expect(err).ToNot(HaveOccurred())
			checkRemoved(eeFetchKey, externalEntity)

			By("ExternalEntity can be re-created when source recovers IP")
			for i, ip := range ipAddresses {
				name := "nic" + fmt.Sprintf("%d", i)
				source = testing.SetNetworkInterfaceIP(kind, externalEntitySource, name, ip)
				err = k8sClient.Status().Update(ctx, source)
				Expect(err).ToNot(HaveOccurred())
			}
			spec = &antreatypes.ExternalEntitySpec{
				Endpoints:    expectedEndpoints,
				Ports:        expectPorts,
				ExternalNode: config.ANPNepheController,
			}
			checkEndpoints(eeFetchKey, spec)
		}

		By("ExternalEntity can be removed when source is removed")
		err = k8sClient.Delete(ctx, externalEntitySource)
		Expect(err).ToNot(HaveOccurred())
		checkRemoved(eeFetchKey, externalEntity)
	}
	table.DescribeTable("Test ExternalEntity Life cycle",
		func(kind string, hasNic, hasPort bool) {
			tester(kind, len(expectedEndpoints), hasNic, hasPort)
		},
		table.Entry("VirtualMachine", "VirtualMachine", true, false),
	)

	Context("After reboot", func() {
		JustBeforeEach(func() {
			restartController = true
		})

		table.DescribeTable("Test ExternalEntity Life cycle",
			func(kind string, hasNic, hasPort bool) {
				tester(kind, len(expectedEndpoints), hasNic, hasPort)
			},
			table.Entry("VirtualMachine", "VirtualMachine", true, false),
		)
	})
})
