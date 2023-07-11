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
	"time"

	"k8s.io/apimachinery/pkg/watch"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/securitygroup"
	"antrea.io/nephe/pkg/inventory/indexer"
)

var (
	lastSyncTime = time.Now().Unix()
)

// syncImpl synchronizes securityGroup memberships with cloud.
// Return true if cloud and controller has same membership.
func (s *securityGroupImpl) syncImpl(csg cloudSecurityGroup, syncContent *cloudresource.SynchronizationContent,
	membershipOnly bool, r *NetworkPolicyReconciler) bool {
	log := r.Log.WithName("CloudSync")
	if syncContent == nil {
		// If syncContent is nil, explicitly set internal sg state to init, so that
		// AddressGroup or AppliedToGroup in cloud can be recreated.
		s.state = securityGroupStateInit
	} else {
		s.state = securityGroupStateCreated
		syncMembers := make([]*cloudresource.CloudResource, 0, len(syncContent.Members))
		for i := range syncContent.Members {
			syncMembers = append(syncMembers, &syncContent.Members[i])
		}

		cachedMembers := s.members
		if len(syncMembers) > 0 && syncMembers[0].Type == cloudresource.CloudResourceTypeNIC {
			cachedMembers, _ = r.getNICsOfCloudResources(s.members)
		}
		if !membershipOnly && len(syncContent.MembersWithOtherSGAttached) > 0 {
			log.V(1).Info("AppliedTo members have non-nephe sg attached", "Name", s.id.Name, "State", s.state,
				"members", syncContent.MembersWithOtherSGAttached)
		} else if compareCloudResources(cachedMembers, syncMembers) {
			return true
		} else {
			log.V(1).Info("Members are not in sync with cloud", "Name", s.id.Name, "State", s.state,
				"Sync members", syncMembers, "Cached SG members", cachedMembers)
		}
	}

	if s.state == securityGroupStateCreated {
		log.V(1).Info("Update securityGroup", "Name", s.id.Name,
			"MembershipOnly", membershipOnly, "CloudSecurityGroup", syncContent)
		_ = s.updateImpl(csg, nil, nil, membershipOnly, r)
	} else if s.state == securityGroupStateInit {
		log.V(1).Info("Add securityGroup", "Name", s.id.Name,
			"MembershipOnly", membershipOnly, "CloudSecurityGroup", syncContent)
		_ = s.addImpl(csg, membershipOnly, r)
	}
	return false
}

// sync synchronizes addressSecurityGroup with cloud.
func (a *addrSecurityGroup) sync(syncContent *cloudresource.SynchronizationContent, r *NetworkPolicyReconciler) {
	log := r.Log.WithName("CloudSync")
	if a.deletePending {
		log.V(1).Info("AddressSecurityGroup pending delete", "Name", a.id.Name)
		return
	}
	_ = a.syncImpl(a, syncContent, true, r)
}

