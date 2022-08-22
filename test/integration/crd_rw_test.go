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
	"path"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/test/utils"
)

var _ = Describe(fmt.Sprintf("%s,%s: Basic CRD Read-Write", focusAws, focusAzure), func() {
	const (
		nameSpaceName   = "test-crd-rw"
		testAccountName = "test-account"
	)

	var (
		namespace     v1.Namespace
		secret        *corev1.Secret
		account       *v1alpha1.CloudProviderAccount
		cloudProvider string

		selector = &v1alpha1.CloudEntitySelector{
			ObjectMeta: v12.ObjectMeta{
				Name:      testAccountName,
				Namespace: nameSpaceName,
			},
			Spec: v1alpha1.CloudEntitySelectorSpec{
				AccountName: testAccountName,
				VMSelector: []v1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &v1alpha1.EntityMatch{
							MatchID: "",
						},
					},
				},
			},
		}
	)

	AfterEach(func() {
		result := CurrentGinkgoTestDescription()
		if result.Failed {
			if len(supportBundleDir) > 0 {
				logf.Log.Info("Collect support bundles for test failure.")
				fileName := utils.GenerateNameFromText(result.FullTestText, testFocus)
				utils.CollectSupportBundle(kubeCtl, path.Join(supportBundleDir, fileName))
			}
		}
	})

	setCloudAccount := func() {
		accountParameters := cloudVPC.GetCloudAccountParameters(testAccountName, nameSpaceName, cloudCluster)
		cloudProvider = strings.Split(cloudProviders, ",")[0]
		switch cloudProvider {
		case string(v1alpha1.AWSCloudProvider):
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v12.ObjectMeta{
					Name:      testAccountName,
					Namespace: nameSpaceName,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
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
					Namespace: nameSpaceName,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
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
		}
		// SDK call will automatically base64 encode data.
		secret = &corev1.Secret{
			ObjectMeta: v12.ObjectMeta{
				Name:      accountParameters.SecretRef.Name,
				Namespace: accountParameters.SecretRef.Namespace,
			},
			Data: map[string][]byte{accountParameters.SecretRef.Key: []byte(accountParameters.SecretRef.Credential)},
		}
	}

	createNS := func() {
		namespace.Name = nameSpaceName
		logf.Log.Info("Create", "namespace", namespace.Name)
		err := k8sClient.Create(context.TODO(), &namespace)
		Expect(err).ToNot(HaveOccurred())
	}
	deleteNS := func() {
		logf.Log.Info("Delete", "namespace", namespace.Name)
		err := k8sClient.Delete(context.TODO(), &namespace)
		Expect(err).ToNot(HaveOccurred())
	}
	createAccount := func() {
		setCloudAccount()
		logf.Log.Info("Create", "secret", secret.Name)
		err := k8sClient.Create(context.TODO(), secret)
		Expect(err).ToNot(HaveOccurred())
		logf.Log.Info("Create", "account", account.Name)
		err = k8sClient.Create(context.TODO(), account)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(5 * time.Second)
	}
	deleteAccount := func() {
		logf.Log.Info("Delete", "account", account.Name)
		err := k8sClient.Delete(context.TODO(), account)
		Expect(err).ToNot(HaveOccurred())
	}

	table.DescribeTable("Modifying CRDs",
		func(crd client.Object, createNS, deleteNS, createAccount, deleteAccount func()) {
			if createNS != nil {
				createNS()
			}
			accessor, _ := meta.Accessor(crd)
			accessor.SetName("test")
			accessor.SetNamespace(namespace.Name)

			if createAccount != nil {
				createAccount()
			}

			By("Can be added")
			err := k8sClient.Create(context.TODO(), crd)
			Expect(err).ToNot(HaveOccurred())

			By("Can be retrieved")
			key := client.ObjectKeyFromObject(crd)
			err = k8sClient.Get(context.TODO(), key, crd)
			Expect(err).ToNot(HaveOccurred())

			By("Can be deleted")
			err = k8sClient.Delete(context.TODO(), crd)
			Expect(err).ToNot(HaveOccurred())

			if deleteAccount != nil {
				deleteAccount()
			}

			if deleteNS != nil {
				deleteNS()
			}
		},
		table.Entry("VirtualMachine", &v1alpha1.VirtualMachine{}, createNS, nil, nil, nil),
		table.Entry("CloudEntitySelector", selector, nil, nil, createAccount, deleteAccount),
		table.Entry("ExternalEntity", &antreatypes.ExternalEntity{}, nil, deleteNS, nil, nil),
	)
})
