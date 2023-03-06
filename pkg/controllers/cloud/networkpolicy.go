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
	"net"
	"reflect"
	"strings"

	"github.com/mohae/deepcopy"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreanetcore "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	cloud "antrea.io/nephe/apis/crd/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/cloud-provider/utils"
	"antrea.io/nephe/pkg/controllers/config"
	converter "antrea.io/nephe/pkg/converter/target"
)

// InProgress indicates a securityGroup operation is in progress.
type InProgress struct{}

func (i *InProgress) String() string {
	return "in-progress"
}

func (i *InProgress) Error() string {
	return "in-progress"
}

// securityGroupOperation specified operations sent to cloud plug-in.
type securityGroupOperation int

const (
	securityGroupOperationAdd securityGroupOperation = iota
	securityGroupOperationUpdateMembers
	securityGroupOperationClearMembers
	securityGroupOperationUpdateRules
	securityGroupOperationDelete
)

func (o securityGroupOperation) String() string {
	return []string{"CREATE", "UPDATE-MEMBERS", "CLEAR-MEMBERS", "UPDATE-RULES", "DELETE"}[o]
}

// securityGroupState is the state of securityGroup.
type securityGroupState int

const (
	securityGroupStateInit securityGroupState = iota
	securityGroupStateCreated
	securityGroupStateGarbageCollectState
)

func (s securityGroupState) String() string {
	return []string{"INIT", "CREATED", "GARBAGE"}[s]
}

// securityGroupStatus returns plug-in status of a securityGroup operation.
type securityGroupStatus struct {
	sg  cloudSecurityGroup
	op  securityGroupOperation
	err error
}

// cloudSecurityGroup is the common interface for addrSecurityGroup and appliedToSecurityGroup.
type cloudSecurityGroup interface {
	add(r *NetworkPolicyReconciler) error
	delete(r *NetworkPolicyReconciler) error
	update(added, removed []*securitygroup.CloudResource, r *NetworkPolicyReconciler) error
	notify(op securityGroupOperation, status error, r *NetworkPolicyReconciler) error
	isReady() bool
	getID() securitygroup.CloudResourceID
	getMembers() []*securitygroup.CloudResource
	notifyNetworkPolicyChange(r *NetworkPolicyReconciler)
	sync(c *securitygroup.SynchronizationContent, r *NetworkPolicyReconciler)
}

var (
	_ cloudSecurityGroup = &addrSecurityGroup{}
	_ cloudSecurityGroup = &appliedToSecurityGroup{}

	AntreaProtocolMap = map[antreanetworking.Protocol]int{
		antreanetworking.ProtocolTCP:  6,
		antreanetworking.ProtocolUDP:  17,
		antreanetworking.ProtocolSCTP: 132,
	}
)

const (
	uniqueGroupNameMemberPrefix = "mm_"
)

func getGroupUniqueName(name string, memberOnly bool) string {
	if memberOnly {
		return uniqueGroupNameMemberPrefix + name
	}
	return name
}

func getGroupIDFromUniqueName(name string) (string, bool) {
	if strings.HasPrefix(name, uniqueGroupNameMemberPrefix) {
		return name[len(uniqueGroupNameMemberPrefix):], true
	}
	return name, false
}

// getNormalizedName replaces any occurrence of / with -.
func getNormalizedName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "/", "-"))
}

// diffAppliedToGrp returned added and removed groups from appliedToGroup a to b.
func diffAppliedToGrp(a, b []string) ([]string, []string) {
	temp := map[string]int{}
	for _, s := range a {
		temp[s]++
	}
	for _, s := range b {
		temp[s]--
	}
	var added, removed []string
	for s, v := range temp {
		if v > 0 {
			removed = append(removed, s)
		} else if v < 0 {
			added = append(added, s)
		}
	}
	return added, removed
}

// diffAddressGrp returns added and removed groups from rules a to b.
func diffAddressGrp(a, b []antreanetworking.NetworkPolicyRule) ([]string, []string) {
	temp := map[string]int{}
	for _, r := range a {
		for _, g := range r.From.AddressGroups {
			temp[g]++
		}
		for _, g := range r.To.AddressGroups {
			temp[g]++
		}
	}
	for _, r := range b {
		for _, g := range r.From.AddressGroups {
			temp[g]--
		}
		for _, g := range r.To.AddressGroups {
			temp[g]--
		}
	}
	var added, removed []string
	for s, v := range temp {
		if v > 0 {
			removed = append(removed, s)
		} else if v < 0 {
			added = append(added, s)
		}
	}
	return added, removed
}

// mergeCloudResources added and removed CloudResources from src.
func mergeCloudResources(src, added, removed []*securitygroup.CloudResource) (list []*securitygroup.CloudResource) {
	srcMap := make(map[string]*securitygroup.CloudResource)
	for _, s := range src {
		srcMap[s.CloudResourceID.String()] = s
	}
	for _, r := range removed {
		delete(srcMap, r.CloudResourceID.String())
	}
	for _, a := range added {
		rsc := *a
		srcMap[a.CloudResourceID.String()] = &rsc
	}
	for _, v := range srcMap {
		list = append(list, v)
	}
	return
}

// compareCloudResources returns true if content in cloudResource lists s1 and s2 are the same.
func compareCloudResources(s1, s2 []*securitygroup.CloudResource) bool {
	if len(s1) != len(s2) {
		return false
	}
	return len(mergeCloudResources(s1, nil, s2)) == 0
}

// mergeIPs added and removed IPNets from src.
func mergeIPs(src, added, removed []*net.IPNet) (list []*net.IPNet) {
	srcMap := make(map[string]*net.IPNet)
	for _, s := range src {
		srcMap[s.String()] = s
	}
	for _, r := range removed {
		delete(srcMap, r.String())
	}
	for _, a := range added {
		rsc := *a
		srcMap[a.String()] = &rsc
	}
	for _, v := range srcMap {
		list = append(list, v)
	}
	return
}

