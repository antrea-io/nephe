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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	antreav1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: ExternalNode", focusAws, focusAzure), func() {
	const (
		namespaceName   = "external-node-test"
		testAccountName = "external-node-test-account"
	)

	var (
		pollInterval  uint = 30
		namespace     *corev1.Namespace
		account       *v1alpha1.CloudProviderAccount
		selector      *v1alpha1.CloudEntitySelector
		secret        *corev1.Secret
		cloudProvider string
		vmIdToMatch   string
		testVm        string
		nic           string
		publicIp      string
		privateIp     string
	)

	createNS := func() {
		logf.Log.Info("Create", "namespace", namespace.Name)
		err := k8sClient.Create(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
	}

	deleteNS := func() {
		logf.Log.Info("Delete", "namespace", namespace.Name)
		err := k8sClient.Delete(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
	}

	createAccount := func() {
		logf.Log.Info("Create", "account", account.Name)
		err := k8sClient.Create(context.TODO(), secret)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Create(context.TODO(), account)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(5 * time.Second)
	}

	deleteAccount := func() {
		logf.Log.Info("Delete", "account", account.Name)
		err := k8sClient.Delete(context.TODO(), account)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(context.TODO(), secret)
		Expect(err).ToNot(HaveOccurred())
	}

	applyCloudEntitySelector := func(selectorExists bool, agented bool) error {
		logf.Log.Info("Apply entity selector with VM ID as a matchID", "VM ID", vmIdToMatch)
		if len(selector.Spec.VMSelector) == 0 {
			selector.Spec.VMSelector = append(selector.Spec.VMSelector,
				v1alpha1.VirtualMachineSelector{
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchID: vmIdToMatch,
						},
					},
					Agented: agented,
				})
		} else {
			selector.Spec.VMSelector[0].VMMatch[0].MatchID = vmIdToMatch
			selector.Spec.VMSelector[0].Agented = agented
		}
		var err error
		if selectorExists {
			err = k8sClient.Update(context.TODO(), selector)
		} else {
			err = k8sClient.Create(context.TODO(), selector)
		}
		return err
	}

	deleteCloudEntitySelector := func() error {
		logf.Log.Info("Delete", "selector", selector.Name)
		err := k8sClient.Delete(context.TODO(), selector)
		return err
	}

	validateResult := func(expectedResult string, kubectlCmd string, agented bool, en *antreav1alpha1.ExternalNode,
		ee *antreav1alpha2.ExternalEntity) {
		var actualResult string

		output, err := kubeCtl.Cmd(kubectlCmd)
		Expect(err).ToNot(HaveOccurred())

		// Trim ' from output and remove trailing "\n".
		stringList := strings.Split(strings.Trim(output, "'"), "\n")
		if len(stringList) > 0 {
			actualResult = stringList[0]
		} else {
			actualResult = ""
		}
		if agented == true {
			logf.Log.Info("Validate External Node Name", "Actual", actualResult, "Expected", expectedResult)
			Expect(actualResult == expectedResult).To(BeTrue())

			// Validate spec field in the generated External Node CR.
			spec := &antreav1alpha1.ExternalNodeSpec{
				Interfaces: []antreav1alpha1.NetworkInterface{
					{
						Name: strings.ToLower(nic),
						IPs:  []string{privateIp, publicIp},
					},
				},
			}
			logf.Log.Info("Validate External Node Spec", "Actual", &en.Spec, "Expected", spec)
			Expect(&en.Spec).To(Equal(spec))
		} else {
			logf.Log.Info("Validate External Entity Name", "Actual", actualResult, "Expected", expectedResult)
			Expect(actualResult == expectedResult).To(BeTrue())

			// Validate spec field in the generated External Entity CR.
			spec := &antreav1alpha2.ExternalEntitySpec{
				Endpoints: []antreav1alpha2.Endpoint{
					{
						IP: privateIp,
					},
					{
						IP: publicIp,
					},
				},
				ExternalNode: config.ANPNepheController,
			}
			logf.Log.Info("Validate External Entity Spec", "Actual", &ee.Spec, "Expected", spec)
			Expect(&ee.Spec).To(Equal(spec))
		}
	}

	checkEnCreated := func(key client.ObjectKey, en *antreav1alpha1.ExternalNode) error {
		logf.Log.Info("Check for External Node creation")
		err := wait.PollImmediate(time.Second*5, time.Second*60, func() (bool, error) {
			ret := k8sClient.Get(context.TODO(), key, en)
			if ret != nil {
				return false, nil
			}
			return true, nil
		})
		return err
	}

	checkEeCreated := func(key client.ObjectKey, ee *antreav1alpha2.ExternalEntity) error {
		logf.Log.Info("Check for External Entity creation")
		err := wait.PollImmediate(time.Second*5, time.Second*60, func() (bool, error) {
			ret := k8sClient.Get(context.TODO(), key, ee)
			if ret != nil {
				return false, nil
			}
			return true, nil
		})
		return err
	}

	checkEnDeleted := func(key client.ObjectKey) error {
		logf.Log.Info("Check for External Node deletion")
		err := wait.PollImmediate(time.Second*5, time.Second*60, func() (bool, error) {
			en := &antreav1alpha1.ExternalNode{}
			ret := k8sClient.Get(context.TODO(), key, en)
			if ret != nil && errors.IsNotFound(ret) {
				return true, nil
			}
			return false, nil
		})
		return err
	}

	checkEeDeleted := func(key client.ObjectKey) error {
		logf.Log.Info("Check for External Entity deletion")
		err := wait.PollImmediate(time.Second*5, time.Second*60, func() (bool, error) {
			ee := &antreav1alpha2.ExternalEntity{}
			ret := k8sClient.Get(context.TODO(), key, ee)
			if ret != nil && errors.IsNotFound(ret) {
				return true, nil
			}
			return false, nil
		})
		return err
	}

	tester := func(expectedResult string, selectorExists bool, agented bool, isDelete bool) {
		fetchKey := client.ObjectKey{
			Name:      expectedResult,
			Namespace: namespace.Name,
		}
		if agented == true && isDelete == true {
			err := deleteCloudEntitySelector()
			Expect(err).ToNot(HaveOccurred())

			err = checkEnDeleted(fetchKey)
			if err != nil {
				logf.Log.Error(err, "External Node delete failed", "fetch key", fetchKey)
			}
			Expect(err).NotTo(HaveOccurred())
		} else {
			err := applyCloudEntitySelector(selectorExists, agented)
			Expect(err).ToNot(HaveOccurred())

			var kubectlCmd string
			en := &antreav1alpha1.ExternalNode{}
			ee := &antreav1alpha2.ExternalEntity{}
			if agented == true {
				// Check if External Node is getting created.
				err := checkEnCreated(fetchKey, en)
				if err != nil {
					logf.Log.Error(err, "External Node creation failed", "fetch key", fetchKey)
				}
				Expect(err).NotTo(HaveOccurred())

				// Check if External entity is deleted when transitioned from agented = false.
				if selectorExists == true {
					err = checkEeDeleted(fetchKey)
					if err != nil {
						logf.Log.Error(err, "External Entity deletion failed", "fetch key", fetchKey)
					}
					Expect(err).NotTo(HaveOccurred())
				}

				kubectlCmd = fmt.Sprintf("get externalnode -n %s -o=jsonpath='{range.items[*]}{.metadata.name}{\"\\n\"}{end}'", namespace.Name)
			} else {
				// Check if External Entity is getting created.
				err := checkEeCreated(fetchKey, ee)
				if err != nil {
					logf.Log.Error(err, "External Entity creation failed", "fetch key", fetchKey)
				}
				Expect(err).NotTo(HaveOccurred())

				// Check if External Node is deleted when transitioned from agented = true.
				err = checkEnDeleted(fetchKey)
				if err != nil {
					logf.Log.Error(err, "External Node deletion failed", "fetch key", fetchKey)
				}
				Expect(err).NotTo(HaveOccurred())

				kubectlCmd = fmt.Sprintf("get externalentity -n %s -o=jsonpath='{range.items[*]}{.metadata.name}{\"\\n\"}{end}'", namespace.Name)
			}

			// Validate generated CR and the spec.
			validateResult(expectedResult, kubectlCmd, agented, en, ee)
		}
	}

	BeforeEach(func() {
		if preserveSetupOnFail {
			preserveSetup = true
		}
		namespace = &corev1.Namespace{}
		namespace.Name = namespaceName + "-" + fmt.Sprintf("%x", rand.Int())

		vmIdToMatch = cloudVPC.GetVMIDs()[0]
		testVm = cloudVPC.GetVMs()[0]
		nic = cloudVPC.GetPrimaryNICs()[0]
		publicIp = cloudVPC.GetVMIPs()[0]
		privateIp = cloudVPC.GetVMPrivateIPs()[0]

		accountParameters := cloudVPC.GetCloudAccountParameters(testAccountName, namespace.Name, cloudCluster)
		cloudProvider = strings.Split(cloudProviders, ",")[0]
		switch cloudProvider {
		case string(v1alpha1.AWSCloudProvider):
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountName,
					Namespace: namespace.Name,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollInterval,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: accountParameters.Aws.Region,
						SecretRef: &v1alpha1.SecretReference{
							Name:      accountParameters.SecretRef.Name,
							Namespace: accountParameters.SecretRef.Namespace,
							Key:       accountParameters.SecretRef.Key,
						},
					},
				},
			}
		case string(v1alpha1.AzureCloudProvider):
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountName,
					Namespace: namespace.Name,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollInterval,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: accountParameters.Azure.Location,
						SecretRef: &v1alpha1.SecretReference{
							Name:      accountParameters.SecretRef.Name,
							Namespace: accountParameters.SecretRef.Namespace,
							Key:       accountParameters.SecretRef.Key,
						},
					},
				},
			}
		default:
			Fail("Invalid Provider Type")
		}
		selector = &v1alpha1.CloudEntitySelector{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAccountName,
				Namespace: namespace.Name,
			},
			Spec: v1alpha1.CloudEntitySelectorSpec{
				AccountName: testAccountName,
				VMSelector:  []v1alpha1.VirtualMachineSelector{},
			},
		}
		// SDK call will automatically base64 encode data.
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      accountParameters.SecretRef.Name,
				Namespace: accountParameters.SecretRef.Namespace,
			},
			Data: map[string][]byte{accountParameters.SecretRef.Key: []byte(accountParameters.SecretRef.Credential)},
		}
		createNS()
		createAccount()
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
		deleteAccount()
		deleteNS()
	})

	table.DescribeTable("Testing agented and agentless configuration through CES",
		func() {
			expectedResult := fmt.Sprintf("virtualmachine-%s", testVm)

			By("Configure entity selector with agented field set to true, check ExternalNode creation")
			tester(expectedResult, false, true, false)

			By("Change entity selector with agented field set to false, check ExternalEntity creation")
			tester(expectedResult, true, false, false)

			By("Change entity selector back to agented field set to true, check ExternalNode creation")
			tester(expectedResult, true, true, false)

			By("Delete entity selector and check for External Node deletion")
			tester(expectedResult, true, true, true)

		},
		table.Entry("Test External Node Lifecycle"),
	)
})
