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

package accountmanager

import (
	"context"
	"fmt"
	"sync"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/testing/cloud"
	nephetypes "antrea.io/nephe/pkg/types"
)

var _ = Describe("Account Poller", func() {
	Context("Account poller workflow", func() {
		var (
			credentials               = "credentials"
			testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSecretNamespacedName  = types.NamespacedName{Namespace: "namespace", Name: "secret01"}
			testCesNamespacedName     = types.NamespacedName{Namespace: "namespace01", Name: "Ces01"}
			fakeClient                client.WithWatch
			secret                    *corev1.Secret
			account                   *v1alpha1.CloudProviderAccount
			ces                       *v1alpha1.CloudEntitySelector
			accountPollerObj          *accountPoller
			pollIntv                  uint
			mockCtrl                  *mock.Controller
			mockCloudInterface        *cloud.MockCloudInterface
		)
		BeforeEach(func() {
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))

			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			mockCtrl = mock.NewController(GinkgoT())
			mockCloudInterface = cloud.NewMockCloudInterface(mockCtrl)
			cloudInventory := inventory.InitInventory()
			accountPollerObj = &accountPoller{
				log:       logf.Log,
				Client:    fakeClient,
				inventory: cloudInventory,
				mutex:     sync.RWMutex{},
			}

			pollIntv = 1
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-east-1"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
			secret = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					"credentials": []byte(credential),
				},
			}
			ces = &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testCesNamespacedName.Name,
					Namespace: testCesNamespacedName.Namespace,
					OwnerReferences: []v1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/crdv1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       testAccountNamespacedName.Name,
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName:      testAccountNamespacedName.Name,
					AccountNamespace: testAccountNamespacedName.Namespace,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: "xyz",
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID:   "xyz",
									MatchName: "xyz",
								},
							},
						},
					},
				},
			}
			accountPollerObj.initVmSelectorCache()
			accountPollerObj.cloudInterface = mockCloudInterface
		})
		It("Add or update selector", func() {
			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)

			// Add selector.
			accountPollerObj.addOrUpdateSelector(ces)
			Expect(len(accountPollerObj.vmSelector.List())).To(Equal(1))

			// Update selector using same config.
			accountPollerObj.addOrUpdateSelector(ces)
			Expect(len(accountPollerObj.vmSelector.List())).To(Equal(1))

			selector := &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testCesNamespacedName.Name,
					Namespace: testCesNamespacedName.Namespace,
					OwnerReferences: []v1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/crdv1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       testAccountNamespacedName.Name,
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName:      testAccountNamespacedName.Name,
					AccountNamespace: testAccountNamespacedName.Namespace,
					VMSelector:       []v1alpha1.VirtualMachineSelector{},
				},
			}

			// Delete selector.
			accountPollerObj.addOrUpdateSelector(selector)
			Expect(len(accountPollerObj.vmSelector.List())).To(Equal(0))
		})
		It("Remove selector", func() {
			vmLabelsMap := map[string]string{
				labels.CloudAccountName:       testAccountNamespacedName.Name,
				labels.CloudAccountNamespace:  testAccountNamespacedName.Namespace,
				labels.CloudSelectorName:      testCesNamespacedName.Name,
				labels.CloudSelectorNamespace: testCesNamespacedName.Namespace,
				labels.CloudVmUID:             "ubuntu",
				labels.CloudVpcUID:            "vpcid",
				labels.VpcName:                "vpc",
			}
			vmList := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmObj := new(runtimev1alpha1.VirtualMachine)
			vmObj.Name = "ubuntu"
			vmObj.Namespace = testCesNamespacedName.Namespace
			vmObj.Labels = vmLabelsMap
			vmList["ubuntu"] = vmObj
			accountPollerObj.inventory.BuildVmCache(vmList, &testAccountNamespacedName, &testCesNamespacedName)
			vms := accountPollerObj.inventory.GetAllVms()
			Expect(len(vms)).To(Equal(1))

			// Delete VMs from cache.
			_ = accountPollerObj.inventory.DeleteVmsFromCache(&testAccountNamespacedName, &testCesNamespacedName)
			vms = accountPollerObj.inventory.GetAllVms()
			Expect(len(vms)).To(Equal(0))
		})
		It("Account polling", func() {
			vpcLabelsMap := map[string]string{
				labels.CloudAccountName:      testAccountNamespacedName.Name,
				labels.CloudAccountNamespace: testAccountNamespacedName.Namespace,
			}
			vpcList := make(map[string]*runtimev1alpha1.Vpc)
			vpcObj1 := new(runtimev1alpha1.Vpc)
			vpcObj1.Name = "obj1"
			vpcObj1.Namespace = testAccountNamespacedName.Namespace
			vpcObj1.Labels = vpcLabelsMap
			vpcObj1.Status.Region = "west"
			vpcObj1.Status.CloudId = "vpcid"
			vpcObj1.Status.CloudName = "vpc"
			vpcList["vpcid"] = vpcObj1

			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)

			accountPollerObj.accountNamespacedName = &testAccountNamespacedName
			mockCloudInterface.EXPECT().DoInventoryPoll(&testAccountNamespacedName).Return(nil).AnyTimes()
			mockCloudInterface.EXPECT().GetAccountStatus(&testAccountNamespacedName).Return(&v1alpha1.
				CloudProviderAccountStatus{}, nil).AnyTimes()

			// Invalid VPC.
			mockCloudInterface.EXPECT().GetAccountCloudInventory(&testAccountNamespacedName).Return(nil,
				fmt.Errorf("error")).Times(1)
			accountPollerObj.doAccountPolling()
			Expect(len(accountPollerObj.inventory.GetAllVpcs())).To(Equal(0))

			// Empty VPC.
			cloudInventory := nephetypes.CloudInventory{}
			mockCloudInterface.EXPECT().GetAccountCloudInventory(&testAccountNamespacedName).Return(&cloudInventory,
				nil).Times(1)
			accountPollerObj.doAccountPolling()
			Expect(len(accountPollerObj.inventory.GetAllVpcs())).To(Equal(0))

			// Valid VPC.
			cloudInventory = nephetypes.CloudInventory{VpcMap: vpcList}
			mockCloudInterface.EXPECT().GetAccountCloudInventory(&testAccountNamespacedName).Return(&cloudInventory,
				nil).Times(1)
			accountPollerObj.doAccountPolling()
			Expect(len(accountPollerObj.inventory.GetAllVpcs())).To(Equal(len(vpcList)))
			vmLabelsMap := map[string]string{
				labels.CloudAccountName:       testAccountNamespacedName.Name,
				labels.CloudAccountNamespace:  testAccountNamespacedName.Namespace,
				labels.CloudSelectorName:      testCesNamespacedName.Name,
				labels.CloudSelectorNamespace: testCesNamespacedName.Namespace,
				labels.CloudVmUID:             "ubuntu",
				labels.CloudVpcUID:            "vpcid",
				labels.VpcName:                "vpc",
			}
			vmList := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmObj := new(runtimev1alpha1.VirtualMachine)
			vmObj.Name = "ubuntu"
			vmObj.Namespace = testCesNamespacedName.Namespace
			vmObj.Labels = vmLabelsMap
			vmList["ubuntu"] = vmObj

			cloudInventory = nephetypes.CloudInventory{VpcMap: vpcList}
			// Invalid VMs.
			mockCloudInterface.EXPECT().GetAccountCloudInventory(&testAccountNamespacedName).Return(&cloudInventory,
				nil).Times(1)
			accountPollerObj.doAccountPolling()
			Expect(len(accountPollerObj.inventory.GetAllVms())).To(Equal(0))

			// Valid VMs.
			vmMap := make(map[types.NamespacedName]map[string]*runtimev1alpha1.VirtualMachine)
			vmMap[testCesNamespacedName] = vmList
			cloudInventory = nephetypes.CloudInventory{VpcMap: vpcList, VmMap: vmMap}
			mockCloudInterface.EXPECT().GetAccountCloudInventory(&testAccountNamespacedName).Return(&cloudInventory,
				nil).Times(1)
			accountPollerObj.doAccountPolling()
			Expect(len(accountPollerObj.inventory.GetAllVms())).To(Equal(len(vmList)))
		})
		It("Update account status", func() {
			accountPollerObj.accountNamespacedName = &testAccountNamespacedName
			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)

			// Valid CPA status.
			mockCloudInterface.EXPECT().GetAccountStatus(&testAccountNamespacedName).Return(&v1alpha1.
				CloudProviderAccountStatus{}, nil).Times(1)
			accountPollerObj.updateAccountStatus(mockCloudInterface)
			Expect(account.Status.Error).To(Equal(""))

			// Error CPA status.
			mockCloudInterface.EXPECT().GetAccountStatus(&testAccountNamespacedName).Return(&v1alpha1.
				CloudProviderAccountStatus{}, fmt.Errorf("error")).Times(1)
			accountPollerObj.updateAccountStatus(mockCloudInterface)
			accountError := &v1alpha1.CloudProviderAccount{}
			_ = fakeClient.Get(context.Background(), testAccountNamespacedName, accountError)
			Expect(accountError.Status.Error).To(ContainSubstring("error"))
		})
		It("Get vm selector match", func() {
			vmLabelsMap := map[string]string{
				labels.CloudAccountName:       testAccountNamespacedName.Name,
				labels.CloudAccountNamespace:  testAccountNamespacedName.Namespace,
				labels.CloudSelectorName:      testCesNamespacedName.Name,
				labels.CloudSelectorNamespace: testCesNamespacedName.Namespace,
				labels.CloudVmUID:             "ubuntu",
				labels.CloudVpcUID:            "vpcid",
				labels.VpcName:                "vpc",
			}
			vmStatus := &runtimev1alpha1.VirtualMachineStatus{
				Provider: runtimev1alpha1.AWSCloudProvider,
				State:    runtimev1alpha1.Running,
				Agented:  false,
				CloudId:  "xyz",
			}
			vmList := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmObj := new(runtimev1alpha1.VirtualMachine)
			vmObj.Name = "ubuntu"
			vmObj.Namespace = testCesNamespacedName.Namespace
			vmObj.Labels = vmLabelsMap
			vmObj.Status = *vmStatus
			vmList["ubuntu"] = vmObj
			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)

			accountPollerObj.addOrUpdateSelector(ces)
			Expect(len(accountPollerObj.vmSelector.List())).To(Equal(1))

			// Match cloud id.
			selector := accountPollerObj.getVmSelectorMatch(vmObj)
			Expect(selector).NotTo(BeNil())

			vmStatus = &runtimev1alpha1.VirtualMachineStatus{
				Provider:   runtimev1alpha1.AWSCloudProvider,
				State:      runtimev1alpha1.Running,
				Agented:    false,
				CloudName:  "xyz",
				CloudVpcId: "xyz",
			}
			vmObj.Status = *vmStatus

			// Match cloud Name and VPC id.
			selector = accountPollerObj.getVmSelectorMatch(vmObj)
			Expect(selector).NotTo(BeNil())

			vmStatus = &runtimev1alpha1.VirtualMachineStatus{
				Provider:   runtimev1alpha1.AWSCloudProvider,
				State:      runtimev1alpha1.Running,
				Agented:    false,
				CloudVpcId: "xyz",
			}
			vmObj.Status = *vmStatus

			// Match cloud VPC id.
			selector = accountPollerObj.getVmSelectorMatch(vmObj)
			Expect(selector).NotTo(BeNil())

			ces = &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testCesNamespacedName.Name,
					Namespace: testCesNamespacedName.Namespace,
					OwnerReferences: []v1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/crdv1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       testAccountNamespacedName.Name,
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName:      testAccountNamespacedName.Name,
					AccountNamespace: testAccountNamespacedName.Namespace,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID:   "xyz",
									MatchName: "xyz",
								},
							},
						},
					},
				},
			}

			// Match VM name and id.
			accountPollerObj.addOrUpdateSelector(ces)
			Expect(len(accountPollerObj.vmSelector.List())).To(Equal(1))
			vmStatus = &runtimev1alpha1.VirtualMachineStatus{
				Provider:  runtimev1alpha1.AWSCloudProvider,
				State:     runtimev1alpha1.Running,
				Agented:   false,
				CloudName: "xyz",
			}
			vmObj.Status = *vmStatus
			selector = accountPollerObj.getVmSelectorMatch(vmObj)
			Expect(selector).NotTo(BeNil())
		})
	})
})