// vpcsFromGroupMembers, provided with a list of ExternalEntityReferences, returns corresponding CloudResources keyed by VPC.
// If an ExternalEntity does not correspond to a CloudResource, its IP(s) is returned.
func vpcsFromGroupMembers(members []antreanetworking.GroupMember, r *NetworkPolicyReconciler) (
	map[string][]*securitygroup.CloudResource, []*net.IPNet, []*types.NamespacedName, error) {
	vpcs := make(map[string][]*securitygroup.CloudResource)
	var ipBlocks []*net.IPNet
	var notFoundMember []*types.NamespacedName
	for _, m := range members {
		if m.ExternalEntity == nil {
			continue
		}
		e := &antreanetcore.ExternalEntity{}
		key := client.ObjectKey{Name: m.ExternalEntity.Name, Namespace: m.ExternalEntity.Namespace}
		if err := r.Get(context.TODO(), key, e); err != nil {
			if apierrors.IsNotFound(err) {
				namespacedName := &types.NamespacedName{Namespace: m.ExternalEntity.Namespace, Name: m.ExternalEntity.Name}
				notFoundMember = append(notFoundMember, namespacedName)
				continue
			}
			r.Log.Error(err, "client get ExternalEntity", "key", key)
			return nil, nil, nil, err
		}
		kind, ok := e.Labels[config.ExternalEntityLabelKeyKind]
		if !ok {
			r.Log.Error(fmt.Errorf(""), "kind label not found in ExternalEntity", "key", key, "labels", e.Labels)
			continue
		}
		var cloudRsc securitygroup.CloudResource
		readAnnotations := false
		if kind == converter.GetExternalEntityLabelKind(&cloud.VirtualMachine{}) {
			cloudRsc.Type = securitygroup.CloudResourceTypeVM
			readAnnotations = true
		} else {
			r.Log.Error(fmt.Errorf(""), "invalid cloud resource type received", "kind", kind)
		}
		if readAnnotations {
			ownerAnnotations, ownerCloudProvider, err := getOwnerProperties(e, r)
			if err != nil {
				r.Log.Error(err, "externalEntity owner not found", "key", key, "kind", kind)
				namespacedName := &types.NamespacedName{Namespace: m.ExternalEntity.Namespace, Name: m.ExternalEntity.Name}
				notFoundMember = append(notFoundMember, namespacedName)
				continue
			}
			vpc, ok := ownerAnnotations[cloudcommon.AnnotationCloudAssignedVPCIDKey]
			if !ok {
				r.Log.Error(fmt.Errorf(""), "vpc annotation not found in ExternalEntity owner", "key", key, "kind", kind)
				continue
			}
			cloudRsc.Vpc = vpc
			vpcs[vpc] = append(vpcs[vpc], &cloudRsc)
			cloudAssignedID, ok := ownerAnnotations[cloudcommon.AnnotationCloudAssignedIDKey]
			if !ok {
				r.Log.Error(fmt.Errorf(""), "cloud assigned ID annotation not found in ExternalEntity owner", "key", key, "kind", kind)
				continue
			}
			cloudRsc.Name = cloudAssignedID
			cloudAccountID, ok := ownerAnnotations[cloudcommon.AnnotationCloudAccountIDKey]
			if !ok {
				r.Log.Error(fmt.Errorf(""), "cloud account ID annotation not found in ExternalEntity owner", "key", key, "kind", kind)
				continue
			}
			cloudRsc.AccountID = cloudAccountID
			cloudRsc.CloudProvider = ownerCloudProvider
		} else {
			for _, ep := range e.Spec.Endpoints {
				var ipnet *net.IPNet
				if _, ipnet, _ = net.ParseCIDR(ep.IP); ipnet == nil {
					_, ipnet, _ = net.ParseCIDR(ep.IP + "/32")
				}
				ipBlocks = append(ipBlocks, ipnet)
			}
		}
	}
	return vpcs, ipBlocks, notFoundMember, nil
}

// getOwnerProperties gets VM object from etcd and returns annotations and cloud provider type from it.
func getOwnerProperties(e *antreanetcore.ExternalEntity, r *NetworkPolicyReconciler) (map[string]string, string, error) {
	if len(e.OwnerReferences) == 0 {
		return nil, "", fmt.Errorf("externalEntiry owner not found (%v/%v)", e.Namespace, e.Name)
	}
	namespace := e.Namespace
	owner := e.OwnerReferences[0]
	key := client.ObjectKey{Name: owner.Name, Namespace: namespace}
	if owner.Kind == cloudcommon.VirtualMachineCRDKind {
		vm := &cloud.VirtualMachine{}
		if err := r.Get(context.TODO(), key, vm); err != nil {
			r.Log.Error(err, "client get VirtualMachine", "key", key)
			return nil, "", err
		}
		return vm.Annotations, string(vm.Status.Provider), nil
	}
	return nil, "", fmt.Errorf("unsupported cloud owner kind")
}

// securityGroupImpl supplies common implementations for addrSecurityGroup and appliedToSecurityGroup.
type securityGroupImpl struct {
	// Members of this SecurityGroup.
	members []*securitygroup.CloudResource
	// SecurityGroup identifier.
	id securitygroup.CloudResource
	// Current state of this SecurityGroup.
	state securityGroupState
	// To be deleted.
	deletePending bool
	// status of last operation.
	status error
	// current retried operation.
	retryOp *securityGroupOperation
	// true if retry operation is ongoing.
	retryInProgress bool
}

// getID returns securityGroup ID.
func (s *securityGroupImpl) getID() securitygroup.CloudResourceID {
	return s.id.CloudResourceID
}

// getMembers returns securityGroup members.
func (s *securityGroupImpl) getMembers() []*securitygroup.CloudResource {
	return s.members
}

// addImpl invokes cloud plug-in to create a SecurityGroup.
func (s *securityGroupImpl) addImpl(c cloudSecurityGroup, membershipOnly bool, r *NetworkPolicyReconciler) error {
	var indexer cache.Indexer
	if membershipOnly {
		indexer = r.addrSGIndexer
	} else {
		indexer = r.appliedToSGIndexer
	}
	if err := indexer.Add(c); err != nil {
		return err
	}
	if !r.syncedWithCloud {
		return nil
	}
	if s.retryOp != nil {
		return nil
	}
	r.Log.V(1).Info("Creating SecurityGroup", "Name", s.id.Name, "MembershipOnly", membershipOnly)
	ch := securitygroup.CloudSecurityGroup.CreateSecurityGroup(&s.id, membershipOnly)
	s.status = &InProgress{}
	go func() {
		err := <-ch
		r.cloudResponse <- &securityGroupStatus{sg: c, op: securityGroupOperationAdd, err: err}
	}()
	return nil
}

// deleteImpl invokes cloud plug-in to delete a SecurityGroup.
func (s *securityGroupImpl) deleteImpl(c cloudSecurityGroup, membershipOnly bool, r *NetworkPolicyReconciler) error {
	var indexKey string
	var indexer cache.Indexer
	if membershipOnly {
		indexKey = networkPolicyIndexerByAddrGrp
		indexer = r.addrSGIndexer
	} else {
		indexKey = networkPolicyIndexerByAppliedToGrp
		indexer = r.appliedToSGIndexer
	}
	uName := getGroupUniqueName(s.id.CloudResourceID.String(), membershipOnly)
	guName := getGroupUniqueName(s.id.Name, membershipOnly)
	if !r.pendingDeleteGroups.Has(guName) {
		r.pendingDeleteGroups.Add(guName, &pendingGroup{refCnt: new(int)})
	}
	if s.state != securityGroupStateGarbageCollectState {
		nps, err := r.networkPolicyIndexer.ByIndex(indexKey, s.id.Name)
		if err != nil {
			return fmt.Errorf("get networkpolicy indexer with index=%v, name=%v: %w", indexKey, s.id.Name, err)
		}
		if len(nps) != 0 {
			r.Log.V(1).Info("Deleting SecurityGroup pending, in use by networkpolicies", "Name", s.id.Name,
				"MembershipOnly", membershipOnly, "anpNum", len(nps))
			s.deletePending = true
			return nil
		}
		if membershipOnly {
			refs, err := r.appliedToSGIndexer.ByIndex(appliedToIndexerByAddrGroupRef, s.id.CloudResourceID.String())
			if err != nil {
				return fmt.Errorf("get appliedTo indexer with name=%v: %w", s.id.CloudResourceID.String(), err)
			}
			if len(refs) != 0 {
				r.Log.V(1).Info("Deleting SecurityGroup pending, referenced by appliedTo groups", "Name", s.id.Name,
					"MembershipOnly", membershipOnly, "refNum", len(refs))
				s.deletePending = true
				return nil
			}
		}
		r.Log.V(1).Info("Deleting SecurityGroup", "Name", s.id.Name, "MembershipOnly", membershipOnly)
		if err := indexer.Delete(c); err != nil {
			r.Log.Error(err, "deleting SecurityGroup from indexer", "Name", s.id.Name)
		}
		// delete operation supersedes any prior retry operations.
		r.retryQueue.Remove(uName)
		s.retryOp = nil
		if s.state == securityGroupStateInit {
			// Security group has not been or in the process of being created by cloud.
			if _, ok := s.status.(*InProgress); !ok {
				return nil
			}
		}
		s.state = securityGroupStateGarbageCollectState
	}
	if s.retryOp != nil {
		return nil
	}
	_ = r.pendingDeleteGroups.Update(guName, false, 1, false)
	s.status = &InProgress{}
	ch := securitygroup.CloudSecurityGroup.DeleteSecurityGroup(&s.id, membershipOnly)
	go func() {
		err := <-ch
		if err == nil && !membershipOnly {
			s.deleteSgRulesFromIndexer(r)
		}
		r.cloudResponse <- &securityGroupStatus{sg: c, op: securityGroupOperationDelete, err: err}
	}()
	return nil
}