// sync synchronizes appliedToSecurityGroup with cloud.
func (a *appliedToSecurityGroup) sync(syncContent *cloudresource.SynchronizationContent, r *NetworkPolicyReconciler) {
	log := r.Log.WithName("CloudSync")
	if a.deletePending {
		log.V(1).Info("AppliedSecurityGroup pending delete", "Name", a.id.Name)
		return
	}
	if a.syncImpl(a, syncContent, false, r) && len(a.members) > 0 {
		a.hasMembers = true
	}

	// roughly count rule items in network policies and update rules if any mismatch with syncContent.
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		log.Error(err, "get networkPolicy by indexer", "Index", networkPolicyIndexerByAppliedToGrp, "Key", a.id.Name)
		return
	}
	items := make(map[string]int)
	for _, i := range nps {
		np := i.(*networkPolicy)
		if !np.rulesReady {
			r.Log.V(1).Info("Compute rules", "networkPolicy", np.Name)
			if !np.computeRules(r) {
				log.V(1).Info("NetworkPolicy is not ready", "Name", np.Name, "Namespace", np.Namespace)
			}
		}
		for _, iRule := range np.ingressRules {
			if _, ok := iRule.AppliedToGroup[a.id.Name]; !ok {
				// Skip this rule if it's not meant for given appliedToGroup.
				continue
			}
			countIngressRuleItems(iRule, items, false)
		}
		for _, eRule := range np.egressRules {
			if _, ok := eRule.AppliedToGroup[a.id.Name]; !ok {
				// Skip this rule if it's not meant for given appliedToGroup.
				continue
			}
			countEgressRuleItems(eRule, items, false)
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
	cloudRuleMap := make(map[string]*cloudresource.CloudRule)
	for _, obj := range rules {
		rule := obj.(*cloudresource.CloudRule)
		cloudRuleMap[rule.Hash] = rule
	}

	// roughly count and compare rules in syncContent against nps.
	// also updates cloudRuleIndexer in the process.
	for _, rule := range syncContent.IngressRules {
		if rule.NpNamespacedName != "" && rule.Rule != nil {
			iRule := rule.Rule.(*cloudresource.IngressRule)
			countIngressRuleItems(iRule, items, true)
		}
		if a.checkAndUpdateIndexer(r, rule, cloudRuleMap) {
			indexerUpdate = true
		}
	}
	for _, rule := range syncContent.EgressRules {
		if rule.NpNamespacedName != "" && rule.Rule != nil {
			eRule := rule.Rule.(*cloudresource.EgressRule)
			countEgressRuleItems(eRule, items, true)
		}
		if a.checkAndUpdateIndexer(r, rule, cloudRuleMap) {
			indexerUpdate = true
		}
	}
	// remove rules no longer exist in cloud from indexer.
	for _, rule := range cloudRuleMap {
		indexerUpdate = true
		_ = r.cloudRuleIndexer.Delete(rule)
	}

	if indexerUpdate {
		_ = a.updateAddrGroupReference(r)
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
		if !a.ruleReady {
			a.markDirty(r, false)
		}
		a.ruleReady = true
	}
}

// syncWithCloud synchronizes security group in controller with cloud.
// This is a blocking call intentionally so that no other events are accepted during
// synchronization.
func (r *NetworkPolicyReconciler) syncWithCloud(forceSync bool) {
	if !forceSync && time.Now().Unix()-lastSyncTime < r.CloudSyncInterval {
		return
	}

	log := r.Log.WithName("CloudSync")
	if r.bookmarkCnt < npSyncReadyBookMarkCnt {
		return
	}
	ch := securitygroup.CloudSecurityGroup.GetSecurityGroupSyncChan()
	cloudAddrSGs := make(map[cloudresource.CloudResourceID]*cloudresource.SynchronizationContent)
	cloudAppliedToSGs := make(map[cloudresource.CloudResourceID]*cloudresource.SynchronizationContent)
	removeAddrSgs := make([]*addrSecurityGroup, 0)
	for content := range ch {
		log.V(1).Info("Sync from cloud", "SecurityGroup", content)
		if content.MembershipOnly {
			// mark unknown address groups and pending delete groups for deletion after appliedTo groups updates address group references.
			_, ok, _ := r.addrSGIndexer.GetByKey(content.Resource.CloudResourceID.String())
			if !ok {
				state := securityGroupStateCreated
				sg := newAddrSecurityGroup(&content.Resource, []*cloudresource.CloudResource{}, &state).(*addrSecurityGroup)
				sg.deletePending = true
				sg.retryEnabled = false
				removeAddrSgs = append(removeAddrSgs, sg)
				// add unknown address group in indexer for future network policy notify.
				_ = r.addrSGIndexer.Add(sg)
				continue
			}
		} else if _, ok, _ := r.appliedToSGIndexer.GetByKey(content.Resource.CloudResourceID.String()); !ok {
			// remove unknown appliedTo groups.
			log.V(0).Info("Delete appliedTo security group not found in cache", "Name", content.Resource.Name)
			state := securityGroupStateCreated
			sg := newAppliedToSecurityGroup(&content.Resource, []*cloudresource.CloudResource{}, &state).(*appliedToSecurityGroup)
			sg.retryEnabled = false
			_ = sg.delete(r)
			continue
		}
		// copy channel reference content to a local variable because we use pointer to reference to cloud sg.
		cc := content
		if content.MembershipOnly {
			cloudAddrSGs[content.Resource.CloudResourceID] = &cc
		} else {
			cloudAppliedToSGs[content.Resource.CloudResourceID] = &cc
		}
	}
	r.syncedWithCloud = true
	for _, i := range r.addrSGIndexer.List() {
		sg := i.(*addrSecurityGroup)
		sg.retryEnabled = false
		sg.sync(cloudAddrSGs[sg.getID()], r)
		sg.retryEnabled = true
	}
	for _, i := range r.appliedToSGIndexer.List() {
		sg := i.(*appliedToSecurityGroup)
		sg.retryEnabled = false
		sg.sync(cloudAppliedToSGs[sg.getID()], r)
		sg.retryEnabled = true
	}
	// remove unknown address groups after syncing appliedTo group references.
	for _, sg := range removeAddrSgs {
		log.V(0).Info("Delete address security group not found in cache", "Name", sg.id.Name)
		sg.retryEnabled = false
		_ = sg.delete(r)
	}
	lastSyncTime = time.Now().Unix()
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
	r.syncWithCloud(true)
	return true
}

// getNICsOfCloudResources returns NICs of cloud resources if available.
func (r *NetworkPolicyReconciler) getNICsOfCloudResources(resources []*cloudresource.CloudResource) (
	[]*cloudresource.CloudResource, error) {
	if len(resources) == 0 {
		return nil, nil
	}
	if resources[0].Type == cloudresource.CloudResourceTypeNIC {
		return resources, nil
	}

	nics := make([]*cloudresource.CloudResource, 0, len(resources))
	for _, rsc := range resources {
		id := rsc.Name
		vmItems, err := r.Inventory.GetVmFromIndexer(indexer.VirtualMachineByCloudId, id)
		if err != nil {
			r.Log.Error(err, "failed to get VMs from VM cache")
			return resources, err
		}

		for _, item := range vmItems {
			vm := item.(*runtimev1alpha1.VirtualMachine)
			for _, nic := range vm.Status.NetworkInterfaces {
				nics = append(nics, &cloudresource.CloudResource{Type: cloudresource.CloudResourceTypeNIC,
					CloudResourceID: cloudresource.CloudResourceID{Name: nic.Name, Vpc: rsc.Vpc}})
			}
		}
	}
	return nics, nil
}

// checkAndUpdateIndexer checks if rule is present in indexer and updates the indexer if not present.
// Returns true if indexer is updated.
func (a *appliedToSecurityGroup) checkAndUpdateIndexer(r *NetworkPolicyReconciler, rule cloudresource.CloudRule,
	existingRuleMap map[string]*cloudresource.CloudRule) bool {
	indexerUpdate := false

	// update rule if not found in indexer, otherwise remove from map to indicate a matching rule is found.
	if _, found := existingRuleMap[rule.Hash]; !found {
		indexerUpdate = true
		_ = r.cloudRuleIndexer.Update(&rule)
	} else {
		delete(existingRuleMap, rule.Hash)
	}

	// return if indexer is updated or not.
	return indexerUpdate
}

// countIngressRuleItems updates the count of corresponding items in the given map based on contents of the specified ingress rule.
func countIngressRuleItems(iRule *cloudresource.IngressRule, items map[string]int, subtract bool) {
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
		updateCountForItem(portStr, items, subtract)
	}
	for _, ip := range iRule.FromSrcIP {
		updateCountForItem(ip.String(), items, subtract)
	}
	for _, sg := range iRule.FromSecurityGroups {
		updateCountForItem(sg.String(), items, subtract)
	}
}

// countEgressRuleItems updates the count of corresponding items in the given map based on contents of the specified egress rule.
func countEgressRuleItems(eRule *cloudresource.EgressRule, items map[string]int, subtract bool) {
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
		updateCountForItem(portStr, items, subtract)
	}
	for _, ip := range eRule.ToDstIP {
		updateCountForItem(ip.String(), items, subtract)
	}
	for _, sg := range eRule.ToSecurityGroups {
		updateCountForItem(sg.String(), items, subtract)
	}
}

// updateCountForItem adds or subtracts the item count in the items map.
func updateCountForItem(item string, items map[string]int, subtract bool) {
	if subtract {
		items[item]--
	} else {
		items[item]++
	}
}
