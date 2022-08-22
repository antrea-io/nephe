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
	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sync synchronizes securityGroup memberships with cloud.
// Return true if cloud and controller has same membership.
func (s *securityGroupImpl) syncImpl(csg cloudSecurityGroup, c *securitygroup.SynchronizationContent, membershipOnly bool,
	r *NetworkPolicyReconciler) bool {
	log := r.Log.WithName("CloudSync")
	if c != nil {
		s.state = securityGroupStateCreated
		cMembers := make([]*securitygroup.CloudResource, 0, len(c.Members))
		for i := range c.Members {
			cMembers = append(cMembers, &c.Members[i])
		}

		nMembers := s.members
		if len(cMembers) > 0 && cMembers[0].Type == securitygroup.CloudResourceTypeNIC {
			nMembers, _ = r.getNICsOfCloudResources(s.members)
		}
		if compareCloudResources(nMembers, cMembers) {
			log.V(1).Info("Same SecurityGroup found", "Name", s.id, "State", s.state)
			return true
		}
	} else if len(s.members) == 0 {
		log.V(1).Info("Empty memberships", "Name", s.id)
		return true
	}

	if s.state == securityGroupStateCreated {
		log.V(1).Info("Update securityGroup", "Name", s.id, "MembershipOnly", membershipOnly, "CloudSecurityGroup", c)
		_ = s.updateImpl(csg, nil, nil, membershipOnly, r)
	} else if s.state == securityGroupStateInit {
		log.V(1).Info("Add securityGroup", "Name", s.id, "MembershipOnly", membershipOnly, "CloudSecurityGroup", c)
		_ = s.addImpl(csg, membershipOnly, r)
	}
	return false
}

// sync synchronizes addressSecurityGroup with cloud.
func (a *addrSecurityGroup) sync(c *securitygroup.SynchronizationContent,
	r *NetworkPolicyReconciler) {
	log := r.Log.WithName("CloudSync")
	if a.deletePending {
		log.V(1).Info("AddressSecurityGroup pending delete", "Name", a.id)
		return
	}
	_ = a.syncImpl(a, c, true, r)
}

// sync synchronizes appliedToSecurityGroup with cloud.
func (a *appliedToSecurityGroup) sync(c *securitygroup.SynchronizationContent,
	r *NetworkPolicyReconciler) {
	log := r.Log.WithName("CloudSync")
	if a.deletePending {
		log.V(1).Info("AppliedSecurityGroup pending delete", "Name", a.id)
		return
	}
	if a.syncImpl(a, c, false, r) && len(a.members) > 0 {
		a.hasMembers = true
	}
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		log.Error(err, "Get networkPolicy by indexer", "Index", networkPolicyIndexerByAppliedToGrp, "Key", a.id.Name)
		return
	}
	items := make(map[string]int)
	for _, i := range nps {
		np := i.(*networkPolicy)

		if !np.computeRules(r) {
			log.V(1).Info("Skip sync, networkPolicy not ready", "Name", np.Name, "Namespace", np.Namespace)
			return
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
	if c == nil {
		_ = a.updateRules(r)
		return
	}
	// Rough compare rules
	for _, iRule := range c.IngressRules {
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
	}
	for _, eRule := range c.EgressRules {
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
	}
	for k, i := range items {
		if i != 0 {
			log.V(1).Info("Update appliedToSecurityGroup rules", "Name", a.id.String(), "CloudSecurityGroup", c, "Item", k, "Diff", i)
			_ = a.updateRules(r)
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
		if _, ok, _ := indexer.GetByKey(content.Resource.String()); !ok {
			state := securityGroupStateCreated
			_ = sgNew(&content.Resource, []*securitygroup.CloudResource{}, &state).delete(r)
			continue
		}
		// copy channel reference content to a local variable because we use pointer to
		// reference to cloud sg.
		cc := content
		if content.MembershipOnly {
			cloudAddrSGs[content.Resource] = &cc
		} else {
			cloudAppliedToSGs[content.Resource] = &cc
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
	// For cloud resource with any non-antrea+ SG, tricking plug-in to remove them by explicitly
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
		name := rsc.Name.Name
		vmList := &v1alpha1.VirtualMachineList{}
		if err := r.List(context.TODO(), vmList, client.MatchingFields{virtualMachineIndexerByCloudName: name}); err != nil {
			return resources, err
		}
		for _, vm := range vmList.Items {
			for _, nic := range vm.Status.NetworkInterfaces {
				nics = append(nics, &securitygroup.CloudResource{Type: securitygroup.CloudResourceTypeNIC,
					Name: securitygroup.CloudResourceID{Name: nic.Name, Vpc: rsc.Name.Vpc}})
			}
		}
	}
	return nics, nil
}
