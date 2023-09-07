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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s: Entity selector test", focusAws), func() {
	const (
		namespaceName   = "entity-selector-test"
		testAccountName = "entity-selector-test-account"
	)

	const (
		vpcIDMatch   = "vpc-id"
		vpcNameMatch = "vpc-name"
		vmIDMatch    = "vm-id"
		vmNameMatch  = "vm-name"
	)

	var (
		vpcID        string
		vpcName      string
		vmList       []string
		vmIDList     []string
		vmNameList   []string
		pollInterval uint = 30
		numNamespace int
		cpaNamespace *v1.Namespace
		cesNamespace []v1.Namespace
		// CES can be in same namespace as CPA(cpaNamespace) or a different namespace(cesNamespace).
		account       *v1alpha1.CloudProviderAccount
		selector      []v1alpha1.CloudEntitySelector
		secret        *corev1.Secret
		cloudProvider string
	)

	createNS := func(namespace *v1.Namespace) {
		logf.Log.Info("Create", "namespace", namespace.Name)
		err := k8sClient.Create(context.TODO(), namespace)
		Expect(err).ToNot(HaveOccurred())
	}

	deleteNS := func(namespace *v1.Namespace) {
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

	applyEntitySelector := func(matchKey string, matchValue []string, selectorExists bool, cesNs []v1.Namespace) {
		for i := range cesNs {
			logf.Log.Info("Apply entity selector with", matchKey, matchValue[i], "namespace", cesNs[i].Name)
			// set CES namespace based on the value passed to the function.
			if !selectorExists {
				selector[i] = v1alpha1.CloudEntitySelector{
					ObjectMeta: v12.ObjectMeta{
						Name:      testAccountName,
						Namespace: cesNs[i].Name,
					},
					Spec: v1alpha1.CloudEntitySelectorSpec{
						AccountName:      testAccountName,
						AccountNamespace: cpaNamespace.Name,
						VMSelector:       []v1alpha1.VirtualMachineSelector{},
					},
				}
			} else {
				// fetch the latest CES in order to avoid resource version related issues on CES update.
				namespacedName := types.NamespacedName{Name: testAccountName, Namespace: cesNs[i].Name}
				err := k8sClient.Get(context.TODO(), namespacedName, &selector[i])
				Expect(err).ToNot(HaveOccurred())
			}

			switch matchKey {
			case vpcIDMatch:
				if len(selector[i].Spec.VMSelector) == 0 {
					selector[i].Spec.VMSelector = append(selector[i].Spec.VMSelector,
						v1alpha1.VirtualMachineSelector{VpcMatch: &v1alpha1.EntityMatch{MatchID: matchValue[i]}})
				} else {
					selector[i].Spec.VMSelector[0].VpcMatch.MatchID = matchValue[i]
				}
			case vpcNameMatch:
				if len(selector[i].Spec.VMSelector) == 0 {
					selector[i].Spec.VMSelector = append(selector[i].Spec.VMSelector,
						v1alpha1.VirtualMachineSelector{VpcMatch: &v1alpha1.EntityMatch{MatchName: matchValue[i]}})
				} else {
					selector[i].Spec.VMSelector[0].VpcMatch.MatchName = matchValue[i]
				}
			case vmIDMatch:
				if len(selector[i].Spec.VMSelector) == 0 {
					selector[i].Spec.VMSelector = append(selector[i].Spec.VMSelector,
						v1alpha1.VirtualMachineSelector{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: matchValue[i],
								},
							}})
				} else {
					selector[i].Spec.VMSelector[0].VMMatch[0].MatchID = matchValue[i]
				}
			case vmNameMatch:
				if len(selector[i].Spec.VMSelector) == 0 {
					selector[i].Spec.VMSelector = append(selector[i].Spec.VMSelector,
						v1alpha1.VirtualMachineSelector{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: matchValue[i],
								},
							}})
				} else {
					selector[i].Spec.VMSelector[0].VMMatch[0].MatchName = matchValue[i]
				}
			default:
				logf.Log.Error(errors.New("invalid matchKey"), "matchKey", matchKey)
			}
			var err error
			if selectorExists {
				err = k8sClient.Update(context.TODO(), &selector[i])
			} else {
				err = k8sClient.Create(context.TODO(), &selector[i])
			}
			Expect(err).ToNot(HaveOccurred())
		}
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

	tester := func(matchKey string, matchValue []string, selectorExists bool, cesNs []v1.Namespace, expectedResult [][]string) {
		applyEntitySelector(matchKey, matchValue, selectorExists, cesNs)
		time.Sleep(time.Duration(pollInterval) * time.Second)

		for i := range cesNs {
			var expectedResultValue []string
			if len(expectedResult) > 0 {
				expectedResultValue = expectedResult[i]
			}
			getVMsCmd := fmt.Sprintf(
				"get virtualmachine -n %s -o=jsonpath='{range.items[*]}{.metadata.name}{\"\\n\"}{end}'", cesNs[i].Name)
			output, err := kubeCtl.Cmd(getVMsCmd)
			Expect(err).ToNot(HaveOccurred())
			VMList := strings.Split(strings.Trim(output, "'"), "\n")
			actualResult := removeEmptyStr(VMList)
			logf.Log.Info("After test", "Actual Result ", actualResult)
			logf.Log.Info("After test", "Expected Result ", expectedResultValue)
			Expect(checkEqual(actualResult, expectedResultValue)).To(BeTrue())
		}
	}

	setParams := func(matchKey string, appendKey string) ([]string, [][]string) {
		var matchValue []string
		var expectedResult [][]string

		switch matchKey {
		case vpcIDMatch:
			for i := 0; i < numNamespace; i++ {
				matchValue = append(matchValue, vpcID+appendKey)
				expectedResult = append(expectedResult, [][]string{vmList}...)
			}
		case vpcNameMatch:
			for i := 0; i < numNamespace; i++ {
				matchValue = append(matchValue, vpcName+appendKey)
				expectedResult = append(expectedResult, [][]string{vmIDList}...)
			}
		case vmIDMatch:
			for i := 0; i < numNamespace; i++ {
				matchValue = append(matchValue, vmIDList[i]+appendKey)
				temp := []string{vmList[i]}
				expectedResult = append(expectedResult, [][]string{temp}...)
			}
		case vmNameMatch:
			for i := 0; i < numNamespace; i++ {
				matchValue = append(matchValue, vmNameList[i]+appendKey)
				temp := []string{vmList[i]}
				expectedResult = append(expectedResult, [][]string{temp}...)
			}
		default:
			logf.Log.Error(errors.New("invalid matchKey"), "matchKey", matchKey)
		}

		return matchValue, expectedResult
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
		numNamespace = len(vmList)
		cpaNamespace = &v1.Namespace{}
		cpaNamespace.Name = namespaceName + "-" + fmt.Sprintf("%x", rand.Int())
		// create namespaces for CES to test CES in different namespace from CPA.
		cesNamespace = make([]v1.Namespace, numNamespace)
		for i := 0; i < numNamespace; i++ {
			cesNamespace[i] = v1.Namespace{}
			cesNamespace[i].Name = namespaceName + "-" + fmt.Sprintf("%x", rand.Int())
		}
		selector = make([]v1alpha1.CloudEntitySelector, numNamespace)
		accountParameters := cloudVPC.GetCloudAccountParameters(testAccountName, cpaNamespace.Name, false)
		cloudProvider = strings.Split(cloudProviders, ",")[0]
		switch cloudProvider {
		case string(runtimev1alpha1.AWSCloudProvider):
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v12.ObjectMeta{
					Name:      testAccountName,
					Namespace: cpaNamespace.Name,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollInterval,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{accountParameters.Aws.Region},
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
				ObjectMeta: v12.ObjectMeta{
					Name:      testAccountName,
					Namespace: cpaNamespace.Name,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollInterval,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: []string{accountParameters.Azure.Location},
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

		// SDK call will automatically base64 encode data.
		secret = &corev1.Secret{
			ObjectMeta: v12.ObjectMeta{
				Name:      accountParameters.SecretRef.Name,
				Namespace: accountParameters.SecretRef.Namespace,
			},
			Data: map[string][]byte{accountParameters.SecretRef.Key: []byte(accountParameters.SecretRef.Credential)},
		}
		createNS(cpaNamespace)
		for i := 0; i < numNamespace; i++ {
			createNS(&cesNamespace[i])
		}
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
		deleteNS(cpaNamespace)
		for i := 0; i < numNamespace; i++ {
			deleteNS(&cesNamespace[i])
		}
	})

	DescribeTable("Testing entity selector",
		func(matchKey string) {
			By("Apply entity selector with valid match key")
			matchValue, expectedResult := setParams(matchKey, "")
			tester(matchKey, matchValue, false, cesNamespace, expectedResult)

			By("Change match key to invalid non-empty value")
			matchValue, _ = setParams(matchKey, "xyz")
			tester(matchKey, matchValue, true, cesNamespace, nil)

			By("Change match key back to valid value again")
			matchValue, expectedResult = setParams(matchKey, "")
			tester(matchKey, matchValue, true, cesNamespace, expectedResult)

		},
		Entry(focusAzure+":"+"VPC id match", vpcIDMatch),
		Entry(focusAzure+":"+"VM id match", vmIDMatch),
		Entry(focusAzure+":"+"VM name match", vmNameMatch),
		// vpcNameMatch test apply only to aws for now
		Entry("VPC name match", vpcNameMatch),
	)
})
