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
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreav1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	antreanetworkingclient "antrea.io/antrea/pkg/client/clientset/versioned/typed/controlplane/v1beta2"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/pkg/controllers/inventory"
)

const (
	NetworkPolicyStatusIndexerByNamespace       = "namespace"
	addrAppliedToIndexerByGroupID               = "GroupID"
	appliedToIndexerByAddrGroupRef              = "AddressGrp"
	networkPolicyIndexerByAddrGrp               = "AddressGrp"
	networkPolicyIndexerByAppliedToGrp          = "AppliedToGrp"
	cloudResourceNPTrackerIndexerByAppliedToGrp = "AppliedToGrp"
	cloudRuleIndexerByAppliedToGrp              = "AppliedToGrp"

	operationCount = 15

	cloudResponseChBuffer = 50

	// NetworkPolicy controller is ready to sync after it receives bookmarks from
	// networkpolicy, addrssGroup and appliedToGroup.
	npSyncReadyBookMarkCnt = 3
)

// +kubebuilder:rbac:groups=controlplane.antrea.io,resources=networkpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.antrea.io,resources=addressgroups,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.antrea.io,resources=appliedtogroups,verbs=get;list;watch

type NetworkPolicyController interface {
	LocalEvent(watch.Event)
}

// NetworkPolicyReconciler reconciles a NetworkPolicy object.
type NetworkPolicyReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	antreaClient antreanetworkingclient.ControlplaneV1beta2Interface

	// ExternalEntity Reconcile
	pendingSyncCount int
	initialized      bool

	// Watcher interfaces
	addrGroupWatcher      watch.Interface
	appliedToGroupWatcher watch.Interface
	networkPolicyWatcher  watch.Interface

	// Indexers
	networkPolicyIndexer          cache.Indexer
	addrSGIndexer                 cache.Indexer
	appliedToSGIndexer            cache.Indexer
	cloudResourceNPTrackerIndexer cache.Indexer
	virtualMachinePolicyIndexer   cache.Indexer
	cloudRuleIndexer              cache.Indexer

	Inventory inventory.Interface

	// pendingDeleteGroups keep tracks of deleting AddressGroup or AppliedToGroup.
	pendingDeleteGroups *PendingItemQueue
	retryQueue          *PendingItemQueue

	// cloudResponse receives responses from cloud operations.
	cloudResponse chan *securityGroupStatus

	// syncedWithCloud is true if controller has synchronized with cloud at least once.
	syncedWithCloud bool

	// CloudSyncInterval specifies the interval (in seconds) to be used for syncing cloud resources with controller.
	CloudSyncInterval int64

	// Bookmark events received prior to sync with the cloud.
	bookmarkCnt int

	// localRequest sends and receives network policy requests from local stack.
	localRequest chan watch.Event
}

// isNetworkPolicySupported check if network policy is supported.
func (r *NetworkPolicyReconciler) isNetworkPolicySupported(anp *antreanetworking.NetworkPolicy) error {
	if anp.SourceRef == nil {
		return fmt.Errorf("source reference not set in network policy")
	}
	if anp.SourceRef.Type != antreanetworking.AntreaNetworkPolicy {
		return fmt.Errorf("only antrea network policy is supported")
	}
	// Check for support actions.
	for _, rule := range anp.Rules {
		if rule.Action != nil && *rule.Action != antreav1alpha1.RuleActionAllow {
			return fmt.Errorf("only Allow action is supported in antrea network policy")
		}
		// check for supported protocol.
		for _, s := range rule.Services {
			if _, ok := AntreaProtocolMap[*s.Protocol]; !ok {
				return fmt.Errorf("unsupported protocol %v, only %v protocols are supported",
					*s.Protocol, reflect.ValueOf(AntreaProtocolMap).MapKeys())
			}
		}
	}
	return nil
}

