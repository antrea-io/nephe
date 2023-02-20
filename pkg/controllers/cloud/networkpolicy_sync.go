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

package cloud

import (
	"context"
	"fmt"

	"github.com/mohae/deepcopy"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

// sync synchronizes securityGroup memberships with cloud.
// Return true if cloud and controller has same membership.
func (sg *securityGroupImpl) syncImpl(csg cloudSecurityGroup, syncContent *securitygroup.SynchronizationContent,
	membershipOnly bool, r *NetworkPolicyReconciler) bool {
	log := r.Log.WithName("CloudSync")
	if syncContent == nil {
		// If syncContent is nil, explicitly set internal sg state to init, so that
		// AddressGroup or AppliedToGroup in cloud can be recreated.
		sg.state = securityGroupStateInit
	} else if syncContent != nil {
		sg.state = securityGroupStateCreated
		syncMembers := make([]*securitygroup.CloudResource, 0, len(syncContent.Members))
		for i := range syncContent.Members {
			syncMembers = append(syncMembers, &syncContent.Members[i])
		}

		cachedMembers := sg.members
		if len(syncMembers) > 0 && syncMembers[0].Type == securitygroup.CloudResourceTypeNIC {
			cachedMembers, _ = r.getNICsOfCloudResources(sg.members)
		}
		if compareCloudResources(cachedMembers, syncMembers) {
			return true
		} else {
			log.V(1).Info("Members are not in sync with cloud", "Name", sg.id.Name, "State", sg.state,
				"Sync members", syncMembers, "Cached SG members", cachedMembers)
		}
	} else if len(sg.members) == 0 {
		log.V(1).Info("Empty memberships", "Name", sg.id.Name)
		return true
	}

	if sg.state == securityGroupStateCreated {
		log.V(1).Info("Update securityGroup", "Name", sg.id.Name,
			"MembershipOnly", membershipOnly, "CloudSecurityGroup", syncContent)
		_ = sg.updateImpl(csg, nil, nil, membershipOnly, r)
	} else if sg.state == securityGroupStateInit {
		log.V(1).Info("Add securityGroup", "Name", sg.id.Name,
			"MembershipOnly", membershipOnly, "CloudSecurityGroup", syncContent)
		_ = sg.addImpl(csg, membershipOnly, r)
	}
	return false
}

// sync synchronizes addressSecurityGroup with cloud.
func (a *addrSecurityGroup) sync(syncContent *securitygroup.SynchronizationContent,
	r *NetworkPolicyReconciler) {
	log := r.Log.WithName("CloudSync")
	if a.deletePending {
		log.V(1).Info("AddressSecurityGroup pending delete", "Name", a.id.Name)
		return
	}
	_ = a.syncImpl(a, syncContent, true, r)
}

// sync synchronizes appliedToSecurityGroup with cloud.
func (a *appliedToSecurityGroup) sync(syncContent *securitygroup.SynchronizationContent,
	r *NetworkPolicyReconciler) {
	log := r.Log.WithName("CloudSync")
	if a.deletePending {
		log.V(1).Info("AppliedSecurityGroup pending delete", "Name", a.id.Name)
		return
	}
	if a.syncImpl(a, syncContent, false, r) && len(a.members) > 0 {
		a.hasMembers = true
	}
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		log.Error(err, "get networkPolicy by indexer", "Index", networkPolicyIndexerByAppliedToGrp, "Key", a.id.Name)
		return
	}
	items := make(map[string]int)
	for _, i := range nps {
		np := i.(*networkPolicy)

		if !np.computeRules(r) {
			log.V(1).Info("np not ready", "Name", np.Name, "Namespace", np.Namespace)
		}
		for _, iRule := range np.ingressRules {
			proto := 0
			if iRule.Protocol != nil {
				proto = *iRule.Protocol
			}
			port := 0
			if iRule.FromPort != nil {
				port = *iRule.FromPort
			}
			if proto > 0 || port > 0 {
				portStr := fmt.Sprintf("protocol=%v,port=%v", proto, port)
				items[portStr]++
			}
			for _, ip := range iRule.FromSrcIP {
				items[ip.String()]++
			}
			for _, sg := range iRule.FromSecurityGroups {
				items[sg.String()]++
			}
		}
		for _, eRule := range np.egressRules {
			proto := 0
			if eRule.Protocol != nil {
				proto = *eRule.Protocol
			}
			port := 0
			if eRule.ToPort != nil {
				port = *eRule.ToPort
			}
			if proto > 0 || port > 0 {
				portStr := fmt.Sprintf("protocol=%v,port=%v", proto, port)
				items[portStr]++
			}
			for _, ip := range eRule.ToDstIP {
				items[ip.String()]++
			}
			for _, sg := range eRule.ToSecurityGroups {
				items[sg.String()]++
			}
		}
	}

	if syncContent == nil {
		_ = a.updateAllRules(r)
		return
	}

	rules, err := r.cloudRuleIndexer.ByIndex(cloudRuleIndexerByAppliedToGrp, a.id.CloudResourceID.String())
	if err != nil {
		log.Error(err, "get cloudRule indexer", "Key", a.id.CloudResourceID.String())
		return
	}
	indexerUpdate := false
	cloudRuleMap := make(map[string]*securitygroup.CloudRule)
	for _, obj := range rules {
		rule := obj.(*securitygroup.CloudRule)
		cloudRuleMap[rule.Hash] = rule
	}

	// Rough compare rules
	for _, iRule := range syncContent.IngressRules {
		proto := 0
		if iRule.Protocol != nil {
			proto = *iRule.Protocol
		}
		port := 0
		if iRule.FromPort != nil {
			port = *iRule.FromPort
		}
		if proto > 0 || port > 0 {
			portStr := fmt.Sprintf("protocol=%v,port=%v", proto, port)
			items[portStr]--
		}
		for _, ip := range iRule.FromSrcIP {
			items[ip.String()]--
		}
		for _, sg := range iRule.FromSecurityGroups {
			items[sg.String()]--
		}
		i := deepcopy.Copy(iRule).(securitygroup.IngressRule)
		rule := &securitygroup.CloudRule{
			Rule:         &i,
			AppliedToGrp: a.id.CloudResourceID.String(),
		}
		rule.Hash = rule.GetHash()
		if _, found := cloudRuleMap[rule.Hash]; found {
			delete(cloudRuleMap, rule.Hash)
		} else {
			indexerUpdate = true
			_ = r.cloudRuleIndexer.Update(rule)
		}
	}
	for _, eRule := range syncContent.EgressRules {
		proto := 0
		if eRule.Protocol != nil {
			proto = *eRule.Protocol
		}
		port := 0
		if eRule.ToPort != nil {
			port = *eRule.ToPort
		}
		if proto > 0 || port > 0 {
			portStr := fmt.Sprintf("protocol=%v,port=%v", proto, port)
			items[portStr]--
		}
		for _, ip := range eRule.ToDstIP {
			items[ip.String()]--
		}
		for _, sg := range eRule.ToSecurityGroups {
			items[sg.String()]--
		}
		e := deepcopy.Copy(eRule).(securitygroup.EgressRule)
		rule := &securitygroup.CloudRule{
			Rule:         &e,
			AppliedToGrp: a.id.CloudResourceID.String(),
		}
		rule.Hash = rule.GetHash()
		if _, found := cloudRuleMap[rule.Hash]; found {
			delete(cloudRuleMap, rule.Hash)
		} else {
			indexerUpdate = true
			_ = r.cloudRuleIndexer.Update(rule)
		}
	}
	// remove rules no longer exist in cloud from indexer.
	for _, rule := range cloudRuleMap {
		indexerUpdate = true
		_ = r.cloudRuleIndexer.Delete(rule)
	}

	if indexerUpdate {
		_ = a.updateAllRules(r)
		return
	}

	for k, i := range items {
		if i != 0 {
			log.V(1).Info("Update appliedToSecurityGroup rules", "Name",
				a.id.CloudResourceID.String(), "CloudSecurityGroup", syncContent, "Item", k, "Diff", i)
			_ = a.updateAllRules(r)
			return
		}
	}

	// rule machines
	if len(nps) > 0 {
		if !a.hasRules {
			a.markDirty(r, false)
		}
		a.hasRules = true
	}
}

