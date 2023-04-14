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
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
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
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/config"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: ExternalNode", focusAws, focusAzure), func() {
	const (
		namespaceName   = "external-node-test"
		testAccountName = "external-node-test-account"
		vmPrefix        = "virtualmachine"
	)

	const (
		vpcIDMatch  = "vpc-id"
		vmIDMatch   = "vm-id"
		vmNameMatch = "vm-name"
	)

	var (
		pollInterval  uint = 30
		namespace     *corev1.Namespace
		account       *v1alpha1.CloudProviderAccount
		selector      *v1alpha1.CloudEntitySelector
		secret        *corev1.Secret
		cloudProvider string
		vpcID         string
		vmList        []string
		vmIDList      []string
		vmNameList    []string
		nics          []string
		publicIps     []string
		privateIps    []string
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

	applyCloudEntitySelector := func(matchKey string, matchValue string, agented bool, selectorExists bool) error {
		logf.Log.Info("Apply entity selector with", matchKey, matchValue)
		switch matchKey {
		case vpcIDMatch:
			if len(selector.Spec.VMSelector) == 0 {
				selector.Spec.VMSelector = append(selector.Spec.VMSelector,
					v1alpha1.VirtualMachineSelector{
						VpcMatch: &v1alpha1.EntityMatch{
							MatchID: matchValue,
						},
						Agented: agented,
					})
			} else {
				selector.Spec.VMSelector[0].VpcMatch.MatchID = matchValue
				selector.Spec.VMSelector[0].Agented = agented
			}

		case vmIDMatch:
			if len(selector.Spec.VMSelector) == 0 {
				selector.Spec.VMSelector = append(selector.Spec.VMSelector,
					v1alpha1.VirtualMachineSelector{
						VMMatch: []v1alpha1.EntityMatch{
							{
								MatchID: matchValue,
							},
						},
						Agented: agented,
					})
			} else {
				selector.Spec.VMSelector[0].VMMatch[0].MatchID = matchValue
				selector.Spec.VMSelector[0].Agented = agented
			}
		case vmNameMatch:
			if len(selector.Spec.VMSelector) == 0 {
				selector.Spec.VMSelector = append(selector.Spec.VMSelector,
					v1alpha1.VirtualMachineSelector{
						VMMatch: []v1alpha1.EntityMatch{
							{
								MatchName: matchValue,
							},
						},
						Agented: agented,
					})
			} else {
				selector.Spec.VMSelector[0].VMMatch[0].MatchName = matchValue
				selector.Spec.VMSelector[0].Agented = agented
			}
		default:
			Fail("invalid matchKey")
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

	removeEmptyStr := func(input []string) []string {
		var output []string
		for _, vm := range input {
			if vm != "" {
				output = append(output, vm)
			}
		}
		return output
	}

	checkEqual := func(actual, expected []string) bool {
		if len(actual) != len(expected) {
			logf.Log.Error(fmt.Errorf(""), "number of items is not same",
				"actual", actual, "expected", expected)
			return false
		}
		sort.Slice(actual, func(i, j int) bool {
			return strings.Compare(actual[i], actual[j]) < 0
		})
		sort.Slice(expected, func(i, j int) bool {
			return strings.Compare(expected[i], expected[j]) < 0
		})
		for i, v := range actual {
			if v != expected[i] {
				logf.Log.Error(fmt.Errorf(""), "mismatch between the actual and expected value",
					"actual", actual, "expected", expected)
				return false
			}
		}
		return true
	}

	validateResult := func(expectedResult []string, kubectlCmd string, agented bool, enList []antreav1alpha1.ExternalNode,
		eeList []antreav1alpha2.ExternalEntity) {
		var actualResult []string
		// Keep a copy of expectedResult as the content of slice gets reordered in checkEqual() function.
		copyExpected := make([]string, len(expectedResult))
		copy(copyExpected, expectedResult)

		output, err := kubeCtl.Cmd(kubectlCmd)
		Expect(err).ToNot(HaveOccurred())
		// Trim ' from output and remove trailing "\n".
		stringList := strings.Split(strings.Trim(output, "'"), "\n")
		if len(stringList) > 0 {
			actualResult = removeEmptyStr(stringList)
		} else {
			actualResult = nil
		}

		if agented == true {
			logf.Log.V(2).Info("Validate generated External Node Name(s) with Expected",
				"Actual", actualResult, "Expected", copyExpected)
			Expect(checkEqual(actualResult, copyExpected)).To(BeTrue())

			// Validate spec field of each generated External Node CR against expected.
			for index, en := range enList {
				expectedSpec := &antreav1alpha1.ExternalNodeSpec{
					Interfaces: []antreav1alpha1.NetworkInterface{
						{
							Name: strings.ToLower(nics[index]),
							IPs:  []string{privateIps[index], publicIps[index]},
						},
					},
				}
				logf.Log.V(2).Info("Validate External Node Spec", "Actual", &en.Spec, "Expected", expectedSpec)
				Expect(&en.Spec).To(Equal(expectedSpec))
			}

		} else {
			logf.Log.V(2).Info("Validate generated External Entity Name(s) with Expected",
				"Actual", actualResult, "Expected", expectedResult)
			Expect(checkEqual(actualResult, copyExpected)).To(BeTrue())

			// Validate spec field of each generated External Entity CR against expected.
			for index, ee := range eeList {
				expectedSpec := &antreav1alpha2.ExternalEntitySpec{
					Endpoints: []antreav1alpha2.Endpoint{
						{
							IP: privateIps[index],
						},
						{
							IP: publicIps[index],
						},
					},
					ExternalNode: config.ANPNepheController,
				}
				logf.Log.V(2).Info("Validate External Entity Spec", "Actual", &ee.Spec, "Expected", expectedSpec)
				Expect(&ee.Spec).To(Equal(expectedSpec))
			}
		}
	}

	checkEnCreated := func(key client.ObjectKey, en *antreav1alpha1.ExternalNode) error {
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
		err := wait.PollImmediate(time.Second*5, time.Second*60, func() (bool, error) {
			ret := k8sClient.Get(context.TODO(), key, ee)
			if ret != nil {
				return false, nil
			}
			return true, nil
		})
		return err
	}

	checkAllEnDeleted := func() error {
		logf.Log.Info("Check for External Node deletion")
		err := wait.PollImmediate(time.Second*5, time.Second*60, func() (bool, error) {
			enList := &antreav1alpha1.ExternalNodeList{}
			ret := k8sClient.List(context.TODO(), enList, client.InNamespace(namespace.Name))
			if ret == nil && len(enList.Items) == 0 {
				return true, nil
			}
			return false, nil
		})
		return err
	}

	checkEeDeleted := func(key client.ObjectKey) error {
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

	tester := func(matchKey string, matchValue string, agented bool, expectedResult []string, selectorExists bool,
		isDelete bool) {
		if agented == true && isDelete == true {
			err := deleteCloudEntitySelector()
			Expect(err).ToNot(HaveOccurred())

			err = checkAllEnDeleted()
			if err != nil {
				logf.Log.Error(err, "external node delete failed", "namespace", namespace.Name)
			}
			Expect(err).NotTo(HaveOccurred())
		} else {
			err := applyCloudEntitySelector(matchKey, matchValue, agented, selectorExists)
			Expect(err).ToNot(HaveOccurred())

			var kubectlCmd string
			en := &antreav1alpha1.ExternalNode{}
			ee := &antreav1alpha2.ExternalEntity{}
			var enList []antreav1alpha1.ExternalNode
			var eeList []antreav1alpha2.ExternalEntity
			if agented == true {
				logf.Log.Info("Check for External Node creation")
				for _, expectedEn := range expectedResult {
					fetchKey := client.ObjectKey{
						Name:      expectedEn,
						Namespace: namespace.Name,
					}
					// Check if each External Node(matching expectedResult) is getting created.
					err := checkEnCreated(fetchKey, en)
					if err != nil {
						logf.Log.Error(err, "external node creation failed", "fetch key", fetchKey)
					}
					Expect(err).NotTo(HaveOccurred())
					// For future validation purpose, store External Nodes in the order processed.
					enList = append(enList, *en)

					// Check if External entity(corresponding to External Node) is deleted when moved from agented = false.
					if selectorExists == true {
						err = checkEeDeleted(fetchKey)
						if err != nil {
							logf.Log.Error(err, "external entity deletion failed", "fetch Key", fetchKey)
						}
						Expect(err).NotTo(HaveOccurred())
					}
				}

				kubectlCmd = fmt.Sprintf("get externalnode -n %s -o=jsonpath='{range.items[*]}{.metadata.name}{\"\\n\"}{end}'", namespace.Name)
			} else {
				// Check if External Entity is getting created.
				logf.Log.Info("Check for External Entity creation")
				for _, expectedEe := range expectedResult {
					fetchKey := client.ObjectKey{
						Name:      expectedEe,
						Namespace: namespace.Name,
					}
					err := checkEeCreated(fetchKey, ee)
					if err != nil {
						logf.Log.Error(err, "external entity creation failed", "fetch key", fetchKey)
					}
					Expect(err).NotTo(HaveOccurred())
					// For future validation purpose, store External Entities in the order processed.
					eeList = append(eeList, *ee)
				}
				// Check if External Nodes are deleted when transitioned from agented = true.
				err = checkAllEnDeleted()
				if err != nil {
					logf.Log.Error(err, "external node deletion failed", "namespace", namespace.Name)
				}
				Expect(err).NotTo(HaveOccurred())

				kubectlCmd = fmt.Sprintf("get externalentity -n %s -o=jsonpath='{range.items[*]}{.metadata.name}{\"\\n\"}{end}'", namespace.Name)
			}

			// Validate generated CR and the spec.
			validateResult(expectedResult, kubectlCmd, agented, enList, eeList)
		}
	}

	BeforeEach(func() {
		if preserveSetupOnFail {
			preserveSetup = true
		}
		namespace = &corev1.Namespace{}
		namespace.Name = namespaceName + "-" + fmt.Sprintf("%x", rand.Int())

		vpcID = cloudVPC.GetVPCID()
		vmList = cloudVPC.GetVMs()
		vmIDList = cloudVPC.GetVMIDs()
		vmNameList = cloudVPC.GetVMNames()
		nics = cloudVPC.GetPrimaryNICs()
		publicIps = cloudVPC.GetVMIPs()
		privateIps = cloudVPC.GetVMPrivateIPs()

		accountParameters := cloudVPC.GetCloudAccountParameters(testAccountName, namespace.Name)
		cloudProvider = strings.Split(cloudProviders, ",")[0]
		switch cloudProvider {
		case string(runtimev1alpha1.AWSCloudProvider):
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
		case string(runtimev1alpha1.AzureCloudProvider):
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
		deleteAccount()
		deleteNS()
	})

	DescribeTable("Testing agented and agentless configuration through CES",
		func(matchKey string) {
			var matchValue string
			var expectedResult []string

			switch matchKey {
			case vpcIDMatch:
				matchValue = vpcID
				for _, vm := range vmList {
					expectedResult = append(expectedResult, fmt.Sprintf("%s-%s", vmPrefix, vm))
				}
			case vmIDMatch:
				matchValue = vmIDList[0]
				expectedResult = append(expectedResult, fmt.Sprintf("%s-%s", vmPrefix, vmList[0]))
			case vmNameMatch:
				matchValue = vmNameList[0]
				expectedResult = append(expectedResult, fmt.Sprintf("%s-%s", vmPrefix, vmList[0]))
			default:
				Fail("Invalid match key")
			}

			By("Configure entity selector with agented field set to true, check ExternalNode creation")
			tester(matchKey, matchValue, true, expectedResult, false, false)

			By("Change entity selector with agented field set to false, check ExternalEntity creation")
			tester(matchKey, matchValue, false, expectedResult, true, false)

			By("Change entity selector back to agented field set to true, check ExternalNode creation")
			tester(matchKey, matchValue, true, expectedResult, true, false)

			By("Delete entity selector and check for External Node deletion")
			tester(matchKey, matchValue, true, expectedResult, true, true)

		},
		Entry("Test External Node Lifecycle: VPC ID Match", vpcIDMatch),
		Entry("Test External Node Lifecycle: VM ID Match", vmIDMatch),
		Entry("Test External Node Lifecycle: VM Name Match", vmNameMatch),
	)
})