// updateImpl invokes cloud plug-in to update a SecurityGroup's membership.
func (s *securityGroupImpl) updateImpl(c cloudSecurityGroup, added, removed []*securitygroup.CloudResource,
	membershipOnly bool, r *NetworkPolicyReconciler) error {
	if len(added)+len(removed)+len(s.members) == 0 {
		// Membership is empty with no additional changes, do nothing.
		return nil
	}
	s.members = mergeCloudResources(s.members, added, removed)
	if s.state != securityGroupStateCreated {
		return nil
	}
	members := deepcopy.Copy(s.members).([]*securitygroup.CloudResource)
	// Wait for ongoing retry operation.
	if s.retryOp != nil {
		return nil
	}
	r.Log.V(1).Info("Updating SecurityGroup members", "Name", s.id.Name, "MembershipOnly", membershipOnly,
		"members", members)
	ch := securitygroup.CloudSecurityGroup.UpdateSecurityGroupMembers(&s.id, members, membershipOnly)
	go func() {
		err := <-ch
		if len(s.members) == 0 {
			r.cloudResponse <- &securityGroupStatus{sg: c, op: securityGroupOperationClearMembers, err: err}
		} else {
			r.cloudResponse <- &securityGroupStatus{sg: c, op: securityGroupOperationUpdateMembers, err: err}
		}
	}()
	return nil
}

// notifyImpl handles operation retry and group deletion logic.
func (s *securityGroupImpl) notifyImpl(c PendingItem, membershipOnly bool, op securityGroupOperation, status error,
	r *NetworkPolicyReconciler) {
	moreOps := false
	uName := getGroupUniqueName(s.id.CloudResourceID.String(), membershipOnly)
	if status != nil && !r.retryQueue.Has(uName) &&
		(s.state != securityGroupStateGarbageCollectState || op == securityGroupOperationDelete) {
		// ignore prior non-delete failure during delete
		s.retryOp = &op
		r.retryQueue.Add(uName, c)
	}
	if r.retryQueue.Has(uName) {
		_ = r.retryQueue.Update(uName, status == nil, op)
		if r.retryQueue.Has(uName) && r.retryQueue.GetRetryCount(uName) > 0 {
			moreOps = true
		}
	}
	if op == securityGroupOperationDelete {
		_ = r.pendingDeleteGroups.Update(getGroupUniqueName(s.id.Name, membershipOnly), true, -1, !moreOps)
	}
}

// deleteSgRulesFromIndexer deletes all rules that are part of the security group from the cloudRule indexer.
func (s *securityGroupImpl) deleteSgRulesFromIndexer(r *NetworkPolicyReconciler) {
	rules, err := r.cloudRuleIndexer.ByIndex(cloudRuleIndexerByAppliedToGrp, s.id.CloudResourceID.String())
	if err != nil {
		r.Log.Error(err, "get cloudRule indexer", "index", s.id.CloudResourceID.String())
		return
	}
	for _, rule := range rules {
		_ = r.cloudRuleIndexer.Delete(rule)
	}
}

func (s *securityGroupImpl) markDirty(r *NetworkPolicyReconciler, create bool) {
	for _, rsc := range s.members {
		if tracker := r.getCloudResourceNPTracker(rsc, create); tracker != nil {
			tracker.markDirty()
		}
	}
}

// addrSecurityGroup keeps track of membership within a SecurityGroup in a VPC.
type addrSecurityGroup struct {
	securityGroupImpl
	// IPs presents IPs of these non-cloud ExternalEntities associated with this AddressGroup.
	ipBlocks []*net.IPNet
}

// newAddrSecurityGroup creates a new addSecurityGroup from Antrea AddressGroup.
func newAddrSecurityGroup(id *securitygroup.CloudResource, data interface{}, state *securityGroupState) cloudSecurityGroup {
	sg := &addrSecurityGroup{}
	if ips, ok := data.([]*net.IPNet); ok {
		sg.ipBlocks = ips
	} else {
		sg.members = data.([]*securitygroup.CloudResource)
	}
	if state != nil {
		sg.state = *state
	} else {
		sg.state = securityGroupStateInit
	}
	sg.id = *id
	return sg
}

// add invokes cloud plug-in to create an addrSecurityGroup.
func (a *addrSecurityGroup) add(r *NetworkPolicyReconciler) error {
	if a.isIPBlocks() {
		return r.addrSGIndexer.Add(a)
	}
	return a.addImpl(a, true, r)
}

// delete invokes cloud plug-in to delete an addrSecurityGroup.
func (a *addrSecurityGroup) delete(r *NetworkPolicyReconciler) error {
	if a.isIPBlocks() {
		return r.addrSGIndexer.Delete(a)
	}
	return a.deleteImpl(a, true, r)
}

// updateIPs updates IPs stored in an addrSecurityGroup. It does not trigger operations to cloud plug-in.
func (a *addrSecurityGroup) updateIPs(added, removed []*net.IPNet, r *NetworkPolicyReconciler) {
	r.Log.V(1).Info("AddrSecurityGroup UpdateIPs", "Name", a.id.Name)
	a.ipBlocks = mergeIPs(a.ipBlocks, added, removed)
}

// update invokes cloud plug-in to update an addrSecurityGroup.
func (a *addrSecurityGroup) update(added, removed []*securitygroup.CloudResource, r *NetworkPolicyReconciler) error {
	if a.isIPBlocks() {
		return nil
	}
	return a.updateImpl(a, added, removed, true, r)
}

// isReady returns true if cloud plug-in has created addrSecurityGroup.
func (a *addrSecurityGroup) isReady() bool {
	return a.isIPBlocks() || a.state == securityGroupStateCreated
}

// isIPBlocks returns true if this addrSecurityGroup is used for storing IPBlocks.
func (a *addrSecurityGroup) isIPBlocks() bool {
	return len(a.id.Vpc) == 0
}

