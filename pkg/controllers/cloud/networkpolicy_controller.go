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
	"antrea.io/antrea/pkg/apis/crd/v1alpha1"
	"context"
	"fmt"
	"net"
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
	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	antreanetworkingclient "antrea.io/antrea/pkg/client/clientset/versioned/typed/controlplane/v1beta2"
	cloud "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/controllers/config"
)

const (
	NetworkPolicyStatusIndexerByNamespace       = "namespace"
	addrAppliedToIndexerByGroupID               = "GroupID"
	networkPolicyIndexerByAddrGrp               = "AddressGrp"
	networkPolicyIndexerByAppliedToGrp          = "AppliedToGrp"
	cloudResourceNPTrackerIndexerByAppliedToGrp = "AppliedToGrp"
	virtualMachineIndexerByCloudID              = "metadata.annotations.cloud-assigned-id"
	virtualMachineIndexerByCloudName            = "metadata.annotations.cloud-assigned-name"

	operationCount    = 15
	cloudSyncInterval = 0xff // 256 Seconds

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
	antreaClient *antreanetworkingclient.ControlplaneV1beta2Client

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

	// pendingDeleteGroups keep tracks of deleting AddressGroup or AppliedToGroup.
	pendingDeleteGroups *PendingItemQueue
	retryQueue          *PendingItemQueue

	// cloudResponse receives responses from cloud operations.
	cloudResponse chan *securityGroupStatus

	// syncedWithCloud is true if controller has synchronized with cloud at least once.
	syncedWithCloud bool
	// Bookmark events received prior to sync with the cloud.
	bookmarkCnt int

	// localRequest sends and receives network policy requests from local stack.
	localRequest chan watch.Event

	// Federated ExternalEntities IPs.
	fedExternalEntityIPs map[string][]string
}

// isNetworkPolicySupported check if network policy is supported.
func (r *NetworkPolicyReconciler) isNetworkPolicySupported(anp *antreanetworking.NetworkPolicy) error {
	if anp.SourceRef == nil {
		return fmt.Errorf("source reference not set in network policy")
	}
	if anp.SourceRef.Type != antreanetworking.AntreaNetworkPolicy {
		return fmt.Errorf("only antrea network policy is supported")
	}
	// Check for support actions
	for _, rule := range anp.Rules {
		if rule.Action != nil && *rule.Action != v1alpha1.RuleActionAllow {
			return fmt.Errorf("only Allow action is supported in antrea network policy")
		}
	}
	return nil
}