// updateRuleRealizationStatus checks rule realization status on all appliedTo groups for a np and send status.
func (r *NetworkPolicyReconciler) updateRuleRealizationStatus(currentSgID string, np *networkPolicy, err error) {
	if err != nil {
		r.sendRuleRealizationStatus(&np.NetworkPolicy, err)
		return
	}

	// current sg update rule success, check other sg rule realization status.
	for _, at := range np.AppliedToGroups {
		sgs, e := r.appliedToSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, at)
		if e != nil {
			r.Log.Error(e, "get appliedToSG indexer", "sg", at)
			return
		}
		for _, obj := range sgs {
			sg := obj.(*appliedToSecurityGroup)
			if sg.id.CloudResourceID.String() == currentSgID {
				continue
			}
			// let other sgs handle their update status.
			e = sg.checkRealization(r, np)
			if e != nil {
				return
			}
		}
	}
	r.sendRuleRealizationStatus(&np.NetworkPolicy, nil)
}

// sendRuleRealizationStatus sends anp realization status to antrea controller.
func (r *NetworkPolicyReconciler) sendRuleRealizationStatus(anp *antreanetworking.NetworkPolicy, err error) {
	status := &antreanetworking.NetworkPolicyStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      string(anp.UID),
			Namespace: anp.Namespace,
		},

		Nodes: []antreanetworking.NetworkPolicyNodeStatus{
			{
				NodeName:   config.ANPNepheController,
				Generation: anp.Generation,
			},
		},
	}
	if err != nil {
		status.Nodes[0].RealizationFailure = true
		status.Nodes[0].Message = err.Error()
	}

	go func() {
		r.Log.V(1).Info("Updating rule realization.", "NP", anp.Name, "Namespace", anp.Namespace, "err", err)
		if e := r.antreaClient.NetworkPolicies().UpdateStatus(context.TODO(), status.Name, status); e != nil {
			r.Log.Error(e, "rule realization send failed.", "NP", anp.Name, "Namespace", anp.Namespace)
		}
	}()
}

// normalizedANPObject updates ANP object with Nephe friendly name. Required for Azure
// cloud which doesn't handles / in any cloud resource name.
func (r *NetworkPolicyReconciler) normalizedANPObject(anp *antreanetworking.NetworkPolicy) {
	for i, appliedTo := range anp.AppliedToGroups {
		anp.AppliedToGroups[i] = getNormalizedName(appliedTo)
	}
	for i, rule := range anp.Rules {
		for j, addrGroup := range rule.From.AddressGroups {
			anp.Rules[i].From.AddressGroups[j] = getNormalizedName(addrGroup)
		}
		for j, addrGroup := range rule.To.AddressGroups {
			anp.Rules[i].To.AddressGroups[j] = getNormalizedName(addrGroup)
		}
	}
}