// notify calls into addrSecurityGroup to report operation status from cloud plug-in.
func (a *addrSecurityGroup) notify(op securityGroupOperation, status error, r *NetworkPolicyReconciler) error {
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAddrGrp, a.id.Name)
	if err != nil {
		r.Log.Error(err, "get networkPolicy with key %s from indexer: %w", a.id.Name, err)
	}

	defer func() {
		if a.isReady() {
			for _, i := range nps {
				np := i.(*networkPolicy)
				np.markDirty(r)
			}
		}
		a.notifyImpl(a, true, op, status, r)
	}()

	if !(a.state == securityGroupStateGarbageCollectState && op != securityGroupOperationDelete) {
		a.status = status
	}
	if status != nil {
		r.Log.Error(status, "addrSecurityGroup operation failed", "Name", a.id.Name, "Op", op)
		return nil
	}

	r.Log.V(1).Info("AddrSecurityGroup received operation ok", "Name", a.id.Name, "state", a.state, "status", a.status, "Op", op)
	uName := getGroupUniqueName(a.id.CloudResourceID.String(), true)
	if r.retryQueue.Has(uName) {
		_ = r.retryQueue.Update(uName, false, op)
	}
	switch op {
	case securityGroupOperationAdd:
		if a.state == securityGroupStateInit {
			a.state = securityGroupStateCreated
		}
	default:
		return nil
	}

	r.Log.V(1).Info("AddrSecurityGroup becomes ready", "Name", a.id.Name)
	// update addrSecurityGroup members.
	if err := a.update(nil, nil, r); err != nil {
		return err
	}
	// Update networkPolicies.
	for _, i := range nps {
		np := i.(*networkPolicy)
		if err := np.notifyAddrGrpChanges(r); err != nil {
			r.Log.Error(err, "networkPolicy", "name", np.Name)
		}
	}
	return nil
}

// getStatus returns status of this addrSecurityGroup.
func (a *addrSecurityGroup) getStatus() error {
	if a.status != nil {
		return a.status
	}
	if a.state == securityGroupStateCreated || a.isIPBlocks() {
		return nil
	}
	return &InProgress{}
}

// getIPs returns IPs in an addrSecurityGroup.
func (a *addrSecurityGroup) getIPs() []*net.IPNet {
	return a.ipBlocks
}

// notifyNetworkPolicyChange notifies some NetworkPolicy reference to this securityGroup has changed.
func (a *addrSecurityGroup) notifyNetworkPolicyChange(r *NetworkPolicyReconciler) {
	if !a.deletePending {
		return
	}

	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAddrGrp, a.id.Name)

	if err != nil {
		r.Log.Error(err, "get networkPolicy indexer", a.id.Name, err, "indexKey", networkPolicyIndexerByAddrGrp)
	}
	r.Log.V(1).Info("AddrSecurityGroup notifyNetworkPolicyChange", "Name",
		a.id.String(), "anpNum", len(nps))
	if len(nps) == 0 {
		if err := a.delete(r); err != nil {
			r.Log.Error(err, "delete securityGroup", "Name", a.id.Name)
		}
	}
}

// removeStaleMembers removes sg members that their corresponding CRs no longer exist.
func (a *addrSecurityGroup) removeStaleMembers(stales []*types.NamespacedName, r *NetworkPolicyReconciler) {
	if len(a.members) == 0 {
		return
	}
	srcMap := make(map[string]*securitygroup.CloudResource)
	for _, m := range a.members {
		srcMap[m.Name] = m
	}
	for _, stale := range stales {
		for k := range srcMap {
			if strings.Contains(stale.Name, k) {
				r.Log.V(1).Info("Remove stale members from SecurityGroup", "Stale", stale, "Name", a.id.Name)
				delete(srcMap, k)
			}
		}
	}
	var members []*securitygroup.CloudResource
	for _, m := range srcMap {
		members = append(members, m)
	}
	a.members = members
}

// appliedToSecurityGroup contains information to create a cloud appliedToSecurityGroup.
type appliedToSecurityGroup struct {
	securityGroupImpl
	ruleReady     bool
	hasMembers    bool
	addrGroupRefs map[string]bool
}

// newAddrAppliedGroup creates a new addSecurityGroup from Antrea AddressGroup membership.
func newAppliedToSecurityGroup(id *securitygroup.CloudResource, data interface{}, state *securityGroupState) cloudSecurityGroup {
	members, ok := data.([]*securitygroup.CloudResource)
	if !ok {
		return nil
	}
	sg := &appliedToSecurityGroup{}
	sg.members = members
	sg.id = *id
	if state != nil {
		sg.state = *state
	} else {
		sg.state = securityGroupStateInit
	}
	return sg
}

// add invokes cloud plug-in to create an appliedToSecurityGroup.
func (a *appliedToSecurityGroup) add(r *NetworkPolicyReconciler) error {
	for _, rsc := range a.members {
		if tracker := r.getCloudResourceNPTracker(rsc, true); tracker != nil {
			_ = tracker.update(a, false, r)
		}
	}
	return a.addImpl(a, false, r)
}

// delete invokes cloud plug-in to delete an appliedToSecurityGroup.
func (a *appliedToSecurityGroup) delete(r *NetworkPolicyReconciler) error {
	if a.hasMembers {
		for _, rsc := range a.members {
			if tracker := r.getCloudResourceNPTracker(rsc, false); tracker != nil {
				_ = tracker.update(a, true, r)
			}
		}
	}
	return a.deleteImpl(a, false, r)
}

// isReady returns true if cloud plug-in has created appliedToSecurityGroup.
func (a *appliedToSecurityGroup) isReady() bool {
	return a.state == securityGroupStateCreated
}

// updateAllRules invokes cloud plug-in to update rules of appliedToSecurityGroup for all associated ANPs.
func (a *appliedToSecurityGroup) updateAllRules(r *NetworkPolicyReconciler) error {
	if !a.isReady() {
		return nil
	}

	// Wait for ongoing retry operation.
	if a.retryOp != nil {
		return nil
	}
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		return fmt.Errorf("unable to get networkPolicy with key %s from indexer: %w", a.id.Name, err)
	}
	if len(nps) == 0 {
		a.clearMembers(r)
		return nil
	}

	for _, obj := range nps {
		np := obj.(*networkPolicy)
		a.updateANPRules(r, np)
	}

	return nil
}

// updateANPRules invokes cloud plug-in to update rules of appliedToSecurityGroup for a given ANP.
func (a *appliedToSecurityGroup) updateANPRules(r *NetworkPolicyReconciler, np *networkPolicy) {
	// skip the rule update if:
	// - the security group is not created in the cloud;
	// - the security group is pending deletion;
	// - the specified network policy has not finished computing rules;
	// - there is a pending retry operation.
	if !a.isReady() || a.deletePending || !np.rulesReady || a.retryOp != nil {
		return
	}

	npNamespacedName := np.getNamespacedName()
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		r.Log.Error(err, "get networkPolicy indexer", "sg", a.id.Name)
		err = fmt.Errorf("internal error when updating rules for sg %s anp %s", a.id.Name, npNamespacedName)
		r.sendRuleRealizationStatus(&np.NetworkPolicy, err)
		a.status = err
		_ = a.updateNPTracker(r)
		return
	}
	if len(nps) == 0 {
		a.clearMembers(r)
		return
	}
	// get full set of current rules for this security group.
	allRules := a.combineRules(nps)

	// get current rules for given np to compute rule update delta.
	rules := a.combineRules([]interface{}{np})
	currentRuleMap := make(map[string]*securitygroup.CloudRule)
	for _, rule := range rules {
		currentRuleMap[rule.Hash] = rule
	}

	a.claimUnownedRules(r, currentRuleMap, npNamespacedName)
	addRules, rmRules, err := a.computeRules(r, currentRuleMap, npNamespacedName)
	if err != nil {
		r.sendRuleRealizationStatus(&np.NetworkPolicy, err)
		a.status = err
		_ = a.updateNPTracker(r)
		return
	}

	if len(addRules) == 0 && len(rmRules) == 0 {
		go func() {
			r.updateRuleRealizationStatus(a.id.CloudResourceID.String(), np, nil)
			r.cloudResponse <- &securityGroupStatus{sg: a, op: securityGroupOperationUpdateRules, err: nil}
		}()
		return
	}

	r.Log.V(1).Info("Updating AppliedToSecurityGroup rules for anp", "anp", np.Name, "name", a.id.Name,
		"rules", rules)
	ch := securitygroup.CloudSecurityGroup.UpdateSecurityGroupRules(&a.id, addRules, rmRules, allRules)

	go func() {
		err = <-ch
		if err == nil {
			for _, rule := range addRules {
				_ = r.cloudRuleIndexer.Update(rule)
			}
			for _, rule := range rmRules {
				_ = r.cloudRuleIndexer.Delete(rule)
			}
		}
		r.updateRuleRealizationStatus(a.id.CloudResourceID.String(), np, err)
		r.cloudResponse <- &securityGroupStatus{sg: a, op: securityGroupOperationUpdateRules, err: err}
	}()
}

