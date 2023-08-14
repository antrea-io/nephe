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

package networkpolicy

import (
	"fmt"
	"strings"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/types"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/inventory/indexer"
)

const (
	NetworkPolicyStatusApplied      = "Applied"
	AppliedSecurityGroupDeleteError = "Detaching/Deleting security group %v: %v"
)

// CloudResourceNpTracker tracks NetworkPolicies applied on cloud resource.
type CloudResourceNpTracker struct {
	// CloudResource is a cloud resource.
	CloudResource cloudresource.CloudResource
	// NamespacedName of the cloud resource.
	NamespacedName types.NamespacedName
	// if dirty is true, cloud resource needs to recompute NetworkPolicy status.
	dirty atomic.Value
	// appliedToSGs is list of appliedToSecurityGroup to which cloud resource is a member.
	appliedToSGs map[string]*appliedToSecurityGroup
	// previously appliedToSGs to track sg clean up.
	prevAppliedToSGs map[string]*appliedToSecurityGroup
	// store appliedTo to network policies mapping.
	appliedToToNpMap map[string][]types.NamespacedName
	// NpStatus map of network policy (ANP) name to their realization status.
	NpStatus map[string]*runtimev1alpha1.NetworkPolicyStatus
}

// newCloudResourceNpTracker create a tracker object using cloud resource object.
// Currently, VM is the only cloud resource which has a tracker.
func (r *NetworkPolicyReconciler) newCloudResourceNpTracker(rsc *cloudresource.CloudResource) *CloudResourceNpTracker {
	log := r.Log.WithName("NPTracker")

	vmItems, err := r.Inventory.GetVmFromIndexer(indexer.VirtualMachineByCloudResourceID, rsc.CloudResourceID.String())
	if err != nil || len(vmItems) == 0 {
		log.Error(err, "failed to create np tracker for cloud resource id", "id", rsc.String())
		return nil
	}

	// A vm is uniquely identified by its cloud assigned id and vpc id.
	// TODO: Same vm imported in multiple Namespaces is not supported.
	vm := vmItems[0].(*runtimev1alpha1.VirtualMachine)
	tracker := &CloudResourceNpTracker{
		appliedToSGs:     make(map[string]*appliedToSecurityGroup),
		prevAppliedToSGs: make(map[string]*appliedToSecurityGroup),
		appliedToToNpMap: make(map[string][]types.NamespacedName),
		CloudResource:    *rsc,
		// Use vm namespaced name for the tracker.
		NamespacedName: types.NamespacedName{Name: vm.Name, Namespace: vm.Namespace},
	}

	log.Info("Create np tracker", "namespacedName", tracker.NamespacedName)
	if err := r.npTrackerIndexer.Add(tracker); err != nil {
		log.Error(err, "add to CloudResourceNpTracker indexer")
		return nil
	}
	return tracker
}

// getCloudResourceNpTracker returns a tracker object by a cloud resource.
// If create flag is true, will create a tracker object if not created previously.
func (r *NetworkPolicyReconciler) getCloudResourceNpTracker(rsc *cloudresource.CloudResource, create bool) *CloudResourceNpTracker {
	if obj, found, _ := r.npTrackerIndexer.GetByKey(rsc.String()); found {
		return obj.(*CloudResourceNpTracker)
	} else if create {
		return r.newCloudResourceNpTracker(rsc)
	}
	return nil
}

