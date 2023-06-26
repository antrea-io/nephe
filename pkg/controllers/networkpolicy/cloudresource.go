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

package networkpolicy

import (
	"fmt"
	"reflect"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/types"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/inventory/indexer"
)

const (
	NetworkPolicyStatusApplied = "applied"
)

var (
	resourceNPStatusSetter = map[cloudresource.CloudResourceType]func(tracker *cloudResourceNPTracker,
		reconciler *NetworkPolicyReconciler) (bool, error){
		cloudresource.CloudResourceTypeVM: vmNPStatusSetter,
	}
)

const (
	AppliedSecurityGroupDeleteError = "Detaching/Deleting security group %v: %v"
)

func vmNPStatusSetter(tracker *cloudResourceNPTracker, r *NetworkPolicyReconciler) (bool, error) {
	log := r.Log.WithName("NPTracker")
	status := tracker.computeNPStatus(r)
	updated := false

	vmItems, err := r.Inventory.GetVmFromIndexer(indexer.VirtualMachineByCloudId, tracker.cloudResource.Name)
	if err != nil {
		log.Error(err, "failed to get VM from VM cache")
		return false, err
	}
	for _, item := range vmItems {
		vm := item.(*runtimev1alpha1.VirtualMachine)
		// Check if network policies are available for VM namespace.
		npStatus, ok := status[vm.Namespace]

		// Check if VMP object exists.
		indexKey := types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}
		obj, found, _ := r.virtualMachinePolicyIndexer.GetByKey(indexKey.String())

		// No VMP object and no NPStatus for Namespace, ignore.
		if !ok && !found {
			continue
		}

		var cache *NetworkPolicyStatus
		if found {
			cache = obj.(*NetworkPolicyStatus)
		} else {
			cache = newNetworkPolicyStatus(indexKey.Namespace, indexKey.Name)
		}
		// policy status did not change.
		if ok && reflect.DeepEqual(cache.NPStatus, npStatus) {
			continue
		}

		// cache operation.
		if len(npStatus) != 0 {
			cache.NPStatus = npStatus
			if err := r.virtualMachinePolicyIndexer.Update(cache); err != nil {
				// mark dirty and retry later on error.
				tracker.markDirty()
				continue
			}
			log.V(1).Info("Update vmp status", "resource", cache.String(), "status", npStatus)
		} else {
			if err := r.virtualMachinePolicyIndexer.Delete(cache); err != nil {
				tracker.markDirty()
				continue
			}
			log.V(1).Info("Delete vmp status", "resource", cache.String())
		}
		updated = true
	}
	return updated, nil
}

type NetworkPolicyStatus struct {
	// uniquely identify a resource crd object.
	types.NamespacedName
	// map of network policy (ANP) name to their realization status.
	NPStatus map[string]string
}

func newNetworkPolicyStatus(namespace, name string) *NetworkPolicyStatus {
	npStatus := &NetworkPolicyStatus{
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
		NPStatus:       make(map[string]string),
	}
	return npStatus
}

// cloudResourceNPTracker tracks NetworkPolicies applied on cloud resource.
type cloudResourceNPTracker struct {
	// cloudResource is a cloud resource
	cloudResource cloudresource.CloudResource
	// if dirty is true, cloud resource needs to recompute NetworkPolicy status.
	dirty atomic.Value
	// appliedToSGs is list of appliedToSecurityGroup to which cloud resource is a member.
	appliedToSGs map[string]*appliedToSecurityGroup
	// previously appliedToSGs to track sg clean up.
	prevAppliedToSGs map[string]*appliedToSecurityGroup
	// store appliedTo to network policies mapping.
	appliedToToNpMap map[string][]types.NamespacedName
}

func (r *NetworkPolicyReconciler) newCloudResourceNPTracker(rsc *cloudresource.CloudResource) *cloudResourceNPTracker {
	log := r.Log.WithName("NPTracker")
	tracker := &cloudResourceNPTracker{
		appliedToSGs:     make(map[string]*appliedToSecurityGroup),
		prevAppliedToSGs: make(map[string]*appliedToSecurityGroup),
		appliedToToNpMap: make(map[string][]types.NamespacedName),
		cloudResource:    *rsc,
	}
	if err := r.cloudResourceNPTrackerIndexer.Add(tracker); err != nil {
		log.Error(err, "Add to cloudResourceNPTracker indexer")
		return nil
	}
	return tracker
}

func (r *NetworkPolicyReconciler) getCloudResourceNPTracker(rsc *cloudresource.CloudResource, create bool) *cloudResourceNPTracker {
	if obj, found, _ := r.cloudResourceNPTrackerIndexer.GetByKey(rsc.String()); found {
		return obj.(*cloudResourceNPTracker)
	} else if create {
		return r.newCloudResourceNPTracker(rsc)
	}
	return nil
}

func (r *NetworkPolicyReconciler) processCloudResourceNPTrackers() {
	log := r.Log.WithName("NPTracker")
	for _, i := range r.cloudResourceNPTrackerIndexer.List() {
		tracker := i.(*cloudResourceNPTracker)
		if !tracker.isDirty() {
			continue
		}
		_, err := resourceNPStatusSetter[tracker.cloudResource.Type](tracker, r)
		if err != nil {
			log.Error(err, "Set cloud resource NetworkPolicy status", "crd", tracker.cloudResource)
			continue
		}
		if len(tracker.appliedToSGs) == 0 && len(tracker.prevAppliedToSGs) == 0 {
			log.V(1).Info("Delete np tracker", "Name", tracker.cloudResource.String())
			_ = r.cloudResourceNPTrackerIndexer.Delete(tracker)
			continue
		}
		tracker.unmarkDirty()
	}
}