// clearMembers removes all members from a security group.
func (a *appliedToSecurityGroup) clearMembers(r *NetworkPolicyReconciler) {
	if a.hasMembers {
		r.Log.V(1).Info("Clearing AppliedToSecurityGroup members with no rules", "Name", a.id.Name)
		ch := securitygroup.CloudSecurityGroup.UpdateSecurityGroupMembers(&a.id, nil, false)
		go func() {
			err := <-ch
			r.cloudResponse <- &securityGroupStatus{sg: a, op: securityGroupOperationClearMembers, err: err}
		}()
		return
	}
	// No need to update appliedToSecurityGroup with no members.
	a.ruleReady = false
}

// combineRules converts and combines all rules from given anps to securitygroup.CloudRule.
func (a *appliedToSecurityGroup) combineRules(nps []interface{}) []*securitygroup.CloudRule {
	rules := make([]*securitygroup.CloudRule, 0)
	for _, i := range nps {
		np := i.(*networkPolicy)
		if !np.rulesReady {
			continue
		}
		npNamespacedName := np.getNamespacedName()
		for _, r := range np.ingressRules {
			rule := &securitygroup.CloudRule{
				Rule:          deepcopy.Copy(r).(*securitygroup.IngressRule),
				NetworkPolicy: npNamespacedName,
				AppliedToGrp:  a.id.CloudResourceID.String(),
			}
			rule.Hash = rule.GetHash()
			rules = append(rules, rule)
		}
		for _, r := range np.egressRules {
			rule := &securitygroup.CloudRule{
				Rule:          deepcopy.Copy(r).(*securitygroup.EgressRule),
				NetworkPolicy: npNamespacedName,
				AppliedToGrp:  a.id.CloudResourceID.String(),
			}
			rule.Hash = rule.GetHash()
			rules = append(rules, rule)
		}
	}
	return rules
}

// claimUnownedRules claims unowned rules in cloud rule indexer that matches with given np rules.
func (a *appliedToSecurityGroup) claimUnownedRules(r *NetworkPolicyReconciler, currentRuleMap map[string]*securitygroup.CloudRule,
	npNamespacedName string) {
	previousRules, err := r.cloudRuleIndexer.ByIndex(cloudRuleIndexerByAppliedToGrp, a.id.CloudResourceID.String())
	if err != nil {
		r.Log.Error(err, "get cloudRule indexer", "sg", a.id.CloudResourceID.String())
		return
	}

	for _, obj := range previousRules {
		previousRule := obj.(*securitygroup.CloudRule)

		// rules that are synced from the cloud do not have np associated with them.
		// here we claim any rules with no np associated that also match current np rules, since those rules are already in cloud.
		_, sameRule := currentRuleMap[previousRule.Hash]
		if sameRule && previousRule.NetworkPolicy == "" {
			r.Log.V(1).Info("Claim unowned rule", "np", npNamespacedName, "rule", previousRule)
			previousRule.NetworkPolicy = npNamespacedName
			_ = r.cloudRuleIndexer.Update(previousRule)
		}
	}
}

// computeRules computes the rule update delta of an ANP by comparing current rules in np and previous rules in indexer.
func (a *appliedToSecurityGroup) computeRules(r *NetworkPolicyReconciler, currentRuleMap map[string]*securitygroup.CloudRule,
	npNamespacedName string) ([]*securitygroup.CloudRule, []*securitygroup.CloudRule, error) {
	previousRules, err := r.cloudRuleIndexer.ByIndex(cloudRuleIndexerByAppliedToGrp, a.id.CloudResourceID.String())
	if err != nil {
		r.Log.Error(err, "get cloudRule indexer", "sg", a.id.CloudResourceID.String())
		return nil, nil, err
	}

	// for each previous rule:
	// same rule with same np found in current rules  -> rule already applied, no-op.
	// no rule with same np found                     -> rule removed, delete.
	// same rule with different np found              -> duplicate rules with other np, err.
	// no rule with different np found                -> no-op.
	addRules := make([]*securitygroup.CloudRule, 0)
	removeRules := make([]*securitygroup.CloudRule, 0)
	for _, obj := range previousRules {
		previousRule := obj.(*securitygroup.CloudRule)
		// build previous rule map as we go.

		sameNP := previousRule.NetworkPolicy == npNamespacedName
		currentRule, sameRule := currentRuleMap[previousRule.Hash]
		if sameRule && !sameNP {
			err = fmt.Errorf("duplicate rules with anp %s", previousRule.NetworkPolicy)
			r.Log.Error(err, "unable to compute rules", "rule", currentRule, "anp", npNamespacedName)
			return nil, nil, err
		}
		if !sameRule && sameNP {
			removeRules = append(removeRules, previousRule)
			continue
		}
		if sameRule && sameNP {
			delete(currentRuleMap, previousRule.Hash)
		}
	}

	// add rules that are not in previous rules.
	for _, rule := range currentRuleMap {
		addRules = append(addRules, rule)
	}

	return addRules, removeRules, nil
}

// checkRealization checks for exact match between desired rule state in given np and realized rule state in cloudRuleIndexer.
func (a *appliedToSecurityGroup) checkRealization(r *NetworkPolicyReconciler, np *networkPolicy) error {
	realizedRules, err := r.cloudRuleIndexer.ByIndex(cloudRuleIndexerByAppliedToGrp, a.id.CloudResourceID.String())
	if err != nil {
		return err
	}

	realizedRuleMap := make(map[string]*securitygroup.CloudRule)
	for _, obj := range realizedRules {
		rule := obj.(*securitygroup.CloudRule)
		// sg might have rules from other nps, ignore those rules.
		if rule.NetworkPolicy != np.getNamespacedName() {
			continue
		}
		realizedRuleMap[rule.Hash] = rule
	}

	for _, irule := range np.ingressRules {
		desiredRule := securitygroup.CloudRule{
			Rule:         irule,
			AppliedToGrp: a.id.CloudResourceID.String(),
		}
		desiredRule.Hash = desiredRule.GetHash()
		_, found := realizedRuleMap[desiredRule.Hash]
		if !found {
			return fmt.Errorf("ingress rule not realized %+v", desiredRule)
		}
		delete(realizedRuleMap, desiredRule.Hash)
	}
	for _, erule := range np.egressRules {
		desiredRule := securitygroup.CloudRule{
			Rule:         erule,
			AppliedToGrp: a.id.CloudResourceID.String(),
		}
		desiredRule.Hash = desiredRule.GetHash()
		_, found := realizedRuleMap[desiredRule.Hash]
		if !found {
			return fmt.Errorf("egress rule not realized %+v", desiredRule)
		}
		delete(realizedRuleMap, desiredRule.Hash)
	}

	if len(realizedRuleMap) != 0 {
		return fmt.Errorf("unexpected rules in cloud %+v", realizedRuleMap)
	}
	return nil
}