// processGroup is common function to process AppliedTo/Address Group updates from Antrea controller.
func (r *NetworkPolicyReconciler) processGroup(groupName string, eventType watch.EventType, isAddrGrp bool,
	added, removed []antreanetworking.GroupMember) error {
	if r.processBookMark(eventType) {
		return nil
	}
	// Address and AppliedTo Group can have the same name received. uGroupName will prefix mm_ for Address Group.
	uGroupName := getGroupUniqueName(groupName, isAddrGrp)
	var err error
	defer func() {
		if err != nil && (eventType == watch.Added || eventType == watch.Modified) {
			if !r.retryQueue.Has(uGroupName) {
				r.retryQueue.Add(uGroupName, &pendingGroup{})
			}
			_ = r.retryQueue.Update(uGroupName, false, eventType, added, removed)
		}
		if eventType == watch.Deleted {
			r.retryQueue.Remove(uGroupName)
		}
	}()

	if securitygroup.CloudSecurityGroup == nil {
		r.Log.V(1).Info("Skip group message no plug-in found", "group", groupName, "membershipOnly", isAddrGrp)
		return nil
	}

	if r.pendingDeleteGroups.Has(uGroupName) {
		_ = r.pendingDeleteGroups.Update(uGroupName, false, eventType, added, removed)
		r.Log.V(1).Info("Wait for group pending delete to complete", "Name", groupName, "membershipOnly", isAddrGrp)
		return nil
	}

	var indexer cache.Indexer
	var creatorFunc func(*securitygroup.CloudResource, interface{}, *securityGroupState) cloudSecurityGroup
	if isAddrGrp {
		indexer = r.addrSGIndexer
		creatorFunc = newAddrSecurityGroup
	} else {
		indexer = r.appliedToSGIndexer
		creatorFunc = newAppliedToSecurityGroup
	}
	var addedMembers, removedMembers map[string][]*securitygroup.CloudResource
	var notFoundMember []*types.NamespacedName
	if eventType == watch.Added {
		if addedMembers, notFoundMember, err = vpcsFromGroupMembers(added, r); err != nil {
			return err
		}
		if len(notFoundMember) > 0 {
			err = fmt.Errorf("missing externalEntities: %v", notFoundMember)
			return err
		}
	} else if eventType == watch.Modified {
		if addedMembers, notFoundMember, err = vpcsFromGroupMembers(added, r); err != nil {
			return err
		}
		if len(notFoundMember) > 0 {
			err = fmt.Errorf("missing externalEntities: %v", notFoundMember)
			return err
		}
		if removedMembers, notFoundMember, err = vpcsFromGroupMembers(removed, r); err != nil {
			return err
		}
		if len(notFoundMember) > 0 {
			sgs, _ := r.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, groupName)
			for _, i := range sgs {
				i.(*addrSecurityGroup).removeStaleMembers(notFoundMember, r)
			}
			sgs1, _ := r.appliedToSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, groupName)
			for _, i := range sgs1 {
				i.(*appliedToSecurityGroup).removeStaleMembers(notFoundMember, r)
			}
		}
	} else if eventType == watch.Deleted {
		sgs, err := indexer.ByIndex(addrAppliedToIndexerByGroupID, groupName)
		if err != nil {
			return err
		}
		for _, i := range sgs {
			sg := i.(cloudSecurityGroup)
			if err := sg.delete(r); err != nil {
				r.Log.Error(err, "delete SecurityGroup on cloud", "key", sg.getID())
			}
		}
		return nil
	} else {
		r.Log.V(1).Info("Skip unhandled watch event", "type", eventType, "group", groupName)
		return nil
	}

	for vpc, members := range addedMembers {
		// AddressGroup and AppliedToGroup cache key is 'Name of the group and VPC ID'. If the Group extends multiple
		// VPCs, multiple entries will be added in cache for each VPC.
		key := &securitygroup.CloudResourceID{Name: groupName, Vpc: vpc}
		var sg cloudSecurityGroup
		if i, ok, _ := indexer.GetByKey(key.String()); ok {
			sg = i.(cloudSecurityGroup)
			if compareCloudResources(members, sg.getMembers()) {
				r.Log.V(1).Info("Unchanged SecurityGroup, ignoring add.", "key", key)
				continue
			}
			removed, ok := removedMembers[vpc]
			if ok {
				delete(removedMembers, vpc)
			}
			if err := sg.update(members, removed, r); err != nil {
				r.Log.Error(err, "update SecurityGroup to cloud", "key", key)
				continue
			}
		} else {
			// All members in a vpc will share same AccountID and CloudProvider.
			cloudRsrc := securitygroup.CloudResource{
				Type:            securitygroup.CloudResourceTypeVM,
				CloudResourceID: *key,
				AccountID:       members[0].AccountID,
				CloudProvider:   members[0].CloudProvider,
			}
			sg = creatorFunc(&cloudRsrc, members, nil)
			if sg == nil {
				r.Log.Error(fmt.Errorf("failed to create"), "cloud resource", "resource", cloudRsrc)
				continue
			}
			if err := sg.add(r); err != nil {
				r.Log.Error(err, "add SecurityGroup to cloud", "key", key)
				continue
			}
		}
	}
	for vpc, members := range removedMembers {
		key := (&securitygroup.CloudResourceID{Name: groupName, Vpc: vpc}).String()
		i, ok, err := indexer.GetByKey(key)
		if !ok {
			r.Log.Error(err, "get from indexer", "key", key)
			continue
		}
		sg := i.(cloudSecurityGroup)
		if err := sg.update(nil, members, r); err != nil {
			r.Log.Error(err, "update SecurityGroup to cloud", "key", sg.getID())
			continue
		}
	}
	return nil
}

