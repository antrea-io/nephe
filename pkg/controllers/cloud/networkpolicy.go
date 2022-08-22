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
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreanetcore "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	cloud "antrea.io/nephe/apis/crd/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
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
		srcMap[s.Name.String()] = s
	}
	for _, r := range removed {
		delete(srcMap, r.Name.String())
	}
	for _, a := range added {
		rsc := *a
		srcMap[a.Name.String()] = &rsc
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
	map[string][]*securitygroup.CloudResource, []*net.IPNet, []string, error) {
	vpcs := make(map[string][]*securitygroup.CloudResource)
	var ipBlocks []*net.IPNet
	var notFoundMember []string
	for _, m := range members {
		if m.ExternalEntity == nil {
			continue
		}
		e := &antreanetcore.ExternalEntity{}
		key := client.ObjectKey{Name: m.ExternalEntity.Name, Namespace: m.ExternalEntity.Namespace}
		if err := r.Get(context.TODO(), key, e); err != nil {
			if apierrors.IsNotFound(err) {
				if ips, ok := r.fedExternalEntityIPs[key.String()]; ok {
					for _, ip := range ips {
						_, ipnet, _ := net.ParseCIDR(ip + "/32")
						ipBlocks = append(ipBlocks, ipnet)
					}
				} else {
					notFoundMember = append(notFoundMember, m.ExternalEntity.Name)
				}
				continue
			}
			r.Log.Error(err, "Client get ExternalEntity", "key", key)
			return nil, nil, nil, err
		}
		kind, ok := e.Labels[config.ExternalEntityLabelKeyKind]
		if !ok {
			r.Log.Error(fmt.Errorf(""), "Kind label not found in ExternalEntity", "key", key, "labels", e.Labels)
			continue
		}
		var cloudRsc securitygroup.CloudResource
		readAnnotations := false
		if kind == converter.GetExternalEntityLabelKind(&cloud.VirtualMachine{}) {
			cloudRsc.Type = securitygroup.CloudResourceTypeVM
			readAnnotations = true
		} else {
			r.Log.Error(fmt.Errorf(""), "Invalid cloud resource type received", "kind", kind)
		}
		if readAnnotations {
			ownerAnnotations, err := getOwnerAnnotations(e, r)
			if err != nil {
				r.Log.Error(err, "externalEntity owner not found", "key", key, "kind", kind)
				continue
			}
			vpc, ok := ownerAnnotations[cloudcommon.AnnotationCloudAssignedVPCIDKey]
			if !ok {
				r.Log.Error(fmt.Errorf(""), "VPC annotation not found in ExternalEntity owner", "key", key, "kind", kind)
				continue
			}
			cloudRsc.Name.Vpc = vpc
			vpcs[vpc] = append(vpcs[vpc], &cloudRsc)
			cloudAssignedID, ok := ownerAnnotations[cloudcommon.AnnotationCloudAssignedIDKey]
			if !ok {
				r.Log.Error(fmt.Errorf(""), "cloud assigned ID annotation not found in ExternalEntity owner", "key", key, "kind", kind)
				continue
			}
			cloudRsc.Name.Name = cloudAssignedID
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

func getOwnerAnnotations(e *antreanetcore.ExternalEntity, r *NetworkPolicyReconciler) (map[string]string, error) {
	if len(e.OwnerReferences) == 0 {
		return nil, fmt.Errorf("externalEntiry owner not found (%v/%v)", e.Namespace, e.Name)
	}
	namespace := e.Namespace
	owner := e.OwnerReferences[0]
	key := client.ObjectKey{Name: owner.Name, Namespace: namespace}
	if owner.Kind == cloudcommon.VirtualMachineCRDKind {
		vm := &cloud.VirtualMachine{}
		if err := r.Get(context.TODO(), key, vm); err != nil {
			r.Log.Error(err, "Client get VirtualMachine", "key", key)
			return nil, err
		}
		return vm.Annotations, nil
	}
	return nil, fmt.Errorf("unsupported cloud owner kind")
}

// deduplicateKey is used for deduplicate network policy rules.
type deduplicateKey struct {
	port     int
	protocol int
}

// overlap decides whether two ip blocks overlap(one contains the other).
// If so, return the one with smaller range. Otherwise, return nil.
func overlap(ip1 *net.IPNet, ip2 *net.IPNet) *net.IPNet {
	maskLen1, _ := ip1.Mask.Size()
	maskLen2, _ := ip2.Mask.Size()
	if maskLen1 < maskLen2 {
		if ip1.Contains(ip2.IP) {
			return ip2
		}
	} else {
		if ip2.Contains(ip1.IP) {
			return ip1
		}
	}
	return nil
}

// deduplicateIP deduplicate a slice of IP addresses.
// For overlapping ip blocks, we use the one with the widest range.
func deduplicateIP(inputIP []*net.IPNet) []*net.IPNet {
	ipList := make([]*net.IPNet, 0)
	existing := make(map[string]*net.IPNet)
	for _, ip := range inputIP {
		if _, ok := existing[ip.String()]; ok {
			continue
		}
		existing[ip.String()] = ip
		ipList = append(ipList, ip)
	}
	for i := 0; i < len(ipList); i++ {
		for j := i + 1; j < len(ipList); j++ {
			smallerIP := overlap(ipList[i], ipList[j])
			if smallerIP != nil {
				delete(existing, smallerIP.String())
			}
		}
	}
	outputIP := make([]*net.IPNet, 0)
	for _, v := range existing {
		outputIP = append(outputIP, v)
	}
	return outputIP
}

// deduplicateSG deduplicate a slice of security groups.
func deduplicateSG(inputSG []*securitygroup.CloudResourceID) []*securitygroup.CloudResourceID {
	outputSG := make([]*securitygroup.CloudResourceID, 0)
	existing := make(map[string]struct{})
	for _, sg := range inputSG {
		if _, ok := existing[sg.String()]; ok {
			continue
		}
		existing[sg.String()] = struct{}{}
		outputSG = append(outputSG, sg)
	}
	return outputSG
}

// deduplicateIngressRules merges duplicated ingress rules on one port into one.
func deduplicateIngressRules(ingressRules []*securitygroup.IngressRule) []*securitygroup.IngressRule {
	inRuleIPSet := make(map[deduplicateKey][]*net.IPNet)
	inRuleSGSet := make(map[deduplicateKey][]*securitygroup.CloudResourceID)
	mergedInRules := make([]*securitygroup.IngressRule, 0)
	for _, r := range ingressRules {
		port, protocol := 0, 0
		if r.FromPort != nil {
			port = *(r.FromPort)
		}
		if r.Protocol != nil {
			protocol = *(r.Protocol)
		}
		ruleKey := deduplicateKey{port: port, protocol: protocol}
		inRuleIPSet[ruleKey] = append(inRuleIPSet[ruleKey], r.FromSrcIP...)
		inRuleSGSet[ruleKey] = append(inRuleSGSet[ruleKey], r.FromSecurityGroups...)
	}
	for k, v := range inRuleIPSet {
		port, protocol := k.port, k.protocol
		var portP, protocolP *int
		if port != 0 {
			portP = &(port)
		}
		if protocol != 0 {
			protocolP = &(protocol)
		}
		inRule := securitygroup.IngressRule{FromPort: portP, FromSrcIP: deduplicateIP(v),
			FromSecurityGroups: deduplicateSG(inRuleSGSet[k]), Protocol: protocolP}
		mergedInRules = append(mergedInRules, &inRule)
	}
	return mergedInRules
}

// deduplicateEgressRules merges duplicated egress rules on one port into one.
func deduplicateEgressRules(egressRules []*securitygroup.EgressRule) []*securitygroup.EgressRule {
	eRuleIPSet := make(map[deduplicateKey][]*net.IPNet)
	eRuleSGSet := make(map[deduplicateKey][]*securitygroup.CloudResourceID)
	mergedERules := make([]*securitygroup.EgressRule, 0)
	for _, r := range egressRules {
		port, protocol := 0, 0
		if r.ToPort != nil {
			port = *(r.ToPort)
		}
		if r.Protocol != nil {
			protocol = *(r.Protocol)
		}
		ruleKey := deduplicateKey{port: port, protocol: protocol}
		eRuleIPSet[ruleKey] = append(eRuleIPSet[ruleKey], r.ToDstIP...)
		eRuleSGSet[ruleKey] = append(eRuleSGSet[ruleKey], r.ToSecurityGroups...)
	}
	for k, v := range eRuleIPSet {
		port, protocol := k.port, k.protocol
		var portP, protocolP *int
		if port != 0 {
			portP = &(port)
		}
		if protocol != 0 {
			protocolP = &(protocol)
		}
		eRule := securitygroup.EgressRule{ToPort: portP, ToDstIP: deduplicateIP(v),
			ToSecurityGroups: deduplicateSG(eRuleSGSet[k]), Protocol: protocolP}
		mergedERules = append(mergedERules, &eRule)
	}
	return mergedERules
}

// securityGroupImpl supplies common implementations for addrSecurityGroup and appliedToSecurityGroup.
type securityGroupImpl struct {
	// Members of this SecurityGroup.
	members []*securitygroup.CloudResource
	// SecurityGroup identifier.
	id securitygroup.CloudResourceID
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
	return s.id
}

// getMembers returns securityGroup members.
func (s *securityGroupImpl) getMembers() []*securitygroup.CloudResource {
	return s.members
}

func (s *securityGroupImpl) removeStaleMembers(stales []string, r *NetworkPolicyReconciler) {
	if len(s.members) == 0 {
		return
	}
	srcMap := make(map[string]*securitygroup.CloudResource)
	for _, m := range s.members {
		srcMap[m.Name.Name] = m
	}
	for _, stale := range stales {
		for k := range srcMap {
			if strings.Contains(stale, k) {
				r.Log.V(1).Info("Remove stale members from SecurityGroup", "Stale", stale, "Name", s.id)
				delete(srcMap, k)
			}
		}
	}
	var members []*securitygroup.CloudResource
	for _, m := range srcMap {
		members = append(members, m)
	}
	s.members = members
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
	r.Log.V(1).Info("Adding SecurityGroup", "Name", s.id, "MembershipOnly", membershipOnly)
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
	uName := getGroupUniqueName(s.id.String(), membershipOnly)
	guName := getGroupUniqueName(s.id.Name, membershipOnly)
	if !r.pendingDeleteGroups.Has(guName) {
		r.pendingDeleteGroups.Add(guName, &pendingGroup{refCnt: new(int)})
	}
	if s.state != securityGroupStateGarbageCollectState {
		sgs, err := r.networkPolicyIndexer.ByIndex(indexKey, s.id.Name)
		if err != nil {
			return fmt.Errorf("get networkpolicy indexer with index=%v, name=%v: %w", indexKey, s.id.Name, err)
		}
		if len(sgs) != 0 {
			r.Log.V(1).Info("Deleting SecurityGroup pending", "Name", s.id, "MembershipOnly", membershipOnly)
			s.deletePending = true
			return nil
		}
		r.Log.V(1).Info("Deleting SecurityGroup", "Name", s.id, "MembershipOnly", membershipOnly)
		if err := indexer.Delete(c); err != nil {
			r.Log.Error(err, "Deleting SecurityGroup from indexer", "Name", s.id)
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
	r.Log.V(1).Info("Updating SecurityGroup", "Name", s.id, "MembershipOnly", membershipOnly,
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
func (s *securityGroupImpl) notifyImpl(c PendingItem, membershipOnly bool, op securityGroupOperation,
	status error, r *NetworkPolicyReconciler) {
	moreOps := false
	uName := getGroupUniqueName(s.id.String(), membershipOnly)
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
func newAddrSecurityGroup(id *securitygroup.CloudResourceID, data interface{}, state *securityGroupState) cloudSecurityGroup {
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
	r.Log.V(1).Info("AddrSecurityGroup UpdateIPs", "Name", a.id)
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
		r.Log.Error(err, "Get networkPolicy with key %s from indexer: %w", a.id.Name, err)
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
		r.Log.Error(status, "AddrSecurityGroup operation failed", "Name", a.id, "Op", op)
		return nil
	}

	r.Log.V(1).Info("AddrSecurityGroup operation received response", "Name", a.id, "state", a.state, "status", a.status, "Op", op)
	uName := getGroupUniqueName(a.id.String(), true)
	if r.retryQueue.Has(uName) {
		_ = r.retryQueue.Update(uName, false, op)
	}
	switch op {
	case securityGroupOperationAdd:
		if a.state == securityGroupStateInit {
			a.state = securityGroupStateCreated
		}
	default:
		r.Log.V(1).Info("AddrSecurityGroup no response processing.")
		return nil
	}

	r.Log.V(1).Info("AddrSecurityGroup becomes ready", "Name", a.id)
	// update addrSecurityGroup members.
	if err := a.update(nil, nil, r); err != nil {
		return err
	}
	// Update networkPolicies.
	for _, i := range nps {
		np := i.(*networkPolicy)
		if err := np.notifyAddrGrpChanges(r); err != nil {
			r.Log.Error(err, "NetworkPolicy", "name", np.Name)
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
	sgs, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAddrGrp, a.id.Name)
	if err != nil {
		r.Log.Error(err, "Get networkPolicy indexer", a.id.Name, err, "indexKey", networkPolicyIndexerByAddrGrp)
	}
	r.Log.V(1).Info("AddrSecurityGroup notifyNetworkPolicyChange", "Name",
		a.id.String(), "anpNum", len(sgs))
	if len(sgs) == 0 {
		if err := a.delete(r); err != nil {
			r.Log.Error(err, "Delete securityGroup", "Name", a.id.Name)
		}
	}
}

// appliedToSecurityGroup contains information to create a cloud appliedToSecurityGroup.
type appliedToSecurityGroup struct {
	securityGroupImpl
	hasRules   bool
	hasMembers bool
}

// newAddrAppliedGroup creates a new addSecurityGroup from Antrea AddressGroup membership.
func newAppliedToSecurityGroup(id *securitygroup.CloudResourceID, data interface{}, state *securityGroupState) cloudSecurityGroup {
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

// updateRules invokes cloud plug-in to update rules of appliedToSecurityGroup.
func (a *appliedToSecurityGroup) updateRules(r *NetworkPolicyReconciler) error {
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
		if a.hasMembers {
			r.Log.V(1).Info("AppliedToSecurityGroup clear members with no rules", "Name", a.id)
			ch := securitygroup.CloudSecurityGroup.UpdateSecurityGroupMembers(&a.id, nil, false)
			go func() {
				err := <-ch
				r.cloudResponse <- &securityGroupStatus{sg: a, op: securityGroupOperationClearMembers, err: err}
			}()
			return nil
		}
		// No need to update appliedToSecurityGroup with no members.
		r.Log.V(1).Info("AppliedToSecurityGroup clear rules", "Name", a.id)
		a.hasRules = false
		return nil
	}
	irules := make([]*securitygroup.IngressRule, 0)
	erules := make([]*securitygroup.EgressRule, 0)
	for _, i := range nps {
		np := i.(*networkPolicy)
		if !np.rulesReady {
			return nil
		}
		irules = append(irules, deepcopy.Copy(np.ingressRules).([]*securitygroup.IngressRule)...)
		erules = append(erules, deepcopy.Copy(np.egressRules).([]*securitygroup.EgressRule)...)
	}
	irules, erules = deduplicateIngressRules(irules), deduplicateEgressRules(erules)
	r.Log.V(1).Info("AppliedToSecurityGroup update rules", "Name", a.id,
		"ingressRules", irules, "egressRules", erules)
	ch := securitygroup.CloudSecurityGroup.UpdateSecurityGroupRules(&a.id, irules, erules)
	go func() {
		err := <-ch
		r.cloudResponse <- &securityGroupStatus{sg: a, op: securityGroupOperationUpdateRules, err: err}
	}()
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
	if a.state == securityGroupStateCreated && a.hasRules {
		return nil
	}
	return &InProgress{}
}

// notify calls into appliedToSecurityGroup to report operation status from cloud plug-in.
func (a *appliedToSecurityGroup) notify(op securityGroupOperation, status error, r *NetworkPolicyReconciler) error {
	defer func() {
		trackers, err := r.cloudResourceNPTrackerIndexer.ByIndex(cloudResourceNPTrackerIndexerByAppliedToGrp, a.id.String())
		if err != nil {
			r.Log.Error(err, "Get cloud resource tracker indexer", "Key", a.id)
			return
		}
		for _, i := range trackers {
			tracker := i.(*cloudResourceNPTracker)
			tracker.markDirty()
		}
		a.notifyImpl(a, false, op, status, r)
	}()

	if !(a.state == securityGroupStateGarbageCollectState && op != securityGroupOperationDelete) {
		a.status = status
	}
	if status != nil {
		r.Log.Error(status, "AppliedToSecurityGroup operation failed", "Name", a.id, "Op", op)
		return nil
	}
	r.Log.V(1).Info("AppliedToSecurityGroup received operation ok", "Name", a.id, "state", a.state, "status", a.status, "Op", op)
	uName := getGroupUniqueName(a.id.String(), false)
	if r.retryQueue.Has(uName) {
		_ = r.retryQueue.Update(uName, false, op)
	}
	switch op {
	case securityGroupOperationAdd:
		// AppliedToSecurityGroup becomes ready. apply any rules.
		if a.state == securityGroupStateInit {
			a.state = securityGroupStateCreated
			return a.updateRules(r)
		}
	case securityGroupOperationUpdateMembers:
		a.hasMembers = true
	case securityGroupOperationUpdateRules:
		// AppliedToSecurityGroup added rules, now add members.
		a.hasRules = true
		if !a.hasMembers {
			return a.update(nil, nil, r)
		}
	case securityGroupOperationClearMembers:
		a.hasMembers = false
		// AppliedToSecurityGroup has cleared members, clear rules.
		if a.hasRules {
			return a.updateRules(r)
		}
	default:
		r.Log.V(1).Info("AppliedToSecurityGroup no response processing.")
	}
	return nil
}

// notifyNetworkPolicyChange notifies some NetworkPolicy reference to this securityGroup has changed.
func (a *appliedToSecurityGroup) notifyNetworkPolicyChange(r *NetworkPolicyReconciler) {
	sgs, err := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, a.id.Name)
	if err != nil {
		r.Log.Error(err, "Get networkPolicy indexer", a.id.Name, err, "indexKey", networkPolicyIndexerByAppliedToGrp)
	}
	r.Log.V(1).Info("AppliedToSecurityGroup notifyNetworkPolicyChange", "Name",
		a.id.String(), "anpNum", len(sgs))
	if len(sgs) == 0 && a.deletePending {
		if err := a.delete(r); err != nil {
			r.Log.Error(err, "Delete securityGroup", "Name", a.id.Name)
		}
	} else if !a.deletePending {
		_ = a.updateRules(r)
	}
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
		ingress := &securitygroup.IngressRule{}
		for _, ip := range rule.From.IPBlocks {
			ipNet := net.IPNet{IP: net.IP(ip.CIDR.IP), Mask: net.CIDRMask(int(ip.CIDR.PrefixLength), 32)}
			ingress.FromSrcIP = append(ingress.FromSrcIP, &ipNet)
		}
		for _, ag := range rule.From.AddressGroups {
			sgs, err := rr.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, ag)
			if err != nil {
				rr.Log.Error(err, "Get AddrSecurityGroup indexer", "Name", ag)
				continue
			}
			if len(sgs) == 0 {
				rr.Log.V(1).Info("Ingress rule cannot be computed with unknown AddressGroup", "AddressGroup", ag)
				ready = false
				return
			}
			for _, i := range sgs {
				sg := i.(*addrSecurityGroup)
				ingress.FromSrcIP = append(ingress.FromSrcIP, sg.getIPs()...)
				id := sg.getID()
				if len(id.Vpc) > 0 {
					ingress.FromSecurityGroups = append(ingress.FromSecurityGroups, &id)
				}
			}
		}
		if ingress.Protocol == nil && ingress.FromSecurityGroups == nil && ingress.FromSrcIP == nil {
			return
		}
		if rule.Services == nil {
			ingressList = append(ingressList, ingress)
			return
		}
		for _, s := range rule.Services {
			ii := deepcopy.Copy(ingress).(*securitygroup.IngressRule)
			if s.Protocol != nil {
				if p, ok := AntreaProtocolMap[*s.Protocol]; ok {
					ii.Protocol = &p
				}
			}
			if s.Port != nil {
				port := int(s.Port.IntVal)
				ii.FromPort = &port
			}
			ingressList = append(ingressList, ii)
		}
		return
	}
	egress := &securitygroup.EgressRule{}
	for _, ip := range rule.To.IPBlocks {
		ipNet := net.IPNet{IP: net.IP(ip.CIDR.IP), Mask: net.CIDRMask(int(ip.CIDR.PrefixLength), 32)}
		egress.ToDstIP = append(egress.ToDstIP, &ipNet)
	}
	for _, ag := range rule.To.AddressGroups {
		sgs, err := rr.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, ag)
		if err != nil {
			rr.Log.Error(err, "Get AddrSecurityGroup indexer", "Name", ag)
			continue
		}
		if len(sgs) == 0 {
			rr.Log.V(1).Info("Egress rule cannot be computed with unknown AddressGroup", "AddressGroup", ag)
			ready = false
			return
		}
		for _, i := range sgs {
			sg := i.(*addrSecurityGroup)
			egress.ToDstIP = append(egress.ToDstIP, sg.getIPs()...)
			id := sg.getID()
			if len(id.Vpc) > 0 {
				egress.ToSecurityGroups = append(egress.ToSecurityGroups, &id)
			}
		}
	}
	if egress.Protocol == nil && egress.ToSecurityGroups == nil && egress.ToDstIP == nil {
		return
	}
	if rule.Services == nil {
		egressList = append(egressList, egress)
		return
	}
	for _, s := range rule.Services {
		// No deep copy ??
		ee := deepcopy.Copy(egress).(*securitygroup.EgressRule)
		if s.Protocol != nil {
			if p, ok := AntreaProtocolMap[*s.Protocol]; ok {
				ee.Protocol = &p
			}
		}
		if s.Port != nil {
			port := int(s.Port.IntVal)
			ee.ToPort = &port
		}
		egressList = append(egressList, ee)
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

// update an networkPolicy from Antrea controller.
func (n *networkPolicy) update(anp *antreanetworking.NetworkPolicy, recompute bool, r *NetworkPolicyReconciler) {
	if !recompute {
		// Marks appliedToSGs removed
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
			// Indexer does not work with in-place update. Do delete->update->add
			if err := r.networkPolicyIndexer.Delete(n); err != nil {
				r.Log.Error(err, "Delete networkPolicy indexer", "Name", n.Name)
			}
			_, removedAddr = diffAddressGrp(n.Rules, anp.Rules)
			r.Log.V(1).Info("AddressGroup changes in NetworkPolicy", "old", n.Rules, "new", anp.Rules,
				"diff", removedAddr)
			n.Rules = anp.Rules
			if err := r.networkPolicyIndexer.Add(n); err != nil {
				r.Log.Error(err, "Add networkPolicy indexer", "Name", n.Name)
			}
			if ok := n.computeRules(r); ok {
				modifiedAppliedTo = n.AppliedToGroups
			}
		}
		if !reflect.DeepEqual(anp.AppliedToGroups, n.AppliedToGroups) {
			// Indexer does not work with in-place update. Do delete->update->add
			if err := r.networkPolicyIndexer.Delete(n); err != nil {
				r.Log.Error(err, "Delete networkPolicy indexer", "Name", n.Name)
			}
			addedAppliedTo, removedAppliedTo = diffAppliedToGrp(n.AppliedToGroups, anp.AppliedToGroups)
			n.AppliedToGroups = anp.AppliedToGroups
			if err := r.networkPolicyIndexer.Add(n); err != nil {
				r.Log.Error(err, "Add networkPolicy indexer", "Name", n.Name)
			}
		}
	}

	// process addressGroup need updates in this networkPolicy.
	for _, id := range removedAddr {
		sgs, err := r.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, id)
		if err != nil {
			r.Log.Error(err, "Indexer error for", "AddressGroup", id)
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
			r.Log.Error(err, "Indexer error for", "AppliedToGroup", id)
			continue
		}
		for _, i := range sgs {
			sg := i.(*appliedToSecurityGroup)
			sg.notifyNetworkPolicyChange(r)
		}
	}
}

// delete deletes a networkPolicy.
func (n *networkPolicy) delete(r *NetworkPolicyReconciler) error {
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
				r.Log.Error(err, "Indexer error for", "AddressGroup", name)
				return err
			}
			if len(sgs) == 0 {
				err := fmt.Errorf("internal error")
				r.Log.Error(err, "No rules in networkPolicy because addrGroup unknown",
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
					r.Log.V(1).Info("No rules in networkPolicy because AddrSecurityGroup not ready",
						"networkPolicy", n.Name, "AddressSecurityGroup", sg.id)
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
			if err := sg.updateRules(r); err != nil {
				r.Log.Error(err, "NetworkPolicy update rules")
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
			r.Log.Error(err, "Get appliedToSecurityGroup indexer", "Key", key)
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
	if n.ingressRules == nil && n.egressRules == nil {
		return &InProgress{}
	}
	return n.computeRulesReady(true, r)
}