// updateAddrGroupReference updates appliedTo group addrGroupRefs and notifies removed addrGroups that rules referencing them is removed.
func (a *appliedToSecurityGroup) updateAddrGroupReference(r *NetworkPolicyReconciler) error {
	// get latest irules and erules
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		return fmt.Errorf("unable to get networkPolicy with key %s from indexer: %w", a.id.Name, err)
	}
	rules := a.combineRules(nps)

	// combine rules to get latest addrGroupRefs.
	currentRefs := make(map[string]bool)
	for _, rule := range rules {
		switch rule.Rule.(type) {
		case *securitygroup.IngressRule:
			for _, sg := range rule.Rule.(*securitygroup.IngressRule).FromSecurityGroups {
				currentRefs[sg.String()] = true
			}
		case *securitygroup.EgressRule:
			for _, sg := range rule.Rule.(*securitygroup.EgressRule).ToSecurityGroups {
				currentRefs[sg.String()] = true
			}
		}
	}

	// compute addrGroupRefs removed from previous.
	removedRefs := make([]string, 0)
	for oldRef := range a.addrGroupRefs {
		if _, found := currentRefs[oldRef]; !found {
			removedRefs = append(removedRefs, oldRef)
		}
	}
	// update addrGroupRefs.
	// Indexer does not work with in-place update. Do delete->update->add.
	if err = r.appliedToSGIndexer.Delete(a); err != nil {
		r.Log.Error(err, "delete appliedToSG indexer", "Name", a.id.String())
		return err
	}
	a.addrGroupRefs = currentRefs
	if err = r.appliedToSGIndexer.Add(a); err != nil {
		r.Log.Error(err, "add appliedToSG indexer", "Name", a.id.String())
		return err
	}
	// notify addrSG that references removed.
	return a.notifyAddrGroups(removedRefs, r)
}

// notifyAddrGroups notifies referenced addrGroups that the reference has changed.
func (a *appliedToSecurityGroup) notifyAddrGroups(addrGroups []string, r *NetworkPolicyReconciler) error {
	for _, ref := range addrGroups {
		obj, exist, err := r.addrSGIndexer.GetByKey(ref)
		if err != nil {
			r.Log.Error(err, "get addrSG indexer", "Name", ref)
			return err
		}
		if exist {
			sg := obj.(*addrSecurityGroup)
			sg.notifyNetworkPolicyChange(r)
		}
	}
	return nil
}

// update invokes cloud plug-in to update appliedToSecurityGroup's membership.
func (a *appliedToSecurityGroup) update(added, removed []*securitygroup.CloudResource, r *NetworkPolicyReconciler) error {
	for _, rsc := range removed {
		if tracker := r.getCloudResourceNPTracker(rsc, false); tracker != nil {
			_ = tracker.update(a, true, r)
		}
	}
	for _, rsc := range added {
		if tracker := r.getCloudResourceNPTracker(rsc, true); tracker != nil {
			_ = tracker.update(a, false, r)
		}
	}
	return a.updateImpl(a, added, removed, false, r)
}

// getStatus returns status of this appliedToSecurityGroup.
func (a *appliedToSecurityGroup) getStatus() error {
	if a.status != nil {
		return a.status
	}
	if a.state == securityGroupStateCreated && a.ruleReady {
		return nil
	}
	return &InProgress{}
}

func (a *appliedToSecurityGroup) updateNPTracker(r *NetworkPolicyReconciler) error {
	trackers, err := r.cloudResourceNPTrackerIndexer.ByIndex(cloudResourceNPTrackerIndexerByAppliedToGrp,
		a.id.CloudResourceID.String())
	if err != nil {
		r.Log.Error(err, "get cloud resource tracker indexer", "Key", a.id.Name)
		return err
	}
	for _, i := range trackers {
		tracker := i.(*cloudResourceNPTracker)
		tracker.markDirty()
	}
	return nil
}

// notify calls into appliedToSecurityGroup to report operation status from cloud plug-in.
func (a *appliedToSecurityGroup) notify(op securityGroupOperation, status error, r *NetworkPolicyReconciler) error {
	defer func() {
		if err := a.updateNPTracker(r); err != nil {
			return
		}
		a.notifyImpl(a, false, op, status, r)
	}()

	if !(a.state == securityGroupStateGarbageCollectState && op != securityGroupOperationDelete) {
		a.status = status
	}
	if status != nil {
		r.Log.Error(status, "appliedToSecurityGroup operation failed", "Name", a.id.Name, "Op", op)
		return nil
	}
	r.Log.V(1).Info("AppliedToSecurityGroup received operation ok", "Name", a.id.Name, "state",
		a.state, "status", a.status, "Op", op)
	uName := getGroupUniqueName(a.id.CloudResourceID.String(), false)
	if r.retryQueue.Has(uName) {
		_ = r.retryQueue.Update(uName, false, op)
	}
	switch op {
	case securityGroupOperationAdd:
		// AppliedToSecurityGroup becomes ready. apply any rules.
		if a.state == securityGroupStateInit {
			a.state = securityGroupStateCreated
			return a.updateAllRules(r)
		}
	case securityGroupOperationUpdateMembers:
		a.hasMembers = true
	case securityGroupOperationUpdateRules:
		// AppliedToSecurityGroup added rules, now update addrGroup references and add members.
		if err := a.updateAddrGroupReference(r); err != nil {
			return err
		}
		a.ruleReady = true
		if !a.hasMembers {
			return a.update(nil, nil, r)
		}
	case securityGroupOperationClearMembers:
		// AppliedToSecurityGroup has cleared members, clear rules in cloud and indexer.
		a.hasMembers = false
		if a.ruleReady {
			return a.updateAllRules(r)
		}
	case securityGroupOperationDelete:
		// AppliedToSecurityGroup is deleted, notify all referenced addrGroups.
		ref := make([]string, 0)
		for sg := range a.addrGroupRefs {
			ref = append(ref, sg)
		}
		return a.notifyAddrGroups(ref, r)
	}
	return nil
}

// notifyNetworkPolicyChange notifies some NetworkPolicy reference to this securityGroup has changed.
func (a *appliedToSecurityGroup) notifyNetworkPolicyChange(r *NetworkPolicyReconciler) {
	nps, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		r.Log.Error(err, "get networkPolicy indexer", a.id.Name, err, "indexKey", networkPolicyIndexerByAppliedToGrp)
	}
	r.Log.V(1).Info("AppliedToSecurityGroup notifyNetworkPolicyChange", "Name",
		a.id.CloudResourceID.String(), "anpNum", len(nps))
	if len(nps) == 0 && a.deletePending {
		if err := a.delete(r); err != nil {
			r.Log.Error(err, "delete securityGroup", "Name", a.id.Name)
		}
	}
}