// processAddressGroup processes AddressGroup updates from Antrea controller.
func (r *NetworkPolicyReconciler) processAddressGroup(event watch.Event) error {
	accessor, _ := meta.Accessor(event.Object)
	patch, _ := event.Object.(*antreanetworking.AddressGroupPatch)
	complete, _ := event.Object.(*antreanetworking.AddressGroup)
	if (patch != nil && event.Type != watch.Modified) || (complete != nil && event.Type == watch.Modified) {
		return fmt.Errorf("mismatch message type for addrGroup: type=%v, name=%v", event.Type, accessor.GetName())
	}
	r.Log.V(1).Info("Received AddrGroup event",
		"type", event.Type, "name", accessor.GetName(), "obj", event.Object)
	var added, removed []antreanetworking.GroupMember
	if complete != nil && event.Type == watch.Added {
		added = complete.GroupMembers
	} else if patch != nil {
		added = patch.AddedGroupMembers
		removed = patch.RemovedGroupMembers
	}
	return r.processGroup(getNormalizedName(accessor.GetName()), event.Type, true, added, removed)
}

// processAppliedToGroup processes AppliedToGroup updates from Antrea controller.
func (r *NetworkPolicyReconciler) processAppliedToGroup(event watch.Event) error {
	accessor, _ := meta.Accessor(event.Object)
	patch, _ := event.Object.(*antreanetworking.AppliedToGroupPatch)
	complete, _ := event.Object.(*antreanetworking.AppliedToGroup)
	if (patch != nil && event.Type != watch.Modified) || (complete != nil && event.Type == watch.Modified) {
		return fmt.Errorf("mismatch message type for appliedToGroup: type=%v, name=%v", event.Type, accessor.GetName())
	}
	r.Log.V(1).Info("Received AppliedToGroup event",
		"type", event.Type, "name", accessor.GetName(), "obj", event.Object)
	var added, removed []antreanetworking.GroupMember
	if complete != nil && event.Type == watch.Added {
		added = complete.GroupMembers
	} else if patch != nil {
		added = patch.AddedGroupMembers
		removed = patch.RemovedGroupMembers
	}
	return r.processGroup(getNormalizedName(accessor.GetName()), event.Type, false, added, removed)
}

// processNetworkPolicy processes NetworkPolicy updates from Antrea controller.
func (r *NetworkPolicyReconciler) processNetworkPolicy(event watch.Event) error {
	anp, ok := event.Object.(*antreanetworking.NetworkPolicy)
	if !ok {
		r.Log.V(1).Info("Received unknown message type", "type", event.Type, "obj", event.Object)
		return nil
	}

	r.Log.V(1).Info("Received NetworkPolicy event", "type", event.Type, "obj", anp)

	if r.processBookMark(event.Type) {
		return nil
	}
	if err := r.isNetworkPolicySupported(anp); err != nil {
		r.sendRuleRealizationStatus(anp, err)
		return err
	}
	if anp.Namespace == "" {
		// anp comes from antrea controller, recover to its original name/namespace
		anp.Name = anp.SourceRef.Name
		anp.Namespace = anp.SourceRef.Namespace
	}
	r.normalizedANPObject(anp)

	var np *networkPolicy
	isCreate := false
	npKey := types.NamespacedName{Name: anp.Name, Namespace: anp.Namespace}.String()
	if i, ok, _ := r.networkPolicyIndexer.GetByKey(npKey); !ok {
		np = &networkPolicy{}
		anp.DeepCopyInto(&np.NetworkPolicy)
		if err := r.networkPolicyIndexer.Add(np); err != nil {
			return fmt.Errorf("add NetworkPolicy %v to indexer: %w", npKey, err)
		}
		isCreate = true
	} else {
		np = i.(*networkPolicy)
	}
	if event.Type == watch.Deleted {
		_ = np.delete(r)
		return nil
	}
	if !isCreate && reflect.DeepEqual(anp.Rules, np.Rules) &&
		reflect.DeepEqual(anp.AppliedToGroups, np.AppliedToGroups) {
		r.Log.V(1).Info("Unchanged NetworkPolicy, ignoring update.", "Name", anp.Name, "Namespace", anp.Namespace)
		// Send rule realization status if even no rule diff is found. This is because in case of
		// antrea controller restart, it will send all ANPs again and there won't be any diff.
		r.sendRuleRealizationStatus(anp, nil)
		return nil
	}
	if securitygroup.CloudSecurityGroup == nil {
		r.Log.V(1).Info("Skip NetworkPolicy message, no plug-in found")
		return nil
	}
	np.update(anp, isCreate, r)
	return nil
}