// processMemberGrp is common function to process AppliedTo/AddressGroup updates from Antrea controller.
func (r *NetworkPolicyReconciler) processMemberGrp(name string, eventType watch.EventType, isAddrGrp bool,
	added, removed []antreanetworking.GroupMember) error {
	if r.processBookMark(eventType) {
		return nil
	}
	uName := getGroupUniqueName(name, isAddrGrp)
	var err error
	defer func() {
		if err != nil && (eventType == watch.Added || eventType == watch.Modified) {
			if !r.retryQueue.Has(uName) {
				r.retryQueue.Add(uName, &pendingGroup{})
			}
			_ = r.retryQueue.Update(uName, false, eventType, added, removed)
		}
		if eventType == watch.Deleted {
			r.retryQueue.Remove(getGroupUniqueName(name, isAddrGrp))
		}
	}()

	if securitygroup.CloudSecurityGroup == nil {
		r.Log.V(1).Info("Skip group message no plug-in found", "group", name)
		return nil
	}

	if r.pendingDeleteGroups.Has(uName) {
		_ = r.pendingDeleteGroups.Update(uName, false, eventType, added, removed)
		r.Log.V(1).Info("Wait for group pending delete to complete", "Name", uName)
		return nil
	}

	var indexer cache.Indexer
	var creator func(*securitygroup.CloudResourceID, interface{}, *securityGroupState) cloudSecurityGroup
	if isAddrGrp {
		indexer = r.addrSGIndexer
		creator = newAddrSecurityGroup
	} else {
		indexer = r.appliedToSGIndexer
		creator = newAppliedToSecurityGroup
	}

	var addedMembers, removedMembers map[string][]*securitygroup.CloudResource
	var addedIPs, removedIPs []*net.IPNet
	var notFoundMember []string
	if eventType == watch.Added {
		if addedMembers, addedIPs, notFoundMember, err = vpcsFromGroupMembers(added, r); err != nil {
			return err
		}
		if len(notFoundMember) > 0 {
			err = fmt.Errorf("missing externalEntities: %v", notFoundMember)
			return err
		}
	} else if eventType == watch.Modified {
		if addedMembers, addedIPs, notFoundMember, err = vpcsFromGroupMembers(added, r); err != nil {
			return err
		}
		if len(notFoundMember) > 0 {
			err = fmt.Errorf("missing externalEntities: %v", notFoundMember)
			return err
		}
		if removedMembers, removedIPs, notFoundMember, err = vpcsFromGroupMembers(removed, r); err != nil {
			return err
		}
		if len(notFoundMember) > 0 {
			sgs, _ := r.addrSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, name)
			for _, i := range sgs {
				i.(*addrSecurityGroup).removeStaleMembers(notFoundMember, r)
			}
			sgs1, _ := r.appliedToSGIndexer.ByIndex(addrAppliedToIndexerByGroupID, name)
			for _, i := range sgs1 {
				i.(*appliedToSecurityGroup).removeStaleMembers(notFoundMember, r)
			}
		}
	} else if eventType == watch.Deleted {
		sgs, err := indexer.ByIndex(addrAppliedToIndexerByGroupID, name)
		if err != nil {
			return err
		}
		for _, i := range sgs {
			sg := i.(cloudSecurityGroup)
			if err := sg.delete(r); err != nil {
				r.Log.Error(err, "Delete SecurityGroup to cloud", "key", sg.getID())
			}
		}
		return nil
	} else {
		r.Log.V(1).Info("Skip unhandled watch event", "type", eventType, "group", name)
		return nil
	}

	// sgChanges changes in addrGroup requires, associated nps to recompute their rules.
	sgChanges := false
	for vpc, members := range addedMembers {
		key := &securitygroup.CloudResourceID{Name: name, Vpc: vpc}
		var sg cloudSecurityGroup
		if i, ok, _ := indexer.GetByKey(key.String()); ok {
			sg = i.(cloudSecurityGroup)
			if compareCloudResources(members, sg.getMembers()) {
				r.Log.V(1).Info("Ignore add unchanged SecurityGroup to cloud", "key", key)
				continue
			}
			removed, ok := removedMembers[vpc]
			if ok {
				delete(removedMembers, vpc)
			}
			if err := sg.update(members, removed, r); err != nil {
				r.Log.Error(err, "Update SecurityGroup to cloud", "key", key)
				continue
			}
		} else {
			sg = creator(key, members, nil)
			if sg == nil {
				continue
			}
			if err := sg.add(r); err != nil {
				r.Log.Error(err, "Add SecurityGroup to cloud", "key", key)
				continue
			}
			sgChanges = true
		}
	}
	for vpc, members := range removedMembers {
		key := (&securitygroup.CloudResourceID{Name: name, Vpc: vpc}).String()
		i, ok, err := indexer.GetByKey(key)
		if !ok {
			r.Log.Error(err, "Get from indexer", "key", key)
			continue
		}
		sg := i.(cloudSecurityGroup)
		if err := sg.update(nil, members, r); err != nil {
			r.Log.Error(err, "Update SecurityGroup to cloud", "key", sg.getID())
			continue
		}
	}
	if isAddrGrp && (addedIPs != nil || removedIPs != nil) {
		key := &securitygroup.CloudResourceID{Name: name, Vpc: ""}
		sg, _, _ := indexer.GetByKey(key.String())
		if sg != nil {
			sg.(*addrSecurityGroup).updateIPs(addedIPs, removedIPs, r)
		} else if eventType == watch.Added {
			sg = creator(key, addedIPs, nil)
			_ = sg.(*addrSecurityGroup).add(r)
		} else {
			r.Log.Error(nil, "Update to IP block does find security group", "key", key)
		}
		sgChanges = true
	}
	// Empty membershipGroup
	if eventType == watch.Added && len(added) == 0 && len(removed) == 0 && isAddrGrp {
		key := &securitygroup.CloudResourceID{Name: name, Vpc: ""}
		sg, _, _ := indexer.GetByKey(key.String())
		if sg != nil {
			r.Log.Info("Cannot add an empty membershipGroup that already exists", "Key", key)
			return nil
		}
		sg = creator(key, addedIPs, nil)
		_ = sg.(*addrSecurityGroup).add(r)
		sgChanges = true
	}
	if sgChanges && isAddrGrp {
		nps, _ := r.networkPolicyIndexer.ByIndex(networkPolicyIndexerByAddrGrp, name)
		for _, i := range nps {
			np := i.(*networkPolicy)
			np.update(nil, true, r)
		}
	}
	return nil
}