// removeStaleMembers removes sg members that their corresponding CRs no longer exist and cleans up relevant internal resources.
// No cloud api calls will be made to update members because VM may be terminated in cloud.
func (a *appliedToSecurityGroup) removeStaleMembers(stales []*types.NamespacedName, r *NetworkPolicyReconciler) {
	if len(a.members) == 0 {
		return
	}
	srcMap := make(map[string]*securitygroup.CloudResource)
	for _, m := range a.members {
		name := utils.GetCloudResourceCRName(m.CloudProvider, m.Name)
		srcMap[name] = m
	}
	for _, stale := range stales {
		for name := range srcMap {
			if strings.Contains(stale.Name, name) {
				// remove member np tracker.
				r.Log.V(1).Info("Remove stale members from SecurityGroup", "Stale", stale, "Name", a.id.Name)
				if tracker := r.getCloudResourceNPTracker(srcMap[name], false); tracker != nil {
					_ = tracker.update(a, true, r)
				}
				// remove member vmp.
				vmNamespacedName := types.NamespacedName{Name: name, Namespace: stale.Namespace}
				if obj, found, _ := r.virtualMachinePolicyIndexer.GetByKey(vmNamespacedName.String()); found {
					r.Log.V(1).Info("Delete vmp status", "resource", vmNamespacedName.String())
					_ = r.virtualMachinePolicyIndexer.Delete(obj)
				}
				// remove member from sg.
				delete(srcMap, name)
			}
		}
	}
	var members []*securitygroup.CloudResource
	for _, m := range srcMap {
		members = append(members, m)
	}
	a.members = members
}

// networkPolicyRule describe an Antrea networkPolicy rule.
type networkPolicyRule struct {
	rule *antreanetworking.NetworkPolicyRule
}

// rules generate cloud plug-in ingressRule and/or egressRule from an networkPolicyRule.
func (r *networkPolicyRule) rules(rr *NetworkPolicyReconciler) (ingressList []*securitygroup.IngressRule,
	egressList []*securitygroup.EgressRule, ready bool) {
	ready = true
	rule := r.rule
	if rule.Direction == antreanetworking.DirectionIn {
		iRules := make([]*securitygroup.IngressRule, 0)
		for _, ip := range rule.From.IPBlocks {
			ingress := &securitygroup.IngressRule{}
			ipNet := net.IPNet{IP: net.IP(ip.CIDR.IP), Mask: net.CIDRMask(int(ip.CIDR.PrefixLength), 32)}
			ingress.FromSrcIP = append(ingress.FromSrcIP, &ipNet)
			iRules = append(iRules, ingress)
		}
		for _, ag := range rule.From.AddressGroups {
			sgs, err := rr.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, ag)
			if err != nil {
				rr.Log.Error(err, "get AddrSecurityGroup indexer", "Name", ag)
				continue
			}
			if len(sgs) == 0 {
				rr.Log.V(1).Info("Ingress rule cannot be computed with unknown AddressGroup", "AddressGroup", ag)
				ready = false
				return
			}
			for _, i := range sgs {
				sg := i.(*addrSecurityGroup)
				for _, ip := range sg.getIPs() {
					ingress := &securitygroup.IngressRule{}
					ingress.FromSrcIP = append(ingress.FromSrcIP, ip)
					iRules = append(iRules, ingress)
				}
				id := sg.getID()
				if len(id.Vpc) > 0 {
					ingress := &securitygroup.IngressRule{}
					ingress.FromSecurityGroups = append(ingress.FromSecurityGroups, &id)
					iRules = append(iRules, ingress)
				}
			}
		}
		if len(iRules) == 0 {
			return
		}
		if rule.Services == nil {
			ingressList = append(ingressList, iRules...)
			return
		}
		for _, s := range rule.Services {
			var protocol *int
			var fromPort *int
			if s.Protocol != nil {
				if p, ok := AntreaProtocolMap[*s.Protocol]; ok {
					protocol = &p
				}
			}
			if s.Port != nil {
				port := int(s.Port.IntVal)
				fromPort = &port
			}
			for _, ingress := range iRules {
				i := deepcopy.Copy(ingress).(*securitygroup.IngressRule)
				i.FromPort = fromPort
				i.Protocol = protocol
				ingressList = append(ingressList, i)
			}
		}
		return
	}
	eRules := make([]*securitygroup.EgressRule, 0)
	for _, ip := range rule.To.IPBlocks {
		egress := &securitygroup.EgressRule{}
		ipNet := net.IPNet{IP: net.IP(ip.CIDR.IP), Mask: net.CIDRMask(int(ip.CIDR.PrefixLength), 32)}
		egress.ToDstIP = append(egress.ToDstIP, &ipNet)
		eRules = append(eRules, egress)
	}
	for _, ag := range rule.To.AddressGroups {
		sgs, err := rr.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, ag)
		if err != nil {
			rr.Log.Error(err, "get AddrSecurityGroup indexer", "Name", ag)
			continue
		}
		if len(sgs) == 0 {
			rr.Log.V(1).Info("Egress rule cannot be computed with unknown AddressGroup", "AddressGroup", ag)
			ready = false
			return
		}
		for _, i := range sgs {
			sg := i.(*addrSecurityGroup)
			for _, ip := range sg.getIPs() {
				egress := &securitygroup.EgressRule{}
				egress.ToDstIP = append(egress.ToDstIP, ip)
				eRules = append(eRules, egress)
			}
			id := sg.getID()
			if len(id.Vpc) > 0 {
				egress := &securitygroup.EgressRule{}
				egress.ToSecurityGroups = append(egress.ToSecurityGroups, &id)
				eRules = append(eRules, egress)
			}
		}
	}
	if len(eRules) == 0 {
		return
	}
	if rule.Services == nil {
		egressList = append(egressList, eRules...)
		return
	}
	for _, s := range rule.Services {
		var protocol *int
		var fromPort *int
		if s.Protocol != nil {
			if p, ok := AntreaProtocolMap[*s.Protocol]; ok {
				protocol = &p
			}
		}
		if s.Port != nil {
			port := int(s.Port.IntVal)
			fromPort = &port
		}
		for _, egress := range eRules {
			e := deepcopy.Copy(egress).(*securitygroup.EgressRule)
			e.ToPort = fromPort
			e.Protocol = protocol
			egressList = append(egressList, e)
		}
	}
	return
}

// networkPolicy describe an Antrea internal/user facing networkPolicy.
type networkPolicy struct {
	antreanetworking.NetworkPolicy
	ingressRules []*securitygroup.IngressRule
	egressRules  []*securitygroup.EgressRule
	rulesReady   bool
}

func (n *networkPolicy) getNamespacedName() string {
	return types.NamespacedName{Name: n.Name, Namespace: n.Namespace}.String()
}