// processCloudResourceNpTrackers computes the np status of trackers marked dirty.
func (r *NetworkPolicyReconciler) processCloudResourceNpTrackers() {
	log := r.Log.WithName("NPTracker")
	for _, obj := range r.npTrackerIndexer.List() {
		tracker := obj.(*CloudResourceNpTracker)
		if !tracker.isDirty() {
			continue
		}
		status := tracker.computeNpStatus(r)
		if len(tracker.appliedToSGs) == 0 && len(tracker.prevAppliedToSGs) == 0 {
			log.Info("Delete np tracker", "namespacedName", tracker.NamespacedName)
			_ = r.npTrackerIndexer.Delete(tracker)
			tracker.unmarkDirty()
			continue
		}

		npStatus, ok := status[tracker.NamespacedName.Namespace]
		if !ok {
			// Should not occur.
			log.Error(fmt.Errorf("failed to get NetworkPolicy status for tracker"), "",
				"tracker", tracker, "appliedToSGs", tracker.appliedToSGs, "appliedToToNpMap", tracker.appliedToToNpMap)
			tracker.unmarkDirty()
			continue
		}
		tracker.NpStatus = npStatus

		// After computing the status always update the indexer, so that secondary indexer
		// npTrackerIndexerByAppliedToGrp is re-indexed.
		_ = r.npTrackerIndexer.Delete(tracker)
		_ = r.npTrackerIndexer.Add(tracker)
		tracker.unmarkDirty()
	}
}

// update the appliedToGroup field of the tracker and marks it dirty for recomputation.
func (c *CloudResourceNpTracker) update(sg *appliedToSecurityGroup, isDelete bool, r *NetworkPolicyReconciler) error {
	_, found := c.appliedToSGs[sg.id.CloudResourceID.String()]
	if found != isDelete {
		return nil
	}
	c.markDirty()
	_ = r.npTrackerIndexer.Delete(c)
	if isDelete {
		delete(c.appliedToSGs, sg.id.CloudResourceID.String())
		c.prevAppliedToSGs[sg.id.CloudResourceID.String()] = sg
	} else {
		delete(c.appliedToToNpMap, sg.id.Name)
		delete(c.prevAppliedToSGs, sg.id.CloudResourceID.String())
		c.appliedToSGs[sg.id.CloudResourceID.String()] = sg
	}
	return r.npTrackerIndexer.Add(c)
}

func (c *CloudResourceNpTracker) markDirty() {
	c.dirty.Store(true)
}

func (c *CloudResourceNpTracker) unmarkDirty() {
	c.dirty.Store(false)
}

func (c *CloudResourceNpTracker) isDirty() bool {
	return c.dirty.Load().(bool)
}