// syncWithCloud synchronizes security group in controller with cloud.
// This is a blocking call intentionally so that no other events are accepted during
// synchronization.
func (r *NetworkPolicyReconciler) syncWithCloud() {
	log := r.Log.WithName("CloudSync")

	if r.bookmarkCnt < npSyncReadyBookMarkCnt {
		return
	}
	log.V(1).Info("Started synchronizing cloud security groups with controller")
	ch := securitygroup.CloudSecurityGroup.GetSecurityGroupSyncChan()
	cloudAddrSGs := make(map[securitygroup.CloudResourceID]*securitygroup.SynchronizationContent)
	cloudAppliedToSGs := make(map[securitygroup.CloudResourceID]*securitygroup.SynchronizationContent)
	rscWithUnknownSGs := make(map[securitygroup.CloudResource]struct{})
	for content := range ch {
		log.V(1).Info("Sync from cloud", "SecurityGroup", content)
		indexer := r.addrSGIndexer
		sgNew := newAddrSecurityGroup
		if !content.MembershipOnly {
			indexer = r.appliedToSGIndexer
			sgNew = newAppliedToSecurityGroup
		}
		// Removes unknown sg.
		if _, ok, _ := indexer.GetByKey(content.Resource.CloudResourceID.String()); !ok {
			log.V(0).Info("Delete SecurityGroup not found in cache", "Name", content.Resource.Name, "MembershipOnly", content.MembershipOnly)
			state := securityGroupStateCreated
			_ = sgNew(&content.Resource, []*securitygroup.CloudResource{}, &state).delete(r)
			continue
		}
		// copy channel reference content to a local variable because we use pointer to
		// reference to cloud sg.
		cc := content
		if content.MembershipOnly {
			cloudAddrSGs[content.Resource.CloudResourceID] = &cc
		} else {
			cloudAppliedToSGs[content.Resource.CloudResourceID] = &cc
			for _, rsc := range content.MembersWithOtherSGAttached {
				rscWithUnknownSGs[rsc] = struct{}{}
			}
		}
	}
	r.syncedWithCloud = true
	for _, i := range r.addrSGIndexer.List() {
		sg := i.(*addrSecurityGroup)
		if sg.isIPBlocks() {
			continue
		}
		sg.sync(cloudAddrSGs[sg.getID()], r)
	}
	for _, i := range r.appliedToSGIndexer.List() {
		sg := i.(*appliedToSecurityGroup)
		sg.sync(cloudAppliedToSGs[sg.getID()], r)
	}
	// For cloud resource with any non nephe created SG, tricking plug-in to remove them by explicitly
	// updating a single instance of associated security group.
	for rsc := range rscWithUnknownSGs {
		i, ok, _ := r.cloudResourceNPTrackerIndexer.GetByKey(rsc.String())
		if !ok {
			log.Info("Unable to find resource in tracker", "CloudResource", rsc)
			continue
		}
		tracker := i.(*cloudResourceNPTracker)
		for _, sg := range tracker.appliedToSGs {
			_ = sg.update(nil, nil, r)
			break
		}
	}
	log.V(1).Info("Done synchronizing cloud security groups with controller")
}

