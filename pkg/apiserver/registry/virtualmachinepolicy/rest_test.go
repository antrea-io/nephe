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

package virtualmachinepolicy

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/tools/cache"

	"antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/networkpolicy"
	"antrea.io/nephe/pkg/logging"
)

func TestVirtualMachinePolicy(t *testing.T) {
	logging.SetDebugLog(true)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Virtual Machine Policy Suite")
}

var (
	target = &types.NamespacedName{
		Namespace: "default",
		Name:      "targetname",
	}
)

var _ = Describe("VirtualMachinePolicy test", func() {
	log := logging.GetLogger("VirtualMachinePolicy")
	var virtualMachinePolicyIndexer1 = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			tracker := obj.(*networkpolicy.CloudResourceNpTracker)
			return tracker.CloudResource.String(), nil
		},
		cache.Indexers{
			networkpolicy.NpTrackerIndexerByNamespacedName: func(obj interface{}) ([]string, error) {
				tracker := obj.(*networkpolicy.CloudResourceNpTracker)
				return []string{tracker.NamespacedName.String()}, nil
			},
		})
	var virtualMachinePolicyIndexer2 = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			tracker := obj.(*networkpolicy.CloudResourceNpTracker)
			return tracker.CloudResource.String(), nil
		},
		cache.Indexers{
			networkpolicy.NpTrackerIndexerByNamespacedName: func(obj interface{}) ([]string, error) {
				tracker := obj.(*networkpolicy.CloudResourceNpTracker)
				return []string{tracker.NamespacedName.String()}, nil
			},
		})
	var virtualMachinePolicyIndexer3 = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			tracker := obj.(*networkpolicy.CloudResourceNpTracker)
			return tracker.CloudResource.String(), nil
		},
		cache.Indexers{
			networkpolicy.NpTrackerIndexerByNamespacedName: func(obj interface{}) ([]string, error) {
				tracker := obj.(*networkpolicy.CloudResourceNpTracker)
				return []string{tracker.NamespacedName.String()}, nil
			},
		})
	// To test List functionality with Namespace.
	var virtualMachinePolicyIndexer4 = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			tracker := obj.(*networkpolicy.CloudResourceNpTracker)
			return tracker.CloudResource.String(), nil
		},
		cache.Indexers{
			networkpolicy.NpTrackerIndexerByNamespace: func(obj interface{}) ([]string, error) {
				tracker := obj.(*networkpolicy.CloudResourceNpTracker)
				log.Info("Testing indexer", "tracker", tracker)
				return []string{tracker.NamespacedName.Namespace}, nil
			},
		})
	// To test get functionality for an error case.
	var virtualMachinePolicyIndexer5 = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			tracker := obj.(*networkpolicy.CloudResourceNpTracker)
			return tracker.CloudResource.String(), nil
		},
		cache.Indexers{
			networkpolicy.NpTrackerIndexerByNamespacedName: func(obj interface{}) ([]string, error) {
				tracker := obj.(*networkpolicy.CloudResourceNpTracker)
				return []string{tracker.NamespacedName.String()}, nil
			},
		})
	var npstatus1 = make(map[string]*v1alpha1.NetworkPolicyStatus)
	var npstatus2 = make(map[string]*v1alpha1.NetworkPolicyStatus)
	var npstatus3 = make(map[string]*v1alpha1.NetworkPolicyStatus)
	npstatus1["test1"] = &v1alpha1.NetworkPolicyStatus{
		Reason:      NoneString,
		Realization: v1alpha1.Success,
	}
	npstatus2["test1"] = &v1alpha1.NetworkPolicyStatus{
		Reason:      NoneString,
		Realization: v1alpha1.Success,
	}
	npstatus3["test1"] = &v1alpha1.NetworkPolicyStatus{
		Reason:      NoneString,
		Realization: v1alpha1.Success,
	}
	npstatus2["test2"] = &v1alpha1.NetworkPolicyStatus{
		Reason:      NoneString,
		Realization: v1alpha1.InProgress,
	}

	npstatus3["test2"] = &v1alpha1.NetworkPolicyStatus{
		Reason:      NoneString,
		Realization: v1alpha1.InProgress,
	}
	npstatus3["test3"] = &v1alpha1.NetworkPolicyStatus{
		Reason:      "error",
		Realization: v1alpha1.Failed,
	}
	cacheTest1 := &networkpolicy.CloudResourceNpTracker{
		NamespacedName: *target,
		NpStatus:       npstatus1,
	}
	cacheTest2 := &networkpolicy.CloudResourceNpTracker{
		NamespacedName: *target,
		NpStatus:       npstatus2,
	}
	cacheTest3 := &networkpolicy.CloudResourceNpTracker{
		NamespacedName: *target,
		NpStatus:       npstatus3,
	}
	expectedPolicy1 := &v1alpha1.VirtualMachinePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		Status: v1alpha1.VirtualMachinePolicyStatus{
			Realization: v1alpha1.Success,
			NetworkPolicyDetails: map[string]*v1alpha1.NetworkPolicyStatus{
				"test1": {
					Realization: v1alpha1.Success,
					Reason:      NoneString,
				},
			},
		},
	}
	expectedPolicy2 := &v1alpha1.VirtualMachinePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		Status: v1alpha1.VirtualMachinePolicyStatus{
			Realization: v1alpha1.InProgress,
			NetworkPolicyDetails: map[string]*v1alpha1.NetworkPolicyStatus{
				"test1": {
					Realization: v1alpha1.Success,
					Reason:      NoneString,
				},
				"test2": {
					Realization: v1alpha1.InProgress,
					Reason:      NoneString,
				},
			},
		},
	}
	expectedPolicy3 := &v1alpha1.VirtualMachinePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		Status: v1alpha1.VirtualMachinePolicyStatus{
			Realization: v1alpha1.Failed,
			NetworkPolicyDetails: map[string]*v1alpha1.NetworkPolicyStatus{
				"test1": {
					Realization: v1alpha1.Success,
					Reason:      NoneString,
				},
				"test2": {
					Realization: v1alpha1.InProgress,
					Reason:      NoneString,
				},
				"test3": {
					Realization: v1alpha1.Failed,
					Reason:      "error",
				},
			},
		},
	}
	expectedPolicies := []*v1alpha1.VirtualMachinePolicy{
		expectedPolicy1,
		expectedPolicy2,
		expectedPolicy3,
	}
	Describe("Test Get function of Rest", func() {
		_ = virtualMachinePolicyIndexer1.Add(cacheTest1)
		_ = virtualMachinePolicyIndexer2.Add(cacheTest2)
		_ = virtualMachinePolicyIndexer3.Add(cacheTest3)
		var virtualMachinePolicyIndexers = []cache.Indexer{virtualMachinePolicyIndexer1,
			virtualMachinePolicyIndexer2, virtualMachinePolicyIndexer3}
		It("Test Get function of Rest", func() {
			for i, virtualMachinePolicyIndexer := range virtualMachinePolicyIndexers {
				rest := NewREST(virtualMachinePolicyIndexer, log)
				actualGroupList, err := rest.Get(request.NewDefaultContext(), target.Name, &metav1.GetOptions{})
				Expect(err).Should(BeNil())
				Expect(actualGroupList).To(Equal(expectedPolicies[i]))
			}
		})
		It("Test Get requires namespace", func() {
			for _, virtualMachinePolicyIndexer := range virtualMachinePolicyIndexers {
				rest := NewREST(virtualMachinePolicyIndexer, log)
				_, err := rest.Get(context.TODO(), target.Name, &metav1.GetOptions{})
				Expect(err).ShouldNot(BeNil())
			}
		})
		It("Test Get returns object not found", func() {
			rest := NewREST(virtualMachinePolicyIndexer5, log)
			_, err := rest.Get(request.NewDefaultContext(), target.Name, &metav1.GetOptions{})
			Expect(err).ShouldNot(BeNil())
		})
	})

	Describe("Test List function of Rest", func() {
		_ = virtualMachinePolicyIndexer4.Add(cacheTest1)
		expectedPoliyList := &v1alpha1.VirtualMachinePolicyList{
			Items: []v1alpha1.VirtualMachinePolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: target.Namespace,
						Name:      target.Name,
					},
					Status: v1alpha1.VirtualMachinePolicyStatus{
						Realization: v1alpha1.Success,
						NetworkPolicyDetails: map[string]*v1alpha1.NetworkPolicyStatus{
							"test1": {
								Realization: v1alpha1.Success,
								Reason:      NoneString,
							},
						},
					},
				},
			},
		}
		It("Should return the List result of Rest", func() {
			rest := NewREST(virtualMachinePolicyIndexer1, log)
			actualObj, err := rest.List(context.TODO(), &internalversion.ListOptions{})
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedPoliyList))
		})
		It("Should return the List result of Rest by Namespace", func() {
			rest := NewREST(virtualMachinePolicyIndexer4, log)
			actualObj, err := rest.List(request.NewDefaultContext(), &internalversion.ListOptions{})
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedPoliyList))
		})
	})
})