// computeNpStatus returns networkPolicy status for a VM. Because a VM may be potentially imported
// on multiple namespaces, returned networkPolicy status is a map keyed by namespace.
func (c *CloudResourceNpTracker) computeNpStatus(r *NetworkPolicyReconciler) map[string]map[string]*runtimev1alpha1.NetworkPolicyStatus {
	log := r.Log.WithName("NPTracker")

	// retrieve all network policies related to cloud resource's applied groups
	npMap := make(map[interface{}]string)
	changed := false
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
			changed = true
			appliedToToNpMap[asg.id.Name] = append(appliedToToNpMap[asg.id.Name], namespacedName)
			npMap[i] = key
		}
	}

	// compute status of all network policies
	ret := make(map[string]map[string]*runtimev1alpha1.NetworkPolicyStatus)
	for i, asgName := range npMap {
		np := i.(*networkPolicy)
		npList, ok := ret[np.Namespace]
		if !ok {
			npList = make(map[string]*runtimev1alpha1.NetworkPolicyStatus)
			ret[np.Namespace] = npList
		}
		// An NetworkPolicy is applied when networkPolicy rules are ready to be sent
		// and appliedToSG of this cloud resource is ready.
		if status := np.getStatus(r); status != nil {
			npList[np.Name] = &runtimev1alpha1.NetworkPolicyStatus{
				Reason:      status.Error(),
				Realization: runtimev1alpha1.Failed,
			}
			if strings.Contains(status.Error(), InProgressStr) {
				npList[np.Name].Realization = runtimev1alpha1.InProgress
				npList[np.Name].Reason = NoneStr
			}
			continue
		}
		i, found, _ := r.appliedToSGIndexer.GetByKey(asgName)
		if !found {
			npList[np.Name] = &runtimev1alpha1.NetworkPolicyStatus{
				Reason:      asgName + "=Internal Error ",
				Realization: runtimev1alpha1.Failed,
			}
			continue
		}
		asg := i.(*appliedToSecurityGroup)
		if status := asg.getStatus(); status != nil {
			npList[np.Name] = &runtimev1alpha1.NetworkPolicyStatus{
				Reason:      asgName + "=" + status.Error(),
				Realization: runtimev1alpha1.Failed,
			}
			if strings.Contains(status.Error(), InProgressStr) {
				npList[np.Name].Realization = runtimev1alpha1.InProgress
				npList[np.Name].Reason = NoneStr
			}
			continue
		}
		npList[np.Name] = &runtimev1alpha1.NetworkPolicyStatus{
			Reason:      asgName + "=" + NetworkPolicyStatusApplied,
			Realization: runtimev1alpha1.Success,
		}
	}

	newPrevSgs := make(map[string]*appliedToSecurityGroup)
	for k, v := range c.prevAppliedToSGs {
		newPrevSgs[k] = v
	}

	for _, asg := range newPrevSgs {
		changed = true
		if asg.status == nil {
			delete(c.appliedToToNpMap, asg.id.Name)
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
					npList = make(map[string]*runtimev1alpha1.NetworkPolicyStatus)
					ret[np.Namespace] = npList
				}
				npList[np.Name] = &runtimev1alpha1.NetworkPolicyStatus{
					Reason:      errMsg,
					Realization: runtimev1alpha1.Failed,
				}
				if strings.Contains(asg.status.Error(), InProgressStr) {
					npList[np.Name].Realization = runtimev1alpha1.InProgress
					npList[np.Name].Reason = NoneStr
				}
				namespacedName := types.NamespacedName{Namespace: np.Namespace, Name: np.Name}
				appliedToToNpMap[asg.id.Name] = append(appliedToToNpMap[asg.id.Name], namespacedName)
			}
		} else {
			if namespacedNames, ok := c.appliedToToNpMap[asg.id.Name]; ok {
				for _, namespacedName := range namespacedNames {
					npList, ok := ret[namespacedName.Namespace]
					if !ok {
						npList = make(map[string]*runtimev1alpha1.NetworkPolicyStatus)
						ret[namespacedName.Namespace] = npList
					}
					npList[namespacedName.Name] = &runtimev1alpha1.NetworkPolicyStatus{
						Reason:      errMsg,
						Realization: runtimev1alpha1.Failed,
					}
					if strings.Contains(asg.status.Error(), InProgressStr) {
						npList[namespacedName.Name].Realization = runtimev1alpha1.InProgress
						npList[namespacedName.Name].Reason = NoneStr
					}
					appliedToToNpMap[asg.id.Name] = append(appliedToToNpMap[asg.id.Name], namespacedName)
				}
			}
		}
	}

	// When a NP is deleted, but the appliedToGroup delete is not received yet, then NP Status is computed based on
	// previous appliedToSgs. Suppose prevAppliedToSGs is nil, then NP status will be empty.
	// An additional check required to compute the NP status, based on the appliedToToNpMap and appliedToSGs.
	// If there are any entries in appliedToToNpMap with an appliedToSG, set the np status as in progress, since
	// appliedToGroup is in a transient state.
	if !changed && len(c.appliedToToNpMap) > 0 {
		for _, asg := range c.appliedToSGs {
			if namespacedNames, ok := c.appliedToToNpMap[asg.id.Name]; ok {
				for _, namespacedName := range namespacedNames {
					npList, ok := ret[namespacedName.Namespace]
					if !ok {
						npList = make(map[string]*runtimev1alpha1.NetworkPolicyStatus)
						ret[namespacedName.Namespace] = npList
					}
					npList[namespacedName.Name] = &runtimev1alpha1.NetworkPolicyStatus{
						Reason:      NoneStr,
						Realization: runtimev1alpha1.InProgress,
					}
					appliedToToNpMap[asg.id.Name] = append(appliedToToNpMap[asg.id.Name], namespacedName)
				}
			}
		}
	}

	// Update the map with the latest data.
	c.appliedToToNpMap = appliedToToNpMap
	// Update the previous appliedToSgs.
	if len(newPrevSgs) != len(c.prevAppliedToSGs) {
		c.prevAppliedToSGs = newPrevSgs
	}
	return ret
}
