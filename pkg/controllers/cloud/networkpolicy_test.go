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
	"sort"
	"strings"
	"time"

	mock "github.com/golang/mock/gomock"
	"github.com/mohae/deepcopy"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	cloud "antrea.io/nephe/apis/crd/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/pkg/converter/target"
	cloudtest "antrea.io/nephe/pkg/testing/cloudsecurity"
	"antrea.io/nephe/pkg/testing/controllerruntimeclient"
)

var _ = Describe("NetworkPolicy", func() {
	type securityGroupConfig struct {
		sgDeleteError     error
		sgCreateError     error
		sgMemberError     error
		sgRuleError       error
		k8sGetError       error
		sgRuleNoOrder     bool
		sgDeletePending   bool
		addrSgMemberTimes int
		appSgMemberTimes  int
		appSgRuleTimes    int
		sgCreateTimes     int
		sgDeleteTimes     int
	}

	var (
		reconciler             *NetworkPolicyReconciler
		anp                    *antreanetworking.NetworkPolicy
		namespace              = "anp-ns"
		vpc                    = "test-vpc"
		addrGrpNames           = []string{"addr-grp-1", "addr-grp-2"}
		addrGrps               []*antreanetworking.AddressGroup
		addrGrpIDs             map[string]*securitygroup.CloudResourceID
		appliedToGrpsNames     = []string{"applied-grp-1", "applied-grp-2"}
		appliedToGrps          []*antreanetworking.AppliedToGroup
		appliedToGrpIDs        map[string]*securitygroup.CloudResourceID
		vmNames                = []string{"vm-1", "vm-2", "vm-3", "vm-4", "vm-5", "vm-6"}
		vmNamePrefix           = "id-"
		vmNameToIDMap          map[string]string
		vmExternalEntities     map[string]*antreatypes.ExternalEntity
		vmNameToVirtualMachine map[string]*cloud.VirtualMachine
		vmMembers              map[string]*securitygroup.CloudResource
		ingressRule            *securitygroup.IngressRule
		egressRule             *securitygroup.EgressRule
		patchVMIdx             int
		syncContents           []securitygroup.SynchronizationContent

		// tunable
		sgConfig securityGroupConfig
	)

	BeforeEach(func() {
		mockCtrl = mock.NewController(GinkgoT())
		mockClient = controllerruntimeclient.NewMockClient(mockCtrl)
		mockStatusWriter = controllerruntimeclient.NewMockStatusWriter(mockCtrl)
		mockCloudSecurityAPI = cloudtest.NewMockCloudSecurityGroupAPI(mockCtrl)
		securitygroup.CloudSecurityGroup = mockCloudSecurityAPI
		reconciler = &NetworkPolicyReconciler{
			Log:             logf.Log,
			Client:          mockClient,
			syncedWithCloud: true,
		}
		err := reconciler.SetupWithManager(nil)
		Expect(err).ToNot(HaveOccurred())
		vmMembers = make(map[string]*securitygroup.CloudResource)
		vmExternalEntities = make(map[string]*antreatypes.ExternalEntity)
		vmNameToVirtualMachine = make(map[string]*cloud.VirtualMachine)
		vmNameToIDMap = make(map[string]string)
		// ExternalEntity
		for _, n := range vmNames {
			ee := antreatypes.ExternalEntity{}
			ee.Name = "virtualmachine-" + n
			ee.Namespace = namespace
			eeOwner := v1.OwnerReference{
				Kind: reflect.TypeOf(cloud.VirtualMachine{}).Name(),
				Name: n,
			}
			ee.OwnerReferences = append(ee.OwnerReferences, eeOwner)

			labels := make(map[string]string)
			// TODO: cleanup dead code
			labels[config.ExternalEntityLabelKeyKind] = target.GetExternalEntityLabelKind(&cloud.VirtualMachine{})
			labels[config.ExternalEntityLabelKeyName] = n
			labels[config.ExternalEntityLabelKeyNamespace] = namespace
			labels[config.ExternalEntityLabelCloudVPCKey] = vpc
			ee.Labels = labels
			vmExternalEntities[n] = &ee
			vmMembers[n] = &securitygroup.CloudResource{
				Type: securitygroup.CloudResourceTypeVM,
				Name: securitygroup.CloudResourceID{Name: vmNamePrefix + n, Vpc: vpc}}

			vm := cloud.VirtualMachine{}
			vmID := vmNamePrefix + n
			vm.Name = n
			vm.Namespace = namespace
			vmAnnotations := make(map[string]string)
			vmAnnotations[cloudcommon.AnnotationCloudAssignedIDKey] = vmID
			vmAnnotations[cloudcommon.AnnotationCloudAssignedVPCIDKey] = vpc
			vm.Annotations = vmAnnotations
			vmNameToVirtualMachine[n] = &vm
			vmNameToIDMap[n] = vmID
		}

		// AddressGroups
		addrGrpIDs = make(map[string]*securitygroup.CloudResourceID)
		vmIdx := 0
		ingressIdx := 0
		addrGrps = nil
		syncContents = nil
		for _, n := range addrGrpNames {
			ag := &antreanetworking.AddressGroup{}
			ag.Name = n
			efvm := &antreanetworking.ExternalEntityReference{Name: vmExternalEntities[vmNames[vmIdx]].Name, Namespace: namespace}
			syncContent := securitygroup.SynchronizationContent{}
			syncContent.Resource = securitygroup.CloudResourceID{Name: ag.Name, Vpc: vpc}
			syncContent.Members = []securitygroup.CloudResource{
				{Type: securitygroup.CloudResourceTypeVM,
					Name: securitygroup.CloudResourceID{Name: vmNameToIDMap[vmNames[vmIdx]], Vpc: vpc}},
			}
			syncContent.MembershipOnly = true
			syncContents = append(syncContents, syncContent)
			vmIdx++
			ingressIdx++
			ag.GroupMembers = []antreanetworking.GroupMember{
				{ExternalEntity: efvm},
			}
			addrGrps = append(addrGrps, ag)
			addrGrpIDs[n] = &securitygroup.CloudResourceID{Name: n, Vpc: vpc}
		}
		// AppliedToGroups
		appliedToGrpIDs = make(map[string]*securitygroup.CloudResourceID)
		appliedToGrps = nil
		appliedToVMIdx := vmIdx
		for _, n := range appliedToGrpsNames {
			ag := &antreanetworking.AppliedToGroup{}
			ag.Name = n
			ef := &antreanetworking.ExternalEntityReference{Name: vmExternalEntities[vmNames[vmIdx]].Name, Namespace: namespace}
			syncContent := securitygroup.SynchronizationContent{}
			syncContent.Resource = securitygroup.CloudResourceID{Name: ag.Name, Vpc: vpc}
			syncContent.Members = []securitygroup.CloudResource{
				{Type: securitygroup.CloudResourceTypeVM,
					Name: securitygroup.CloudResourceID{Name: vmNameToIDMap[vmNames[vmIdx]], Vpc: vpc}},
			}
			syncContents = append(syncContents, syncContent)
			vmIdx++
			ag.GroupMembers = []antreanetworking.GroupMember{{ExternalEntity: ef}}
			appliedToGrps = append(appliedToGrps, ag)
			appliedToGrpIDs[n] = &securitygroup.CloudResourceID{Name: n, Vpc: vpc}
		}
		patchVMIdx = vmIdx
		// NetworkPolicy
		anp = &antreanetworking.NetworkPolicy{}
		anp.Name = "anp-test"
		anp.Namespace = namespace
		anp.AppliedToGroups = appliedToGrpsNames
		anp.SourceRef = &antreanetworking.NetworkPolicyReference{
			Type: antreanetworking.AntreaNetworkPolicy,
		}
		protocol := antreanetworking.ProtocolTCP
		port := &intstr.IntOrString{IntVal: 443}
		inRule := antreanetworking.NetworkPolicyRule{Direction: antreanetworking.DirectionIn}
		inRule.Services = []antreanetworking.Service{
			{Port: port, Protocol: &protocol},
		}
		_, ingressIPBlock, _ := net.ParseCIDR("5.5.5.0/24")
		ipInBlock := antreanetworking.IPBlock{}
		ipInBlock.CIDR.IP = antreanetworking.IPAddress(ingressIPBlock.IP)
		ipInBlock.CIDR.PrefixLength = 24
		inRule.From = antreanetworking.NetworkPolicyPeer{AddressGroups: []string{addrGrpNames[0]},
			IPBlocks: []antreanetworking.IPBlock{ipInBlock}}
		anp.Rules = append(anp.Rules, inRule)
		eRule := antreanetworking.NetworkPolicyRule{Direction: antreanetworking.DirectionOut}
		eRule.Services = []antreanetworking.Service{
			{Port: port, Protocol: &protocol},
		}
		_, egressIPBlock, _ := net.ParseCIDR("6.6.6.0/24")
		ipEgBlock := antreanetworking.IPBlock{}
		ipEgBlock.CIDR.IP = antreanetworking.IPAddress(egressIPBlock.IP)
		ipEgBlock.CIDR.PrefixLength = 24
		eRule.To = antreanetworking.NetworkPolicyPeer{AddressGroups: []string{addrGrpNames[1]},
			IPBlocks: []antreanetworking.IPBlock{ipEgBlock}}
		anp.Rules = append(anp.Rules, eRule)

		// rules
		tcp := 6
		portInt := int(port.IntVal)
		ingressRule = &securitygroup.IngressRule{
			FromPort:           &portInt,
			Protocol:           &tcp,
			FromSecurityGroups: []*securitygroup.CloudResourceID{addrGrpIDs[addrGrps[0].Name]},
			FromSrcIP:          []*net.IPNet{ingressIPBlock},
		}
		egressRule = &securitygroup.EgressRule{
			ToPort:           &portInt,
			Protocol:         &tcp,
			ToSecurityGroups: []*securitygroup.CloudResourceID{addrGrpIDs[addrGrps[1].Name]},
			ToDstIP:          []*net.IPNet{egressIPBlock},
		}

		for i := appliedToVMIdx; i < patchVMIdx; i++ {
			syncContents[i].EgressRules = []securitygroup.EgressRule{*egressRule}
			syncContents[i].IngressRules = []securitygroup.IngressRule{*ingressRule}
		}
		sgConfig = securityGroupConfig{}
		sgConfig.sgCreateTimes = 1
		sgConfig.addrSgMemberTimes = 1
		sgConfig.appSgMemberTimes = 1
		sgConfig.appSgRuleTimes = 1
		sgConfig.sgDeleteTimes = 1
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	// returns cloud resources associated with K8s GroupMembers.
	getGrpMembers := func(gms []antreanetworking.GroupMember) []*securitygroup.CloudResource {
		ret := make([]*securitygroup.CloudResource, 0)
		for _, gm := range gms {
			if vm := strings.TrimPrefix(gm.ExternalEntity.Name, "virtualmachine-"); vm != gm.ExternalEntity.Name {
				ret = append(ret, &securitygroup.CloudResource{Type: securitygroup.CloudResourceTypeVM,
					Name: securitygroup.CloudResourceID{Name: vmNameToIDMap[vm], Vpc: vpc}})
			}
		}
		return ret
	}

	// returns k8s ExternalEntities associated with K8s GroupMembers.
	getGrpExternalEntity := func(gms []antreanetworking.GroupMember) []*antreatypes.ExternalEntity {
		ret := make([]*antreatypes.ExternalEntity, 0)
		for _, gm := range gms {
			if vm := strings.TrimPrefix(gm.ExternalEntity.Name, "virtualmachine-"); vm != gm.ExternalEntity.Name {
				// Trim successful
				ret = append(ret, vmExternalEntities[vm])
			}
		}
		return ret
	}

	// returns cloud VPC associated with K8s GroupMembers.
	getGrpVPCs := func(gms []antreanetworking.GroupMember) map[string]struct{} {
		ret := make(map[string]struct{})
		for _, gm := range gms {
			if vm := strings.TrimPrefix(gm.ExternalEntity.Name, "virtualmachine-"); vm != gm.ExternalEntity.Name {
				ret[vpc] = struct{}{}
			}
		}
		return ret
	}

	checkAddrGroup := func(ag *antreanetworking.AddressGroup) {
		for _, ref := range getGrpExternalEntity(ag.GroupMembers) {
			ee := &antreatypes.ExternalEntity{}
			ref.DeepCopyInto(ee)
			key := client.ObjectKey{Name: ee.Name, Namespace: ee.Namespace}
			mockClient.EXPECT().Get(mock.Any(), key, mock.Any()).
				Return(sgConfig.k8sGetError).MaxTimes(1).
				Do(func(_ context.Context, key client.ObjectKey, out *antreatypes.ExternalEntity) {
					ee.DeepCopyInto(out)
				})
			if len(ee.OwnerReferences) != 0 {
				owner := ee.OwnerReferences[0]
				key := client.ObjectKey{Name: owner.Name, Namespace: ee.Namespace}
				mockClient.EXPECT().Get(mock.Any(), key, mock.Any()).
					Return(nil).AnyTimes().
					Do(func(_ context.Context, key client.ObjectKey, out *cloud.VirtualMachine) {
						vm := vmNameToVirtualMachine[owner.Name]
						vm.DeepCopyInto(out)
					})
			}
		}
		for vpc := range getGrpVPCs(ag.GroupMembers) {
			ch := make(chan error)
			grpID := &securitygroup.CloudResourceID{Name: addrGrpIDs[ag.Name].Name, Vpc: vpc}
			createCall := mockCloudSecurityAPI.EXPECT().CreateSecurityGroup(
				grpID, true).Times(sgConfig.sgCreateTimes).
				Return(ch)
			go func(ret chan error) {
				ret <- sgConfig.sgCreateError
			}(ch)
			ch = make(chan error)
			mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupMembers(
				grpID, getGrpMembers(ag.GroupMembers), true).Return(ch).After(createCall).Times(sgConfig.addrSgMemberTimes)
			go func(ret chan error) {
				ret <- sgConfig.sgMemberError
			}(ch)
		}
	}

	// The sequence of IPs in IngressRule/EgressRule might change during rule deduplication.
	// Sort it for equality check.
	sortRuleIPs := func(inputIP []*net.IPNet) []*net.IPNet {
		ipMap := make(map[string]*net.IPNet)
		ipStrList := make([]string, 0)
		for _, ip := range inputIP {
			ipStrList = append(ipStrList, ip.String())
			ipMap[ip.String()] = ip
		}
		sort.Strings(ipStrList)
		outputIP := make([]*net.IPNet, 0)
		for _, ipStr := range ipStrList {
			outputIP = append(outputIP, ipMap[ipStr])
		}
		return outputIP
	}

	sortIngressRuleIPs := func(inRule *securitygroup.IngressRule) *securitygroup.IngressRule {
		ret := deepcopy.Copy(inRule).(*securitygroup.IngressRule)
		ret.FromSrcIP = sortRuleIPs(ret.FromSrcIP)
		return ret
	}

	sortEgressRuleIPs := func(eRule *securitygroup.EgressRule) *securitygroup.EgressRule {
		ret := deepcopy.Copy(eRule).(*securitygroup.EgressRule)
		ret.ToDstIP = sortRuleIPs(ret.ToDstIP)
		return ret
	}

	checkAppliedGroup := func(ag *antreanetworking.AppliedToGroup) {
		for _, ee := range getGrpExternalEntity(ag.GroupMembers) {
			key := client.ObjectKey{Name: ee.Name, Namespace: ee.Namespace}
			mockClient.EXPECT().Get(mock.Any(), key, mock.Any()).
				Return(sgConfig.k8sGetError).MaxTimes(1).
				Do(func(_ context.Context, key client.ObjectKey, out *antreatypes.ExternalEntity) {
					ee.DeepCopyInto(out)
				})
			if len(ee.OwnerReferences) != 0 {
				owner := ee.OwnerReferences[0]
				key := client.ObjectKey{Name: owner.Name, Namespace: ee.Namespace}
				mockClient.EXPECT().Get(mock.Any(), key, mock.Any()).
					Return(nil).AnyTimes().
					Do(func(_ context.Context, key client.ObjectKey, out *cloud.VirtualMachine) {
						vm := vmNameToVirtualMachine[owner.Name]
						vm.DeepCopyInto(out)
					})
			}

		}
		for vpc := range getGrpVPCs(ag.GroupMembers) {
			ch := make(chan error)
			grpID := &securitygroup.CloudResourceID{Name: appliedToGrpIDs[ag.Name].Name, Vpc: vpc}
			createCall := mockCloudSecurityAPI.EXPECT().CreateSecurityGroup(
				grpID, false).Times(sgConfig.sgCreateTimes).
				Return(ch)
			go func(ret chan error) {
				ret <- sgConfig.sgCreateError
			}(ch)
			ch = make(chan error)
			var ruleCall *mock.Call
			if sgConfig.sgRuleNoOrder {
				ruleCall = mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupRules(
					grpID, mock.Any(), mock.Any()).
					Return(ch).After(createCall).MaxTimes(sgConfig.appSgRuleTimes).
					Do(func(id *securitygroup.CloudResourceID, in []*securitygroup.IngressRule, eg []*securitygroup.EgressRule) {
						Expect(len(in)).To(Equal(1))
						Expect(len(eg)).To(Equal(1))
						Expect(sortIngressRuleIPs(in[0])).To(Equal(sortIngressRuleIPs(ingressRule)))
						Expect(sortEgressRuleIPs(eg[0])).To(Equal(sortEgressRuleIPs(egressRule)))
					})
			} else {
				ruleCall = mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupRules(
					grpID, mock.Any(), mock.Any()).
					Return(ch).After(createCall).Times(sgConfig.appSgRuleTimes).
					Do(func(id *securitygroup.CloudResourceID, in []*securitygroup.IngressRule, eg []*securitygroup.EgressRule) {
						Expect(len(in)).To(Equal(1))
						Expect(len(eg)).To(Equal(1))
						Expect(sortIngressRuleIPs(in[0])).To(Equal(sortIngressRuleIPs(ingressRule)))
						Expect(sortEgressRuleIPs(eg[0])).To(Equal(sortEgressRuleIPs(egressRule)))
					})
			}
			go func(ret chan error) {
				ret <- sgConfig.sgRuleError
			}(ch)
			ch = make(chan error)
			if sgConfig.sgRuleNoOrder {
				mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupMembers(
					grpID, getGrpMembers(ag.GroupMembers), false).Return(ch).Times(sgConfig.appSgMemberTimes)
			} else {
				mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupMembers(
					grpID, getGrpMembers(ag.GroupMembers), false).Return(ch).Times(sgConfig.appSgMemberTimes).After(ruleCall)
			}
			go func(ret chan error) {
				ret <- sgConfig.sgMemberError
			}(ch)
		}
	}

	checkAddrGroupDel := func(ag *antreanetworking.AddressGroup) []chan error {
		var chans []chan error
		for vpc := range getGrpVPCs(ag.GroupMembers) {
			ch := make(chan error)
			grpID := &securitygroup.CloudResourceID{Name: addrGrpIDs[ag.Name].Name, Vpc: vpc}
			mockCloudSecurityAPI.EXPECT().DeleteSecurityGroup(grpID, true).Times(sgConfig.sgDeleteTimes).
				Return(ch)
			if !sgConfig.sgDeletePending {
				go func(ret chan error) {
					ret <- sgConfig.sgDeleteError
				}(ch)
			} else {
				chans = append(chans, ch)
			}
		}
		return chans
	}

	checkAppliedGroupDel := func(ag *antreanetworking.AppliedToGroup, outOrder bool) []chan error {
		var chans []chan error
		for vpc := range getGrpVPCs(ag.GroupMembers) {
			grpID := &securitygroup.CloudResourceID{Name: appliedToGrpIDs[ag.Name].Name, Vpc: vpc}
			var ch chan error
			if outOrder {
				ch = make(chan error)
				memberCall := mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupMembers(
					grpID, nil, false).Return(ch)
				go func(ret chan error) {
					ret <- nil
				}(ch)
				ch = make(chan error)
				mockCloudSecurityAPI.EXPECT().DeleteSecurityGroup(grpID, false).Return(ch).After(memberCall)
				if !sgConfig.sgDeletePending {
					go func(ret chan error) {
						ret <- sgConfig.sgDeleteError
					}(ch)
				} else {
					chans = append(chans, ch)
				}
			} else {
				ch = make(chan error)
				mockCloudSecurityAPI.EXPECT().DeleteSecurityGroup(grpID, false).Return(ch).Times(sgConfig.sgDeleteTimes)
				if !sgConfig.sgDeletePending {
					go func(ret chan error) {
						ret <- sgConfig.sgDeleteError
					}(ch)
				} else {
					chans = append(chans, ch)
				}
			}
		}
		return chans
	}

	checkGrpPatchChange := func(grpName string, members []antreanetworking.GroupMember,
		apgs []*antreanetworking.AppliedToGroup, memberChange bool, oldMembers []*antreatypes.ExternalEntity,
		membershipOnly bool) {
		eelist := make([]*antreatypes.ExternalEntity, 0)
		if members != nil {
			eelist = append(eelist, getGrpExternalEntity(members)...)
		}
		eelist = append(eelist, oldMembers...)
		for _, ref := range eelist {
			ee := &antreatypes.ExternalEntity{}
			ref.DeepCopyInto(ee)
			key := client.ObjectKey{Name: ee.Name, Namespace: ee.Namespace}
			mockClient.EXPECT().Get(mock.Any(), key, mock.Any()).
				Return(nil).AnyTimes(). // because unchanged member does not Get.
				Do(func(_ context.Context, key client.ObjectKey, out *antreatypes.ExternalEntity) {
					ee.DeepCopyInto(out)
				})
			if len(ee.OwnerReferences) != 0 {
				owner := ee.OwnerReferences[0]
				key := client.ObjectKey{Name: owner.Name, Namespace: ee.Namespace}
				mockClient.EXPECT().Get(mock.Any(), key, mock.Any()).
					Return(nil).AnyTimes().
					Do(func(_ context.Context, key client.ObjectKey, out *cloud.VirtualMachine) {
						vm := vmNameToVirtualMachine[owner.Name]
						vm.DeepCopyInto(out)
					})
			}

		}
		if memberChange {
			for vpc := range getGrpVPCs(members) {
				ch := make(chan error)
				grpID := &securitygroup.CloudResourceID{Name: grpName, Vpc: vpc}
				mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupMembers(
					grpID, getGrpMembers(members), membershipOnly).Return(ch)
				go func(ret chan error) {
					ret <- nil
				}(ch)
			}
		}
		for _, ag := range apgs {
			for vpc := range getGrpVPCs(ag.GroupMembers) {
				ch := make(chan error)
				grpID := &securitygroup.CloudResourceID{Name: appliedToGrpIDs[ag.Name].Name, Vpc: vpc}
				mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupRules(
					grpID, mock.Any(), mock.Any()).
					Return(ch).
					Do(func(_ *securitygroup.CloudResourceID, in []*securitygroup.IngressRule, eg []*securitygroup.EgressRule) {
						Expect(len(in)).To(Equal(1))
						Expect(len(eg)).To(Equal(1))
						Expect(sortIngressRuleIPs(in[0])).To(Equal(sortIngressRuleIPs(ingressRule)))
						Expect(sortEgressRuleIPs(eg[0])).To(Equal(sortEgressRuleIPs(egressRule)))
					})
				go func(ret chan error) {
					ret <- nil
				}(ch)
			}
		}
	}

	checkNPPatchChange := func(apgs []*antreanetworking.AppliedToGroup) {
		for _, ag := range apgs {
			for vpc := range getGrpVPCs(ag.GroupMembers) {
				ch := make(chan error)
				grpID := &securitygroup.CloudResourceID{Name: appliedToGrpIDs[ag.Name].Name, Vpc: vpc}
				mockCloudSecurityAPI.EXPECT().UpdateSecurityGroupRules(
					grpID, mock.Any(), mock.Any()).
					Return(ch).
					Do(func(_ *securitygroup.CloudResourceID, in []*securitygroup.IngressRule, eg []*securitygroup.EgressRule) {
						Expect(len(in)).To(Equal(1))
						Expect(len(eg)).To(Equal(1))
						Expect(sortIngressRuleIPs(in[0])).To(Equal(sortIngressRuleIPs(ingressRule)))
						Expect(sortEgressRuleIPs(eg[0])).To(Equal(sortEgressRuleIPs(egressRule)))
					})
				go func(ret chan error) {
					ret <- nil
				}(ch)
			}
		}
	}

	wait := func() {
		var err error
		for done := false; !done; {
			select {
			case status := <-reconciler.cloudResponse:
				err = reconciler.processCloudResponse(status)
				Expect(err).ToNot(HaveOccurred())
			case <-time.After(time.Millisecond * 200):
				done = true
			}
		}
	}

	patchAddrGrpMember := func(ag *antreanetworking.AddressGroup,
		add, remove *antreatypes.ExternalEntity, rmIdx int) *antreanetworking.AddressGroupPatch {
		p1 := &antreanetworking.AddressGroupPatch{}
		p1.Name = ag.Name
		if add != nil {
			addRef := &antreanetworking.ExternalEntityReference{Name: add.Name, Namespace: add.Namespace}
			p1.AddedGroupMembers = []antreanetworking.GroupMember{{ExternalEntity: addRef}}
			ag.GroupMembers = append(ag.GroupMembers, antreanetworking.GroupMember{ExternalEntity: addRef})
		}
		if remove != nil {
			removeRef := &antreanetworking.ExternalEntityReference{Name: remove.Name, Namespace: remove.Namespace}
			p1.RemovedGroupMembers = []antreanetworking.GroupMember{{ExternalEntity: removeRef}}
			ag.GroupMembers[rmIdx] = ag.GroupMembers[len(ag.GroupMembers)-1]
			ag.GroupMembers = ag.GroupMembers[:len(ag.GroupMembers)-1]
		}
		return p1
	}

	patchAppliedToGrpMember := func(ag *antreanetworking.AppliedToGroup,
		add, remove *antreatypes.ExternalEntity, rmIdx int) *antreanetworking.AppliedToGroupPatch {
		p1 := &antreanetworking.AppliedToGroupPatch{}
		p1.Name = ag.Name
		if add != nil {
			addRef := &antreanetworking.ExternalEntityReference{Name: add.Name, Namespace: add.Namespace}
			p1.AddedGroupMembers = []antreanetworking.GroupMember{{ExternalEntity: addRef}}
			ag.GroupMembers = append(ag.GroupMembers, antreanetworking.GroupMember{ExternalEntity: addRef})
		}
		if remove != nil {
			removeRef := &antreanetworking.ExternalEntityReference{Name: remove.Name, Namespace: remove.Namespace}
			p1.RemovedGroupMembers = []antreanetworking.GroupMember{{ExternalEntity: removeRef}}
			ag.GroupMembers[rmIdx] = ag.GroupMembers[len(ag.GroupMembers)-1]
			ag.GroupMembers = ag.GroupMembers[:len(ag.GroupMembers)-1]
		}
		return p1
	}

	verifyCreateNP := func() {
		for _, ref := range addrGrps {
			ag := &antreanetworking.AddressGroup{}
			ref.DeepCopyInto(ag)
			checkAddrGroup(ag)
		}
		for _, ref := range appliedToGrps {
			ag := &antreanetworking.AppliedToGroup{}
			ref.DeepCopyInto(ag)
			checkAppliedGroup(ag)
		}
	}

	createNP := func(outOrder bool) {
		var event watch.Event
		var err error
		if outOrder {
			event = watch.Event{Type: watch.Added, Object: anp}
			err = reconciler.processNetworkPolicy(event)
			Expect(err).ToNot(HaveOccurred())
		}
		for _, grp := range addrGrps {
			event = watch.Event{Type: watch.Added, Object: grp}
			err = reconciler.processAddrGrp(event)
			if sgConfig.k8sGetError != nil {
				Expect(err).To(Equal(sgConfig.k8sGetError))
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		}
		for _, grp := range appliedToGrps {
			event = watch.Event{Type: watch.Added, Object: grp}
			err = reconciler.processAppliedToGrp(event)
			if sgConfig.k8sGetError != nil {
				Expect(err).To(Equal(sgConfig.k8sGetError))
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		}
		if !outOrder {
			event = watch.Event{Type: watch.Added, Object: anp}
			err = reconciler.processNetworkPolicy(event)
			Expect(err).ToNot(HaveOccurred())
		}

		// 1 networkPolicy
		// 2 AddrGroup, each with vpc and no vpc.
		// 2 AppliedToGroup with vpc
		nNP := 1
		nAddrGrp := len(anp.Rules)
		nAppGrp := len(anp.AppliedToGroups)
		if sgConfig.sgDeletePending || sgConfig.k8sGetError != nil {
			nNP = 1
			nAddrGrp = 0
			nAppGrp = 0
		}
		Expect(len(reconciler.networkPolicyIndexer.ListKeys())).To(Equal(nNP))
		Expect(len(reconciler.addrSGIndexer.ListKeys())).To(Equal(nAddrGrp))
		Expect(len(reconciler.appliedToSGIndexer.ListKeys())).To(Equal(nAppGrp))
		if !(sgConfig.sgDeletePending || sgConfig.k8sGetError != nil) {
			wait()
		}
	}

	createAndVerifyNP := func(outOrder bool) {
		verifyCreateNP()
		createNP(outOrder)
	}

	deleteNP := func(outOrder bool) {
		var event watch.Event
		var err error
		if outOrder {
			event = watch.Event{Type: watch.Deleted, Object: anp}
			err = reconciler.processNetworkPolicy(event)
			Expect(err).ToNot(HaveOccurred())
		}
		for _, grp := range addrGrps {
			event = watch.Event{Type: watch.Deleted, Object: grp}
			err = reconciler.processAddrGrp(event)
			Expect(err).ToNot(HaveOccurred())
		}
		for _, grp := range appliedToGrps {
			event = watch.Event{Type: watch.Deleted, Object: grp}
			err = reconciler.processAppliedToGrp(event)
			Expect(err).ToNot(HaveOccurred())
		}
		if !outOrder {
			event = watch.Event{Type: watch.Deleted, Object: anp}
			err = reconciler.processNetworkPolicy(event)
			Expect(err).ToNot(HaveOccurred())
		}

		Expect(len(reconciler.networkPolicyIndexer.ListKeys())).To(Equal(0))
		Expect(len(reconciler.addrSGIndexer.ListKeys())).To(Equal(0))
		Expect(len(reconciler.appliedToSGIndexer.ListKeys())).To(Equal(0))

		if !sgConfig.sgDeletePending {
			wait()
		}
	}

	verifyDeleteNP := func(outOrder bool) []chan error {
		var chans []chan error
		for _, ref := range addrGrps {
			ag := &antreanetworking.AddressGroup{}
			ref.DeepCopyInto(ag)
			chans = append(chans, checkAddrGroupDel(ag)...)
		}
		for _, ref := range appliedToGrps {
			ag := &antreanetworking.AppliedToGroup{}
			ref.DeepCopyInto(ag)
			chans = append(chans, checkAppliedGroupDel(ag, outOrder)...)
		}
		return chans
	}

	deleteAndVerifyNP := func(outOrder bool) []chan error {
		chans := verifyDeleteNP(outOrder)
		deleteNP(outOrder)
		return chans
	}

	verifyNPTracker := func(trackedVMs map[string]*cloud.VirtualMachine, hasTracker, hasError bool) {
		mockClient.EXPECT().List(mock.Any(), mock.Any(), mock.Any()).
			Do(func(_ context.Context, vmList *cloud.VirtualMachineList, opt client.MatchingFields) {
				if hasTracker {
					vmList.Items = append(vmList.Items, cloud.VirtualMachine{
						ObjectMeta: v1.ObjectMeta{
							Name:      opt[virtualMachineIndexerByCloudID],
							Namespace: namespace,
						},
					})
					trackedVMs[vmList.Items[0].Name] = &vmList.Items[0]
				} else {
					vmList.Items = append(vmList.Items, *trackedVMs[opt[virtualMachineIndexerByCloudID]])
				}
			}).Times(len(appliedToGrps))
		reconciler.processCloudResourceNPTrackers()
		wait()
		if hasTracker || hasError {
			Expect(len(reconciler.cloudResourceNPTrackerIndexer.List())).To(Equal(len(appliedToGrps)))
		} else {
			Expect(len(reconciler.cloudResourceNPTrackerIndexer.List())).To(Equal(0))
		}
	}

	verifyNPStatus := func(trackedVMs map[string]*cloud.VirtualMachine, hasPolicy, hasError bool) {
		for idx := len(addrGrpNames); idx < len(addrGrpNames)+len(appliedToGrpsNames); idx++ {
			vm := trackedVMs[vmNamePrefix+vmNames[idx]]
			obj, found, _ := reconciler.virtualMachinePolicyIndexer.GetByKey(types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}.String())
			if hasPolicy && !hasError {
				Expect(found).To(BeTrue())
				npStatus := obj.(*NetworkPolicyStatus)
				status, ok := npStatus.NPStatus[anp.Name]
				Expect(ok).To(BeTrue())
				Expect(status).To(Equal(NetworkPolicyStatusApplied))
			} else if !hasPolicy && hasError {
				Expect(found).To(BeTrue())
				npStatus := obj.(*NetworkPolicyStatus)
				Expect(len(npStatus.NPStatus)).To(Equal(1))
			} else if !hasPolicy && !hasError {
				Expect(found).To(BeFalse())
			}
		}
	}

	It("Test NetworkPolicy Indexer", func() {
		var nps []interface{}
		np := &networkPolicy{}
		anp.DeepCopyInto(&np.NetworkPolicy)
		err := reconciler.networkPolicyIndexer.Add(np)
		Expect(err).ToNot(HaveOccurred())
		for _, sg := range appliedToGrpsNames {
			nps, err = reconciler.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, sg)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(nps)).To(BeNumerically("==", 1))
		}
		nps, err = reconciler.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, "patch-grp")
		Expect(err).ToNot(HaveOccurred())
		Expect(len(nps)).To(BeNumerically("==", 0))

		nnp := deepcopy.Copy(np).(*networkPolicy)
		nnp.AppliedToGroups = []string{"patch-grp"}
		err = reconciler.networkPolicyIndexer.Update(nnp)
		Expect(err).ToNot(HaveOccurred())
		nps = reconciler.networkPolicyIndexer.List()
		Expect(len(nps)).To(BeNumerically("==", 1))

		for _, sg := range appliedToGrpsNames {
			nps, err = reconciler.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, sg)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(nps)).To(BeNumerically("==", 0))
		}
		nps, err = reconciler.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAppliedToGrp, "patch-grp")
		Expect(err).ToNot(HaveOccurred())
		Expect(len(nps)).To(BeNumerically("==", 1))
	})

	It("Create networkPolicy", func() {
		createAndVerifyNP(false)
	})

	It("Delete networkPolicy in order", func() {
		createAndVerifyNP(true)
		deleteAndVerifyNP(false)
	})

	It("Delete networkPolicy out order", func() {
		createAndVerifyNP(false)
		deleteAndVerifyNP(true)
	})

	It("Modify addrGroup cloud member", func() {
		createAndVerifyNP(false)
		add := vmExternalEntities[vmNames[patchVMIdx]]
		remove := vmExternalEntities[vmNames[0]]
		addrGrp := addrGrps[0]
		p1 := patchAddrGrpMember(addrGrp, add, remove, 0)
		checkGrpPatchChange(addrGrp.Name, addrGrp.GroupMembers, nil, true, []*antreatypes.ExternalEntity{remove}, true)
		var err error
		event := watch.Event{Type: watch.Modified, Object: p1}
		err = reconciler.processAddrGrp(event)
		Expect(err).ToNot(HaveOccurred())

		wait()
	})

	It("Modify appliedToGroup member", func() {
		createAndVerifyNP(false)
		add := vmExternalEntities[vmNames[patchVMIdx]]
		remove := vmExternalEntities[vmNames[2]]
		appliedToGrp := appliedToGrps[0]
		p1 := patchAppliedToGrpMember(appliedToGrp, add, remove, 0)
		checkGrpPatchChange(appliedToGrp.Name, appliedToGrp.GroupMembers, nil, true, []*antreatypes.ExternalEntity{remove}, false)
		var err error
		event := watch.Event{Type: watch.Modified, Object: p1}
		err = reconciler.processAppliedToGrp(event)
		Expect(err).ToNot(HaveOccurred())

		wait()
	})

	It("Modify networkPolicy address group cloud member", func() {
		createAndVerifyNP(false)

		ag := &antreanetworking.AddressGroup{}
		ag.Name = "ag-patch"
		efvm := &antreanetworking.ExternalEntityReference{Name: vmExternalEntities[vmNames[patchVMIdx]].Name, Namespace: namespace}
		ag.GroupMembers = []antreanetworking.GroupMember{{ExternalEntity: efvm}}
		addrGrpIDs[ag.Name] = &securitygroup.CloudResourceID{Name: ag.Name, Vpc: vpc}

		anp.Rules[0].From.AddressGroups = append(anp.Rules[0].From.AddressGroups, "ag-patch")

		var err error
		checkAddrGroup(ag)
		event := watch.Event{Type: watch.Added, Object: ag}
		err = reconciler.processAddrGrp(event)
		Expect(err).ToNot(HaveOccurred())

		ingressRule.FromSecurityGroups = append(ingressRule.FromSecurityGroups, addrGrpIDs[ag.Name])
		checkNPPatchChange(appliedToGrps)
		event = watch.Event{Type: watch.Modified, Object: anp}
		err = reconciler.processNetworkPolicy(event)
		Expect(err).ToNot(HaveOccurred())

		wait()
	})

	It("Modify networkPolicy appliedTo group", func() {
		createAndVerifyNP(false)
		ag := &antreanetworking.AppliedToGroup{}
		ag.Name = "ag-patch"
		efvm := &antreanetworking.ExternalEntityReference{Name: vmExternalEntities[vmNames[patchVMIdx]].Name, Namespace: namespace}
		ag.GroupMembers = []antreanetworking.GroupMember{{ExternalEntity: efvm}}
		appliedToGrpIDs[ag.Name] = &securitygroup.CloudResourceID{Name: ag.Name, Vpc: vpc}

		anp.AppliedToGroups = append(anp.AppliedToGroups, "ag-patch")

		var err error
		checkAppliedGroup(ag)
		event := watch.Event{Type: watch.Added, Object: ag}
		err = reconciler.processAppliedToGrp(event)
		Expect(err).ToNot(HaveOccurred())
		event = watch.Event{Type: watch.Modified, Object: anp}
		err = reconciler.processNetworkPolicy(event)
		Expect(err).ToNot(HaveOccurred())

		wait()
	})

	It("Tracking networkPolicy", func() {
		trackedVMs := make(map[string]*cloud.VirtualMachine)
		createAndVerifyNP(false)
		verifyNPTracker(trackedVMs, true, false)
		verifyNPStatus(trackedVMs, true, false)
		// return delete error
		sgConfig.sgDeleteError = fmt.Errorf("dummy")
		deleteAndVerifyNP(false)
		verifyNPTracker(trackedVMs, false, true)
		verifyNPStatus(trackedVMs, false, true)
		// retry without delete error
		sgConfig.sgDeleteError = nil
		verifyDeleteNP(false)
		reconciler.retryQueue.CheckToRun()
		wait()
		verifyNPTracker(trackedVMs, false, false)
		verifyNPStatus(trackedVMs, false, false)
	})

	It("Create NetworkPolicy groups after security group garbage collection", func() {
		createAndVerifyNP(false)
		sgConfig.sgDeletePending = true
		chans := deleteAndVerifyNP(false)
		for i := 0; i < 5; i++ {
			createNP(false)
			deleteNP(false)
		}
		createNP(false)
		go func() {
			for _, ch := range chans {
				ch <- nil
			}
		}()
		sgConfig.sgDeletePending = false
		verifyCreateNP()
		wait()
		Expect(len(reconciler.pendingDeleteGroups.items)).To(BeZero())
	})

	It("Create NetworkPolicy groups after security group garbage collection with error", func() {
		createAndVerifyNP(false)
		sgConfig.sgDeletePending = true
		chans := deleteAndVerifyNP(false)
		for i := 0; i < 5; i++ {
			createNP(false)
			deleteNP(false)
		}
		createNP(false)
		go func() {
			for _, ch := range chans {
				ch <- fmt.Errorf("dummy")
			}
		}()
		wait()
		Expect(len(reconciler.retryQueue.items)).To(Equal(len(anp.Rules) + len(anp.AppliedToGroups)))
		sgConfig.sgDeletePending = false
		verifyDeleteNP(false)
		reconciler.retryQueue.CheckToRun()
		verifyCreateNP()
		wait()
		Expect(len(reconciler.retryQueue.items)).To(BeZero())
		Expect(len(reconciler.pendingDeleteGroups.items)).To(BeZero())
	})

	var (
		opSgConfig = map[string][]securityGroupConfig{
			"K8sGet": {
				{
					k8sGetError: fmt.Errorf("dummy"),
				},
				{
					k8sGetError: fmt.Errorf("dummy"),
				},
				{
					addrSgMemberTimes: 1,
					appSgMemberTimes:  1,
					appSgRuleTimes:    1,
					sgCreateTimes:     1,
				},
			},
			securityGroupOperationAdd.String(): {
				{
					sgCreateError: fmt.Errorf("dummy"),
					sgCreateTimes: 1,
				},
				{
					sgCreateError: fmt.Errorf("dummy"),
					sgCreateTimes: 1,
				},
				{
					sgRuleNoOrder:     true,
					addrSgMemberTimes: 2,
					appSgMemberTimes:  1,
					appSgRuleTimes:    2,
					sgCreateTimes:     1,
				},
			},
			securityGroupOperationUpdateMembers.String(): {
				{
					sgMemberError:     fmt.Errorf("dummy"),
					addrSgMemberTimes: 1,
					appSgMemberTimes:  1,
					appSgRuleTimes:    1,
					sgCreateTimes:     1,
				},
				{
					sgMemberError:     fmt.Errorf("dummy"),
					addrSgMemberTimes: 1,
					appSgMemberTimes:  1,
				},
				{
					sgRuleNoOrder:     true,
					addrSgMemberTimes: 2,
					appSgMemberTimes:  2,
					appSgRuleTimes:    1,
					sgDeleteTimes:     1,
				},
			},
			securityGroupOperationUpdateRules.String(): {
				{
					sgRuleError:       fmt.Errorf("dummy"),
					addrSgMemberTimes: 1,
					appSgRuleTimes:    1,
					sgCreateTimes:     1,
				},
				{
					sgRuleError:    fmt.Errorf("dummy"),
					appSgRuleTimes: 1,
				},
				{
					sgRuleNoOrder:    true,
					appSgMemberTimes: 1,
					appSgRuleTimes:   2,
					sgDeleteTimes:    1,
				},
			},
			securityGroupOperationDelete.String(): {
				{
					sgDeleteError: fmt.Errorf("dummy"),
					sgDeleteTimes: 1,
				},
				{
					sgDeleteError: fmt.Errorf("dummy"),
					sgDeleteTimes: 1,
				},
				{sgDeleteTimes: 1},
			},
		}
	)
	table.DescribeTable("NetworkPolicy groups operation failures",
		func(op string, retries int) {
			if op == "K8sGet" {
				// Use single member per SG to avoid expectation ambiguity.
				for _, ag := range addrGrps {
					ag.GroupMembers = ag.GroupMembers[:1]
				}
				ingressRule.FromSrcIP = ingressRule.FromSrcIP[:1]
				egressRule.ToDstIP = egressRule.ToDstIP[:1]
			}
			itemCnt := len(anp.Rules) + len(anp.AppliedToGroups)
			if op == securityGroupOperationUpdateRules.String() {
				itemCnt = len(anp.AppliedToGroups)
			}
			sgConfig = opSgConfig[op][0]
			createAndVerifyNP(false)
			wait()
			sgConfig = opSgConfig[op][1]
			for i := 0; i < retries-1; i++ {
				if i < operationCount {
					Expect(len(reconciler.retryQueue.items)).To(Equal(itemCnt))
					verifyCreateNP()
				}
				reconciler.retryQueue.CheckToRun()
				wait()
			}
			if retries <= operationCount {
				sgConfig = opSgConfig[op][2]
				Expect(len(reconciler.retryQueue.items)).To(Equal(itemCnt))
				verifyCreateNP()
				reconciler.retryQueue.CheckToRun()
				wait()
			}
			Expect(len(reconciler.retryQueue.items)).To(BeZero())
		},
		table.Entry("K8sGet failure count 1", "K8sGet", 1),
		table.Entry("K8sGet failure count 3", "K8sGet", 3),
		table.Entry("K8sGet failure count exceeds limits", "K8sGet", operationCount+2),
		table.Entry("Create failure count 1", securityGroupOperationAdd.String(), 1),
		table.Entry("Create failure count 3", securityGroupOperationAdd.String(), 3),
		table.Entry("Create failure count exceeds limits", securityGroupOperationAdd.String(), operationCount+2),
		table.Entry("Update failure count 1", securityGroupOperationUpdateMembers.String(), 1),
		table.Entry("Update failure count 3", securityGroupOperationUpdateMembers.String(), 3),
		table.Entry("Update failure count exceeds limits", securityGroupOperationUpdateMembers.String(), operationCount+2),
		table.Entry("Update rule failure count 1", securityGroupOperationUpdateRules.String(), 1),
		table.Entry("Update rule failure count 3", securityGroupOperationUpdateRules.String(), 3),
		table.Entry("Update rule failure count exceeds limits", securityGroupOperationUpdateRules.String(), operationCount+2),
	)

	table.DescribeTable("NetworkPolicy groups delete operation failures",
		func(op string, retries int) {
			itemCnt := len(anp.Rules) + len(anp.AppliedToGroups)
			createAndVerifyNP(false)
			sgConfig = opSgConfig[op][0]
			deleteAndVerifyNP(false)
			sgConfig = opSgConfig[op][1]
			for i := 0; i < retries-1; i++ {
				if i < operationCount {
					Expect(len(reconciler.retryQueue.items)).To(Equal(itemCnt))
					verifyDeleteNP(false)
				}
				reconciler.retryQueue.CheckToRun()
				wait()
			}
			if retries <= operationCount {
				sgConfig = opSgConfig[op][2]
				Expect(len(reconciler.retryQueue.items)).To(Equal(itemCnt))
				verifyDeleteNP(false)
				reconciler.retryQueue.CheckToRun()
				wait()
			}
			Expect(len(reconciler.retryQueue.items)).To(BeZero())
		},
		table.Entry("Delete failure count 1", securityGroupOperationDelete.String(), 1),
		table.Entry("Delete failure count 3", securityGroupOperationDelete.String(), 3),
		table.Entry("Delete failure count exceeds limits", securityGroupOperationDelete.String(), operationCount+2),
	)

	table.DescribeTable("NetworkPolicy group deletion cancels retrying operation",
		func(op string) {
			if op == "K8sGet" {
				// Use single member per SG to avoid expectation ambiguity.
				for _, ag := range addrGrps {
					ag.GroupMembers = ag.GroupMembers[:1]
				}
				ingressRule.FromSrcIP = ingressRule.FromSrcIP[:1]
				egressRule.ToDstIP = egressRule.ToDstIP[:1]
			}
			sgConfig = opSgConfig[op][0]
			createAndVerifyNP(false)
			Expect(len(reconciler.networkPolicyIndexer.ListKeys())).To(Equal(1))
			sgConfig = opSgConfig[op][2]
			deleteAndVerifyNP(false)
			Expect(len(reconciler.retryQueue.items)).To(BeZero())
			Expect(len(reconciler.appliedToSGIndexer.ListKeys())).To(BeZero())
			Expect(len(reconciler.addrSGIndexer.ListKeys())).To(BeZero())
			Expect(len(reconciler.networkPolicyIndexer.ListKeys())).To(BeZero())
		},
		table.Entry("K8sGet", "K8sGet"),
		table.Entry("Create", securityGroupOperationAdd.String()),
		table.Entry("Update", securityGroupOperationUpdateMembers.String()),
		table.Entry("Update rule", securityGroupOperationUpdateRules.String()),
	)

	const (
		cloudReturnSameSG = iota
		cloudReturnNoSG
		cloudReturnExtraSG
		cloudReturnDiffMemberSG
		cloudReturnDiffRuleSG
	)

	var (
		cloudSgConfig = map[int]securityGroupConfig{
			cloudReturnSameSG: {
				addrSgMemberTimes: 0,
				appSgMemberTimes:  0,
				appSgRuleTimes:    0,
				sgCreateTimes:     0,
			},
			cloudReturnNoSG: {
				addrSgMemberTimes: 1,
				appSgMemberTimes:  1,
				appSgRuleTimes:    1,
				sgCreateTimes:     1,
			},
			cloudReturnDiffMemberSG: {
				addrSgMemberTimes: 1,
				appSgMemberTimes:  0,
				appSgRuleTimes:    0,
				sgCreateTimes:     0,
			},
			cloudReturnDiffRuleSG: {
				addrSgMemberTimes: 0,
				appSgMemberTimes:  0,
				appSgRuleTimes:    1,
				sgCreateTimes:     0,
			},
			cloudReturnExtraSG: {
				addrSgMemberTimes: 0,
				appSgMemberTimes:  0,
				appSgRuleTimes:    0,
				sgCreateTimes:     0,
			},
		}
	)

	table.DescribeTable("NetworkPolicy synchronize with cloud",
		func(cloudRet int) {
			extraSG := securitygroup.SynchronizationContent{
				Resource:       securitygroup.CloudResourceID{Name: "Extra", Vpc: vpc},
				MembershipOnly: true,
			}
			if cloudRet == cloudReturnDiffMemberSG {
				for i := 0; i < len(addrGrpNames); i++ {
					syncContents[i].Members = append(syncContents[0].Members,
						securitygroup.CloudResource{Type: securitygroup.CloudResourceTypeVM,
							Name: securitygroup.CloudResourceID{Name: vmNameToIDMap[vmNames[patchVMIdx]], Vpc: vpc}},
					)
				}
			} else if cloudRet == cloudReturnDiffRuleSG {
				for i := len(addrGrpNames); i < len(addrGrpNames)+len(appliedToGrpsNames); i++ {
					syncContents[i].IngressRules[0].FromPort = nil
				}
			} else if cloudRet == cloudReturnExtraSG {
				syncContents = append(syncContents, extraSG)
			}
			ch := make(chan securitygroup.SynchronizationContent)
			mockCloudSecurityAPI.EXPECT().GetSecurityGroupSyncChan().Return(ch)
			go func() {
				if cloudRet != cloudReturnNoSG {
					for _, c := range syncContents {
						ch <- c
					}
				}
				close(ch)
			}()
			reconciler.syncedWithCloud = false
			sgConfig = cloudSgConfig[cloudRet]
			createAndVerifyNP(true)
			if cloudRet == cloudReturnExtraSG {
				ch := make(chan error)
				mockCloudSecurityAPI.EXPECT().DeleteSecurityGroup(&extraSG.Resource, true).Return(ch)
				go func() {
					ch <- nil
				}()
			}
			reconciler.bookmarkCnt = npSyncReadyBookMarkCnt
			reconciler.syncWithCloud()
			wait()
		},
		table.Entry("Cloud has no security group", cloudReturnNoSG),
		table.Entry("Cloud has matching security group", cloudReturnSameSG),
		table.Entry("Cloud has mismatch security group member", cloudReturnDiffMemberSG),
		table.Entry("Cloud has mismatch security group rule", cloudReturnDiffRuleSG),
		table.Entry("Cloud has extra security group", cloudReturnExtraSG),
	)
})