// update an networkPolicy from Antrea controller.
func (n *networkPolicy) update(anp *antreanetworking.NetworkPolicy, recompute bool, r *NetworkPolicyReconciler) {
	if !recompute {
		// Marks appliedToSGs removed.
		n.markDirty(r)
		// Marks appliedToSGs added.
		defer n.markDirty(r)
	}
	// Compute appliedToSecurityGroups that needs updates.
	modifiedAppliedTo := make([]string, 0)
	removedAppliedTo := make([]string, 0)
	addedAppliedTo := make([]string, 0)

	var removedAddr []string
	if recompute {
		if ok := n.computeRules(r); !ok {
			return
		}
		modifiedAppliedTo = n.AppliedToGroups
	} else {
		if !reflect.DeepEqual(anp.Rules, n.Rules) {
			// Indexer does not work with in-place update. Do delete->update->add.
			if err := r.networkPolicyIndexer.Delete(n); err != nil {
				r.Log.Error(err, "delete networkPolicy indexer", "Name", n.Name)
			}
			_, removedAddr = diffAddressGrp(n.Rules, anp.Rules)
			n.Rules = anp.Rules
			n.Generation = anp.Generation
			if err := r.networkPolicyIndexer.Add(n); err != nil {
				r.Log.Error(err, "add networkPolicy indexer", "Name", n.Name)
			}
			if ok := n.computeRules(r); ok {
				modifiedAppliedTo = n.AppliedToGroups
			}
		}
		if !reflect.DeepEqual(anp.AppliedToGroups, n.AppliedToGroups) {
			// Indexer does not work with in-place update. Do delete->update->add.
			if err := r.networkPolicyIndexer.Delete(n); err != nil {
				r.Log.Error(err, "delete networkPolicy indexer", "Name", n.Name)
			}
			addedAppliedTo, removedAppliedTo = diffAppliedToGrp(n.AppliedToGroups, anp.AppliedToGroups)
			n.AppliedToGroups = anp.AppliedToGroups
			n.Generation = anp.Generation
			if err := r.networkPolicyIndexer.Add(n); err != nil {
				r.Log.Error(err, "add networkPolicy indexer", "Name", n.Name)
			}
		}
	}

	// process addressGroup need updates in this networkPolicy.
	for _, id := range removedAddr {
		sgs, err := r.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, id)
		if err != nil {
			r.Log.Error(err, "indexer error for", "AddressGroup", id)
			continue
		}
		for _, i := range sgs {
			sg := i.(*addrSecurityGroup)
			sg.notifyNetworkPolicyChange(r)
		}
	}

	// process appliedToGroups needs updates in this networkPolicy.
	modifiedAppliedTo = append(modifiedAppliedTo, removedAppliedTo...)
	modifiedAppliedTo = append(modifiedAppliedTo, addedAppliedTo...)
	for _, id := range modifiedAppliedTo {
		sgs, err := r.appliedToSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, id)
		if err != nil {
			r.Log.Error(err, "indexer error for", "AppliedToGroup", id)
			continue
		}
		for _, i := range sgs {
			sg := i.(*appliedToSecurityGroup)
			sg.notifyNetworkPolicyChange(r)
			sg.updateANPRules(r, n)
		}
	}
}

// delete deletes a networkPolicy.
func (n *networkPolicy) delete(r *NetworkPolicyReconciler) error {
	n.ingressRules = nil
	n.egressRules = nil
	n.markDirty(r)
	if err := r.networkPolicyIndexer.Delete(n); err != nil {
		r.Log.Error(err, "delete from networkPolicy indexer", "Name", n.Name, "Namespace", n.Namespace)
	}
	for _, gname := range n.AppliedToGroups {
		sgs, err := r.appliedToSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, gname)
		if err != nil {
			return fmt.Errorf("unable to get appliedToSGs %s from indexer: %w", gname, err)
		}
		for _, i := range sgs {
			sg := i.(*appliedToSecurityGroup)
			sg.notifyNetworkPolicyChange(r)
			sg.updateANPRules(r, n)
		}
	}
	var addrGrp []string
	for _, rule := range n.Rules {
		addrGrp = append(addrGrp, rule.To.AddressGroups...)
		addrGrp = append(addrGrp, rule.From.AddressGroups...)
	}
	for _, gname := range addrGrp {
		sgs, err := r.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, gname)
		if err != nil {
			return fmt.Errorf("unable to get addrSGs %s from indexer: %w", gname, err)
		}
		for _, i := range sgs {
			sg := i.(*addrSecurityGroup)
			sg.notifyNetworkPolicyChange(r)
		}
	}
	return nil
}

// computeRulesReady computes if rules associated with networkPolicy are ready.
// It entails that all addrSecurityGroups referenced by networkPolicy are created by
// cloud plug-in.
func (n *networkPolicy) computeRulesReady(withStatus bool, r *NetworkPolicyReconciler) error {
	if withStatus || !n.rulesReady {
		// if any AddrSecurityGroup is not ready, no rules shall be returned.
		addrGrpNames := make(map[string]struct{})
		for _, rule := range n.Rules {
			for _, name := range rule.To.AddressGroups {
				addrGrpNames[name] = struct{}{}
			}
			for _, name := range rule.From.AddressGroups {
				addrGrpNames[name] = struct{}{}
			}
		}
		for name := range addrGrpNames {
			sgs, err := r.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, name)
			if err != nil {
				r.Log.Error(err, "indexer error for", "AddressGroup", name)
				return err
			}
			if len(sgs) == 0 {
				err := fmt.Errorf("internal error")
				r.Log.Error(err, "skip computing rules in networkPolicy because AddrSecurityGroup unknown",
					"networkPolicy", n.Name, "AddressGroup", name)
				return err
			}
			for _, i := range sgs {
				sg := i.(*addrSecurityGroup)
				if withStatus {
					if status := sg.getStatus(); status != nil {
						return fmt.Errorf("%v=%v", sg.id.String(), status.Error())
					}
				}
				if !sg.isReady() {
					r.Log.V(1).Info("Skip computing rules in networkPolicy because AddrSecurityGroup not ready",
						"networkPolicy", n.Name, "AddressSecurityGroup", sg.id.Name)
					return nil
				}
			}
		}
		n.rulesReady = true
	}
	return nil
}

// notifyAddrGrpChanges notifies networkPolicy a referenced addrSecurityGroup has changed.
func (n *networkPolicy) notifyAddrGrpChanges(r *NetworkPolicyReconciler) error {
	// Ignore if ruleReady does not change from notReady to ready.
	if n.rulesReady {
		return nil
	}
	_ = n.computeRulesReady(false, r)
	if !n.rulesReady {
		return nil
	}

	for _, gname := range n.AppliedToGroups {
		sgs, err := r.appliedToSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, gname)
		if err != nil {
			return fmt.Errorf("unable to get appliedToSGs %s from indexer: %w", gname, err)
		}
		for _, i := range sgs {
			sg := i.(*appliedToSecurityGroup)
			if err := sg.updateAllRules(r); err != nil {
				r.Log.Error(err, "networkPolicy update rules")
			}
		}
	}
	return nil
}

// computeRules computes ingress and egress rules associated with networkPolicy.
func (n *networkPolicy) computeRules(rr *NetworkPolicyReconciler) bool {
	rr.Log.V(1).Info("Compute rules", "networkPolicy", n.Name)
	n.ingressRules = nil
	n.egressRules = nil
	n.rulesReady = false
	for _, r := range n.Rules {
		ing, eg, ready := (&networkPolicyRule{rule: &r}).rules(rr)
		if !ready {
			n.ingressRules = nil
			n.egressRules = nil
			return false
		}
		if ing != nil {
			n.ingressRules = append(n.ingressRules, ing...)
		}
		if eg != nil {
			n.egressRules = append(n.egressRules, eg...)
		}
	}
	_ = n.computeRulesReady(false, rr)
	return n.rulesReady
}

// markDirty marks all cloud resources this NetworkPolicy applied to dirty.
func (n *networkPolicy) markDirty(r *NetworkPolicyReconciler) {
	for _, key := range n.AppliedToGroups {
		sgs, err := r.appliedToSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, key)
		if err != nil {
			r.Log.Error(err, "get appliedToSecurityGroup indexer", "Key", key)
			return
		}
		for _, i := range sgs {
			asg := i.(*appliedToSecurityGroup)
			asg.markDirty(r, false)
		}
	}
}

// getStatus returns status of networkPolicy.
func (n *networkPolicy) getStatus(r *NetworkPolicyReconciler) error {
	if n.rulesReady {
		return nil
	}
	if err := n.computeRulesReady(true, r); err != nil {
		return err
	}
	return &InProgress{}
}