// processCloudResponse processes cloud operation responses.
func (r *NetworkPolicyReconciler) processCloudResponse(status *securityGroupStatus) error {
	return status.sg.notify(status.op, status.err, r)
}

func (r *NetworkPolicyReconciler) processLocalEvent(event watch.Event) error {
	switch event.Object.(type) {
	case *antreanetworking.NetworkPolicy:
		return r.processNetworkPolicy(event)
	case *antreanetworking.AppliedToGroup:
		return r.processAppliedToGroup(event)
	case *antreanetworking.AppliedToGroupPatch:
		return r.processAppliedToGroup(event)

	default:
		r.Log.Error(nil, "Unknown local event", "Event", event)
	}
	return nil
}

// LocalEvent adds a network policy event from local stack.
func (r *NetworkPolicyReconciler) LocalEvent(event watch.Event) {
	r.localRequest <- event
}

// Start starts NetworkPolicyReconciler.
func (r *NetworkPolicyReconciler) Start(stop context.Context) error {
	// Wait for ExternalEntity to be started.
	if err := r.externalEntityStart(stop); err != nil {
		return err
	}

	// Wait till all the dependent controllers are synced.
	if err := GetControllerSyncStatusInstance().waitForControllersToSync([]controllerType{ControllerTypeCPA,
		ControllerTypeCES, ControllerTypeEE, ControllerTypeVM}, syncTimeout); err != nil {
		return err
	}

	if err := r.resetWatchers(); err != nil {
		r.Log.Error(err, "Start watchers")
	}

	r.Log.Info("Re-sync finished, listening to new events")
	lastSyncTime := time.Now().Unix()
	ticker := time.NewTicker(time.Second)
	for {
		var err error
		select {
		case event, ok := <-r.addrGroupWatcher.ResultChan():
			if !ok || event.Type == watch.Error {
				r.Log.V(1).Info("Closed addrGroupWatcher channel, restart")
				if err := r.resetWatchers(); err != nil {
					r.Log.Error(err, "start watchers")
				}
				break
			}
			err = r.processAddressGroup(event)
		case event, ok := <-r.appliedToGroupWatcher.ResultChan():
			if !ok || event.Type == watch.Error {
				r.Log.V(1).Info("Closed appliedToGroupWatcher channel, restart")
				if err := r.resetWatchers(); err != nil {
					r.Log.Error(err, "start watchers")
				}
				break
			}
			err = r.processAppliedToGroup(event)
		case event, ok := <-r.networkPolicyWatcher.ResultChan():
			if !ok || event.Type == watch.Error {
				r.Log.V(1).Info("Closed networkPolicyWatcher channel, restart")
				if err := r.resetWatchers(); err != nil {
					r.Log.Error(err, "start watchers")
				}
				break
			}
			err = r.processNetworkPolicy(event)
		case status, ok := <-r.cloudResponse:
			if !ok {
				r.Log.Info("Cloud response channel is closed")
				return nil
			}
			err = r.processCloudResponse(status)
		case event, ok := <-r.localRequest:
			if !ok {
				r.Log.Info("Local request channel is closed")
				return nil
			}
			err = r.processLocalEvent(event)
		case <-ticker.C:
			r.backgroupProcess()
			r.retryQueue.CheckToRun()
			if time.Now().Unix()-lastSyncTime >= r.CloudSyncInterval {
				r.syncWithCloud()
				lastSyncTime = time.Now().Unix()
			}
		case <-stop.Done():
			r.Log.Info("Is stopped")
			return nil
		}
		if err != nil {
			r.Log.Error(err, "processing")
		}
	}
}