func (c *cloudResourceNPTracker) update(sg *appliedToSecurityGroup, isDelete bool, r *NetworkPolicyReconciler) error {
	_, found := c.appliedToSGs[sg.id.CloudResourceID.String()]
	if found != isDelete {
		return nil
	}
	c.markDirty()
	_ = r.cloudResourceNPTrackerIndexer.Delete(c)
	if isDelete {
		delete(c.appliedToSGs, sg.id.CloudResourceID.String())
		c.prevAppliedToSGs[sg.id.CloudResourceID.String()] = sg
	} else {
		delete(c.prevAppliedToSGs, sg.id.CloudResourceID.String())
		c.appliedToSGs[sg.id.CloudResourceID.String()] = sg
	}
	return r.cloudResourceNPTrackerIndexer.Add(c)
}

func (c *cloudResourceNPTracker) markDirty() {
	c.dirty.Store(true)
}

func (c *cloudResourceNPTracker) unmarkDirty() {
	c.dirty.Store(false)
}

func (c *cloudResourceNPTracker) isDirty() bool {
	return c.dirty.Load().(bool)
}

// computeNPStatus returns networkPolicy status for a VM. Because a VM may be potentially imported
// on multiple namespaces, returned networkPolicy status is a map keyed by namespace.
func (c *cloudResourceNPTracker) computeNPStatus(r *NetworkPolicyReconciler) map[string]map[string]string {
	log := r.Log.WithName("NPTracker")

	// retrieve all network policies related to cloud resource's applied groups
	npMap := make(map[interface{}]string)
	appliedToToNpMap := make(map[string][]types.NamespacedName)
	for key, asg := range c.appliedToSGs {
		nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, asg.id.Name)
		if err != nil {
			log.Error(err, "Get networkPolicy indexer by index", "index", networkPolicyIndexerByAppliedToGrp,
				"key", asg)
			continue
		}
		// Not considering cloud resources belongs to multiple AppliedToGroups of same NetworkPolicy.
		for _, i := range nps {
			namespacedName := types.NamespacedName{Namespace: i.(*networkPolicy).Namespace, Name: i.(*networkPolicy).Name}
			appliedToToNpMap[asg.id.Name] = append(appliedToToNpMap[asg.id.Name], namespacedName)
			npMap[i] = key
		}
	}

	// compute status of all network policies
	ret := make(map[string]map[string]string)
	for i, asgName := range npMap {
		np := i.(*networkPolicy)
		npList, ok := ret[np.Namespace]
		if !ok {
			npList = make(map[string]string)
			ret[np.Namespace] = npList
		}
		// An NetworkPolicy is applied when
		// networkPolicy rules are ready to be sent, and
		// appliedToSG of this cloud resource is ready.
		if status := np.getStatus(r); status != nil {
			npList[np.Name] = status.Error()
			continue
		}
		i, found, _ := r.appliedToSGIndexer.GetByKey(asgName)
		if !found {
			npList[np.Name] = asgName + "=Internal Error "
			continue
		}
		asg := i.(*appliedToSecurityGroup)
		if status := asg.getStatus(); status != nil {
			npList[np.Name] = asgName + "=" + status.Error()
			continue
		}
		npList[np.Name] = asgName + "=" + NetworkPolicyStatusApplied
	}

	newPrevSgs := make(map[string]*appliedToSecurityGroup)
	for k, v := range c.prevAppliedToSGs {
		newPrevSgs[k] = v
	}

	for _, asg := range newPrevSgs {
		if asg.status == nil {
			delete(c.appliedToToNpMap, asg.id.CloudResourceID.String())
			delete(newPrevSgs, asg.id.CloudResourceID.String())
			continue
		}
		nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, asg.id.Name)
		if err != nil {
			log.Error(err, "Get networkPolicy indexer by index", "index", networkPolicyIndexerByAppliedToGrp,
				"key", asg.id.Name)
			continue
		}
		errMsg := fmt.Sprintf(AppliedSecurityGroupDeleteError, asg.id.CloudResourceID.String(), asg.status.Error())
		if len(nps) != 0 {
			for _, i := range nps {
				np := i.(*networkPolicy)
				npList, ok := ret[np.Namespace]
				if !ok {
					npList = make(map[string]string)
					ret[np.Namespace] = npList
				}
				npList[np.Name] = errMsg
			}
		} else {
			if namespacedNames, ok := c.appliedToToNpMap[asg.id.Name]; ok {
				for _, namespacedName := range namespacedNames {
					npList, ok := ret[namespacedName.Namespace]
					if !ok {
						npList = make(map[string]string)
						ret[namespacedName.Namespace] = npList
					}
					npList[namespacedName.Name] = errMsg
					appliedToToNpMap[asg.id.Name] = append(appliedToToNpMap[asg.id.Name], namespacedName)
				}
			}
		}
	}
	// Update the map with the latest data.
	c.appliedToToNpMap = appliedToToNpMap

	if len(newPrevSgs) != len(c.prevAppliedToSGs) {
		_ = r.cloudResourceNPTrackerIndexer.Delete(c)
		c.prevAppliedToSGs = newPrevSgs
		_ = r.cloudResourceNPTrackerIndexer.Add(c)
	}
	return ret
}
