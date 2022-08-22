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
	"errors"
	"fmt"
	"math/rand"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/onsi/ginkgo/extensions/table"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s: Entity selector test", focusAws), func() {
	const (
		nameSpaceName   = "entity-selector-test"
		testAccountName = "entity-selector-test-account"
	)

	const (
		vpcIDMatch   = "vpc-id"
		vpcNameMatch = "vpc-name"
		vmIDMatch    = "vm-id"
		vmNameMatch  = "vm-name"
	)

	var (
		getVMsCmd     string
		vpcID         string
		vpcName       string
		vmList        []string
		vmIDList      []string
		vmNameList    []string
		pollInterval  uint = 30
		nameSpace     *v1.Namespace
		account       *v1alpha1.CloudProviderAccount
		selector      *v1alpha1.CloudEntitySelector
		secret        *corev1.Secret
		cloudProvider string
	)

	createNS := func() {
		logf.Log.Info("Create", "namespace", nameSpace.Name)
		err := k8sClient.Create(context.TODO(), nameSpace)
		Expect(err).ToNot(HaveOccurred())
	}

	deleteNS := func() {
		logf.Log.Info("Delete", "namespace", nameSpace.Name)
		err := k8sClient.Delete(context.TODO(), nameSpace)
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

	applyEntitySelector := func(matchKey string, matchValue string, selectorExists bool) {
		logf.Log.Info("Apply entity selector with", matchKey, matchValue)
		switch matchKey {
		case vpcIDMatch:
			if len(selector.Spec.VMSelector) == 0 {
				selector.Spec.VMSelector = append(selector.Spec.VMSelector,
					v1alpha1.VirtualMachineSelector{VpcMatch: &v1alpha1.EntityMatch{MatchID: matchValue}})
			} else {
				selector.Spec.VMSelector[0].VpcMatch.MatchID = matchValue
			}
		case vpcNameMatch:
			if len(selector.Spec.VMSelector) == 0 {
				selector.Spec.VMSelector = append(selector.Spec.VMSelector,
					v1alpha1.VirtualMachineSelector{VpcMatch: &v1alpha1.EntityMatch{MatchName: matchValue}})
			} else {
				selector.Spec.VMSelector[0].VpcMatch.MatchName = matchValue
			}
		case vmIDMatch:
			if len(selector.Spec.VMSelector) == 0 {
				selector.Spec.VMSelector = append(selector.Spec.VMSelector,
					v1alpha1.VirtualMachineSelector{
						VMMatch: []v1alpha1.EntityMatch{
							{
								MatchID: matchValue,
							},
						}})
			} else {
				selector.Spec.VMSelector[0].VMMatch[0].MatchID = matchValue
			}
		case vmNameMatch:
			if len(selector.Spec.VMSelector) == 0 {
				selector.Spec.VMSelector = append(selector.Spec.VMSelector,
					v1alpha1.VirtualMachineSelector{
						VMMatch: []v1alpha1.EntityMatch{
							{
								MatchName: matchValue,
							},
						}})
			} else {
				selector.Spec.VMSelector[0].VMMatch[0].MatchName = matchValue
			}
		default:
			logf.Log.Error(errors.New("invalid matchKey"), "matchKey", matchKey)
		}
		var err error
		if selectorExists {
			err = k8sClient.Update(context.TODO(), selector)
		} else {
			err = k8sClient.Create(context.TODO(), selector)
		}
		Expect(err).ToNot(HaveOccurred())
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
			logf.Log.Info("the value is not correct", "actual", actual, "expected", expected)
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
				logf.Log.Info("the value is not correct", "actual", actual, "expected", expected)
				return false
			}
		}
		return true
	}

	// Check whether all elements in expected is contained in actual
	checkContain := func(actual, expected []string) bool {
		if len(expected) > len(actual) {
			logf.Log.Info("the value is not correct", "actual", actual, "expected", expected)
			return false
		}
		actualSet := make(map[string]int)
		for i, v := range actual {
			actualSet[v] = i
		}
		for _, v := range expected {
			if _, ok := actualSet[v]; !ok {
				logf.Log.Info("actual is not contained in the expected", "actual", actual, "expected", expected)
				return false
			}
		}
		return true
	}

	tester := func(matchKey string, matchValue string, selectorExists bool, numOfInterval int, expectedResult []string) {
		applyEntitySelector(matchKey, matchValue, selectorExists)
		time.Sleep(time.Duration(numOfInterval) * time.Duration(pollInterval) * time.Second)
		output, err := kubeCtl.Cmd(getVMsCmd)
		Expect(err).ToNot(HaveOccurred())
		VMList := strings.Split(strings.Trim(output, "'"), "\n")
		// vms in different VPCs might have the same name and all of them will be selected
		if cloudProvider == string(v1alpha1.AzureCloudProvider) && matchKey == vmNameMatch {
			Expect(checkContain(removeEmptyStr(VMList), expectedResult)).To(BeTrue())
		} else {
			actualResult := removeEmptyStr(VMList)
			logf.Log.Info("After test", "Actual Result ", actualResult)
			logf.Log.Info("After test", "Expected Result ", expectedResult)
			Expect(checkEqual(actualResult, expectedResult)).To(BeTrue())
		}
	}

	BeforeEach(func() {
		if preserveSetupOnFail {
			preserveSetup = true
		}
		vpcID = cloudVPC.GetVPCID()
		vpcName = cloudVPC.GetVPCName()
		vmList = cloudVPC.GetVMs()
		vmIDList = cloudVPC.GetVMIDs()
		vmNameList = cloudVPC.GetVMNames()
		nameSpace = &v1.Namespace{}
		nameSpace.Name = nameSpaceName + "-" + fmt.Sprintf("%x", rand.Int())
		getVMsCmd = fmt.Sprintf(
			"get virtualmachines -n %s -o=jsonpath='{range.items[*]}{.metadata.name}{\"\\n\"}{end}'", nameSpace.Name)
		accountParameters := cloudVPC.GetCloudAccountParameters(testAccountName, nameSpace.Name, cloudCluster)
		cloudProvider = strings.Split(cloudProviders, ",")[0]
		switch cloudProvider {
		case string(v1alpha1.AWSCloudProvider):
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v12.ObjectMeta{
					Name:      testAccountName,
					Namespace: nameSpace.Name,
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
				ObjectMeta: v12.ObjectMeta{
					Name:      testAccountName,
					Namespace: nameSpace.Name,
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
			logf.Log.Error(nil, "Invalid provider", "Provider", cloudProvider)
		}
		selector = &v1alpha1.CloudEntitySelector{
			ObjectMeta: v12.ObjectMeta{
				Name:      testAccountName,
				Namespace: nameSpace.Name,
			},
			Spec: v1alpha1.CloudEntitySelectorSpec{
				AccountName: testAccountName,
				VMSelector:  []v1alpha1.VirtualMachineSelector{},
			},
		}
		// SDK call will automatically base64 encode data.
		secret = &corev1.Secret{
			ObjectMeta: v12.ObjectMeta{
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

	table.DescribeTable("Testing entity selector",
		func(matchKey string) {
			var matchValue string
			var expectedResult []string

			switch matchKey {
			case vpcIDMatch:
				matchValue = vpcID
				expectedResult = vmList
			case vpcNameMatch:
				matchValue = vpcName
				expectedResult = vmIDList
			case vmIDMatch:
				matchValue = vmIDList[0]
				expectedResult = vmList[:1]
			case vmNameMatch:
				matchValue = vmNameList[0]
				expectedResult = vmList[:1]
			default:
				logf.Log.Error(errors.New("invalid matchKey"), "matchKey", matchKey)
			}

			By("Apply entity selector with valid match key")
			switch matchKey {
			case vpcNameMatch:
				tester(matchKey, matchValue, false, 3, expectedResult)
			default:
				tester(matchKey, matchValue, false, 1, expectedResult)
			}

			By("Change match key to invalid non-empty value")
			tester(matchKey, matchValue+"a", true, 2, nil)

			By("Change match key back to valid value again")
			tester(matchKey, matchValue, true, 2, expectedResult)
		},
		table.Entry(focusAzure+":"+"VPC id match", vpcIDMatch),
		table.Entry(focusAzure+":"+"VM id match", vmIDMatch),
		table.Entry(focusAzure+":"+"VM name match", vmNameMatch),
		// vpcNameMatch test apply only to aws for now
		table.Entry("VPC name match", vpcNameMatch),
	)
})