// externalEntityStart performs the initialization of the controller.
// A controller is said to be initialized only when the dependent controllers
// are synced, and it keeps a count of pending CRs to be reconciled.
func (r *NetworkPolicyReconciler) externalEntityStart(_ context.Context) error {
	if err := GetControllerSyncStatusInstance().waitForControllersToSync([]controllerType{ControllerTypeCPA}, syncTimeout); err != nil {
		r.Log.Error(err, "dependent controller sync failed", "controller", ControllerTypeCPA.String())
		return err
	}
	eeList := &antreav1alpha2.ExternalEntityList{}
	if err := r.Client.List(context.TODO(), eeList, &client.ListOptions{}); err != nil {
		return err
	}

	r.pendingSyncCount = len(eeList.Items)
	if r.pendingSyncCount == 0 {
		GetControllerSyncStatusInstance().SetControllerSyncStatus(ControllerTypeEE)
	}
	r.initialized = true
	r.Log.Info("Init done", "controller", ControllerTypeEE.String())
	return nil
}

// Reconcile exists to cache ExternalEntities in shared informer.
func (r *NetworkPolicyReconciler) Reconcile(_ context.Context, _ ctrl.Request) (ctrl.Result, error) {
	if !r.initialized {
		if err := GetControllerSyncStatusInstance().waitTillControllerIsInitialized(&r.initialized, initTimeout, ControllerTypeEE); err != nil {
			return ctrl.Result{}, err
		}
	}
	r.updatePendingSyncCountAndStatus()
	return ctrl.Result{}, nil
}

// updatePendingSyncCountAndStatus decrements the pendingSyncCount and when
// pendingSyncCount is 0, sets the sync status.
func (r *NetworkPolicyReconciler) updatePendingSyncCountAndStatus() {
	if r.pendingSyncCount > 0 {
		r.pendingSyncCount--
		if r.pendingSyncCount == 0 {
			GetControllerSyncStatusInstance().SetControllerSyncStatus(ControllerTypeEE)
		}
	}
}

// backgroundProcess runs background processes.
func (r *NetworkPolicyReconciler) backgroupProcess() {
	r.processCloudResourceNPTrackers()
}