// processAddrGrp processes AddrGroup updates from Antrea controller.
func (r *NetworkPolicyReconciler) processAddrGrp(event watch.Event) error {
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
	return r.processMemberGrp(accessor.GetName(), event.Type, true, added, removed)
}

// processAppliedToGrp processes AppliedToGroup updates from Antrea controller.
func (r *NetworkPolicyReconciler) processAppliedToGrp(event watch.Event) error {
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
	return r.processMemberGrp(accessor.GetName(), event.Type, false, added, removed)
}

// processNetworkPolicy processes NetworkPolicy updates from Antrea controller.
func (r *NetworkPolicyReconciler) processNetworkPolicy(event watch.Event) error {
	anp, ok := event.Object.(*antreanetworking.NetworkPolicy)
	if !ok {
		r.Log.V(1).Info("processNetworkPolicy unknown message type", "type", event.Type, "obj", event.Object)
		return nil
	}
	if r.processBookMark(event.Type) {
		return nil
	}

	r.Log.V(1).Info("Received NetworkPolicy event", "type", event.Type, "obj", anp)
	if err := r.isNetworkPolicySupported(anp); err != nil {
		return err
	}
	if anp.Namespace == "" {
		// anp comes from antrea controller, recover to its original name/namespace
		anp.Name = anp.SourceRef.Name
		anp.Namespace = anp.SourceRef.Namespace
	}

	var np *networkPolicy
	isCreate := false
	npKey := types.NamespacedName{Name: anp.Name, Namespace: anp.Namespace}.String()
	if i, ok, _ := r.networkPolicyIndexer.GetByKey(npKey); !ok {
		np = &networkPolicy{}
		anp.DeepCopyInto(&np.NetworkPolicy)
		if err := r.networkPolicyIndexer.Add(np); err != nil {
			return fmt.Errorf("add networkPolicy %v to indexer: %w", npKey, err)
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
		r.Log.V(1).Info("Ignore update unchanged NetworkPolicy", "Name", anp.Name, "Namespace", anp.Namespace)
		return nil
	}
	if securitygroup.CloudSecurityGroup == nil {
		r.Log.V(1).Info("Skip NP message no plug-in found")
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
		return r.processAppliedToGrp(event)
	case *antreanetworking.AppliedToGroupPatch:
		return r.processAppliedToGrp(event)

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
	if err := r.resetWatchers(); err != nil {
		r.Log.Error(err, "Start watchers")
	}

	r.Log.Info("Re-sync finished, listening to new events")
	ticker := time.NewTicker(time.Second)
	for {
		var err error
		select {
		case event, ok := <-r.addrGroupWatcher.ResultChan():
			if !ok || event.Type == watch.Error {
				r.Log.V(1).Info("addrGroupWatcher is closed, restart")
				if err := r.resetWatchers(); err != nil {
					r.Log.Error(err, "Start watchers")
				}
				break
			}
			err = r.processAddrGrp(event)
		case event, ok := <-r.appliedToGroupWatcher.ResultChan():
			if !ok || event.Type == watch.Error {
				r.Log.V(1).Info("appliedToGroupWatcher is closed, restart")
				if err := r.resetWatchers(); err != nil {
					r.Log.Error(err, "Start watchers")
				}
				break
			}
			err = r.processAppliedToGrp(event)
		case event, ok := <-r.networkPolicyWatcher.ResultChan():
			if !ok || event.Type == watch.Error {
				r.Log.V(1).Info("networkPolicyWatcher is closed, restart")
				if err := r.resetWatchers(); err != nil {
					r.Log.Error(err, "Start watchers")
				}
				break
			}
			err = r.processNetworkPolicy(event)
		case status, ok := <-r.cloudResponse:
			if !ok {
				r.Log.Info("Cloud response is closed")
				return nil
			}
			err = r.processCloudResponse(status)
		case event, ok := <-r.localRequest:
			if !ok {
				r.Log.Info("Local request is closed")
				return nil
			}
			err = r.processLocalEvent(event)
		case <-ticker.C:
			r.backgroupProcess()
			r.retryQueue.CheckToRun()
			if time.Now().Unix()&cloudSyncInterval == 0 {
				r.syncWithCloud()
			}
		case <-stop.Done():
			r.Log.Info("is stopped")
			return nil
		}
		if err != nil {
			r.Log.Error(err, "Processing")
		}
	}
}

// Reconcile exists to cache ExternalEntities in shared informer.
func (r *NetworkPolicyReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.V(1).Info("Received ExternalEntity", "namespacedName", req.NamespacedName)
	return ctrl.Result{}, nil
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
			return addrGrp.id.String(), nil
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
			return appliedToGrp.id.String(), nil
		},
		// AppliedToSecurityGroup indexed by Antrea AddrGroup ID.
		cache.Indexers{
			addrAppliedToIndexerByGroupID: func(obj interface{}) ([]string, error) {
				appliedToGrp := obj.(*appliedToSecurityGroup)
				return []string{appliedToGrp.id.Name}, nil
			},
		})
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
		// cloudResourceNPTracker indexed by appliedToSecurityGroup
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
				ret := []string{npStatus.Namespace}
				return ret, nil
			},
		})
	r.localRequest = make(chan watch.Event)
	r.cloudResponse = make(chan *securityGroupStatus)
	r.pendingDeleteGroups = NewPendingItemQueue(r, nil)
	r.fedExternalEntityIPs = make(map[string][]string)
	opCnt := operationCount
	r.retryQueue = NewPendingItemQueue(r, &opCnt)

	if mgr == nil {
		return nil
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &cloud.VirtualMachine{}, virtualMachineIndexerByCloudID,
		func(obj client.Object) []string {
			vm := obj.(*cloud.VirtualMachine)
			cloudID := vm.Annotations[common.AnnotationCloudAssignedIDKey]
			return []string{cloudID}
		}); err != nil {
		return err
	}
	r.antreaClient = antreanetworkingclient.NewForConfigOrDie(mgr.GetConfig())
	if err := ctrl.NewControllerManagedBy(mgr).For(&antreatypes.ExternalEntity{}).Complete(r); err != nil {
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
			r.Log.Error(err, "Watcher connect to AddressGroup")
			time.Sleep(time.Second * 5)
			continue
		}
		if r.appliedToGroupWatcher, err = r.antreaClient.AppliedToGroups().Watch(context.Background(), options); err != nil {
			r.Log.Error(err, "Watcher connect to AppliedToGroups")
			time.Sleep(time.Second * 5)
			continue
		}
		if r.networkPolicyWatcher, err = r.antreaClient.NetworkPolicies().Watch(context.Background(), options); err != nil {
			r.Log.Error(err, "Watcher connect to NetworkPolicy")
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