// processBookMark process bookmark event and return true.
func (r *NetworkPolicyReconciler) processBookMark(event watch.EventType) bool {
	if event != watch.Bookmark {
		return false
	}
	if r.syncedWithCloud {
		return true
	}
	r.bookmarkCnt++
	r.syncWithCloud()
	return true
}

// getNICsOfCloudResources returns NICs of cloud resources if available.
func (r *NetworkPolicyReconciler) getNICsOfCloudResources(resources []*securitygroup.CloudResource) (
	[]*securitygroup.CloudResource, error) {
	if len(resources) == 0 {
		return nil, nil
	}
	if resources[0].Type == securitygroup.CloudResourceTypeNIC {
		return resources, nil
	}

	nics := make([]*securitygroup.CloudResource, 0, len(resources))
	for _, rsc := range resources {
		id := rsc.Name
		vmList := &v1alpha1.VirtualMachineList{}
		if err := r.List(context.TODO(), vmList, client.MatchingFields{virtualMachineIndexerByCloudID: id}); err != nil {
			return resources, err
		}
		for _, vm := range vmList.Items {
			for _, nic := range vm.Status.NetworkInterfaces {
				nics = append(nics, &securitygroup.CloudResource{Type: securitygroup.CloudResourceTypeNIC,
					CloudResourceID: securitygroup.CloudResourceID{Name: nic.Name, Vpc: rsc.Vpc}})
			}
		}
	}
	return nics, nil
}