// SetupWithManager sets up NetworkPolicyReconciler with manager.
func (r *NetworkPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.addrSGIndexer = cache.NewIndexer(
		// Each addrSecurityGroup is uniquely identified by its ID.
		func(obj interface{}) (string, error) {
			addrGrp := obj.(*addrSecurityGroup)
			return addrGrp.id.CloudResourceID.String(), nil
		},
		// addrSecurityGroup indexed by Antrea AddrGroup ID.
		cache.Indexers{
			addrAppliedToIndexerByGroupID: func(obj interface{}) ([]string, error) {
				addrGrp := obj.(*addrSecurityGroup)
				return []string{addrGrp.id.Name}, nil
			},
		})
	r.appliedToSGIndexer = cache.NewIndexer(
		// Each appliedToSecurityGroup is uniquely identified by its ID.
		func(obj interface{}) (string, error) {
			appliedToGrp := obj.(*appliedToSecurityGroup)
			return appliedToGrp.id.CloudResourceID.String(), nil
		},
		// AppliedToSecurityGroup indexed by Antrea AddrGroup ID.
		cache.Indexers{
			addrAppliedToIndexerByGroupID: func(obj interface{}) ([]string, error) {
				appliedToGrp := obj.(*appliedToSecurityGroup)
				return []string{appliedToGrp.id.Name}, nil
			},
			appliedToIndexerByAddrGroupRef: func(obj interface{}) ([]string, error) {
				appliedToGrp := obj.(*appliedToSecurityGroup)
				addrGrps := make([]string, 0)
				for sg := range appliedToGrp.addrGroupRefs {
					addrGrps = append(addrGrps, sg)
				}
				return addrGrps, nil
			},
		},
	)
	r.networkPolicyIndexer = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			np := obj.(*networkPolicy)
			return types.NamespacedName{Name: np.Name, Namespace: np.Namespace}.String(), nil
		},
		cache.Indexers{
			// networkPolicy indexed by Antrea AddrGroup ID.
			networkPolicyIndexerByAddrGrp: func(obj interface{}) ([]string, error) {
				np := obj.(*networkPolicy)
				addrGrps := make([]string, 0)
				for _, rule := range np.Rules {
					addrGrps = append(addrGrps, rule.To.AddressGroups...)
					addrGrps = append(addrGrps, rule.From.AddressGroups...)
				}
				return addrGrps, nil
			},
			// networkPolicy indexed by Antrea AppliedTo ID.
			networkPolicyIndexerByAppliedToGrp: func(obj interface{}) ([]string, error) {
				np := obj.(*networkPolicy)
				return np.AppliedToGroups, nil
			},
		})
	r.cloudResourceNPTrackerIndexer = cache.NewIndexer(
		// Each cloudResourceNPTracker is uniquely identified by cloud resource.
		func(obj interface{}) (string, error) {
			tracker := obj.(*cloudResourceNPTracker)
			return tracker.cloudResource.String(), nil
		},
		// cloudResourceNPTracker indexed by appliedToSecurityGroup.
		cache.Indexers{
			cloudResourceNPTrackerIndexerByAppliedToGrp: func(obj interface{}) ([]string, error) {
				tracker := obj.(*cloudResourceNPTracker)
				sgs := make([]string, 0, len(tracker.appliedToSGs)+len(tracker.prevAppliedToSGs))
				for i := range tracker.appliedToSGs {
					sgs = append(sgs, i)
				}
				for i := range tracker.prevAppliedToSGs {
					sgs = append(sgs, i)
				}
				return sgs, nil
			},
		})
	// cloudRuleIndexer stores the realized rules on the cloud.
	r.cloudRuleIndexer = cache.NewIndexer(
		// Each cloudRule is uniquely identified by its UUID.
		func(obj interface{}) (string, error) {
			rule := obj.(*securitygroup.CloudRule)
			return rule.Hash, nil
		},
		// cloudRules indexed by appliedToSecurityGroup.
		cache.Indexers{
			cloudRuleIndexerByAppliedToGrp: func(obj interface{}) ([]string, error) {
				rule := obj.(*securitygroup.CloudRule)
				return []string{rule.AppliedToGrp}, nil
			},
		})
	r.virtualMachinePolicyIndexer = cache.NewIndexer(
		// Each VirtualMachinePolicy is uniquely identified by namespaced name of corresponding crd object.
		func(obj interface{}) (string, error) {
			npStatus := obj.(*NetworkPolicyStatus)
			return npStatus.String(), nil
		},
		// VirtualMachinePolicy indexed by namespace
		cache.Indexers{
			NetworkPolicyStatusIndexerByNamespace: func(obj interface{}) ([]string, error) {
				npStatus := obj.(*NetworkPolicyStatus)
				return []string{npStatus.Namespace}, nil
			},
		})
	r.localRequest = make(chan watch.Event)
	r.cloudResponse = make(chan *securityGroupStatus, cloudResponseChBuffer)
	r.pendingDeleteGroups = NewPendingItemQueue(r, nil)
	opCnt := operationCount
	r.retryQueue = NewPendingItemQueue(r, &opCnt)

	if mgr == nil {
		return nil
	}
	r.antreaClient = antreanetworkingclient.NewForConfigOrDie(mgr.GetConfig())
	if err := ctrl.NewControllerManagedBy(mgr).For(&antreav1alpha2.ExternalEntity{}).Complete(r); err != nil {
		return err
	}
	return mgr.Add(r)
}

func (r *NetworkPolicyReconciler) resetWatchers() error {
	var err error
	options := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("nodeName", config.ANPNepheController).String(),
	}
	for {
		if r.addrGroupWatcher, err = r.antreaClient.AddressGroups().Watch(context.Background(), options); err != nil {
			r.Log.Error(err, "watcher connect to AddressGroup")
			time.Sleep(time.Second * 5)
			continue
		}
		if r.appliedToGroupWatcher, err = r.antreaClient.AppliedToGroups().Watch(context.Background(), options); err != nil {
			r.Log.Error(err, "watcher connect to AppliedToGroups")
			time.Sleep(time.Second * 5)
			continue
		}
		if r.networkPolicyWatcher, err = r.antreaClient.NetworkPolicies().Watch(context.Background(), options); err != nil {
			r.Log.Error(err, "watcher connect to NetworkPolicy")
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}
	return err
}

func (r *NetworkPolicyReconciler) GetVirtualMachinePolicyIndexer() cache.Indexer {
	return r.virtualMachinePolicyIndexer
}
