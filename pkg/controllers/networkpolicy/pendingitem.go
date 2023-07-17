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

	"github.com/mohae/deepcopy"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
)

var (
	retryInterval int64 = 20
	lastRetryTime       = time.Now().Unix()
)

type PendingItem interface {
	// RunPendingItem runs this pending item, it returns true if item
	// should be removed.
	RunPendingItem(id string, context interface{}) bool
	// UpdatePendingItem updates ths pending item, returns
	UpdatePendingItem(id string, context interface{}, updates ...interface{})
	// RunOrDeletePendingItem returns run as true, if this item can be run;
	// returns delete as true, if this item shall be removed.
	RunOrDeletePendingItem(id string, context interface{}) (run bool, delete bool)
	// ClearPendingState clears item state when removing item from pending queue.
	ClearPendingState()
}

type countingPendingItem struct {
	PendingItem
	retryCount *int
}

type PendingItemQueue struct {
	items      map[string]countingPendingItem
	context    interface{}
	retryCount *int
}

// NewPendingItemQueue returns a new PendingItemQueue.
// If retryCount is not provided, item is removed if item.RunOrDeletePendingItem returns true;
// if retryCount is provided, item is also removed when item.RunPendingItem is called.
func NewPendingItemQueue(context interface{}, retryCount *int) *PendingItemQueue {
	return &PendingItemQueue{
		items:      make(map[string]countingPendingItem),
		context:    context,
		retryCount: retryCount,
	}
}

// Add a pending item to queue.
func (q *PendingItemQueue) Add(id string, p PendingItem) {
	log := q.context.(*NetworkPolicyReconciler).Log.WithName("PendingItemQueue")
	if _, ok := q.items[id]; ok {
		log.Info("Add existing item", "Name", id)
		return
	}
	q.items[id] = countingPendingItem{p, deepcopy.Copy(q.retryCount).(*int)}
}

// Remove a pending item from queue.
func (q *PendingItemQueue) Remove(id string) {
	if p, ok := q.items[id]; ok {
		p.ClearPendingState()
	}
	delete(q.items, id)
}

// Has returns true if an item in the queue.
func (q *PendingItemQueue) Has(id string) bool {
	_, ok := q.items[id]
	return ok
}

// Update item and check to run item.
func (q *PendingItemQueue) Update(id string, checkRun bool, updates ...interface{}) error {
	log := q.context.(*NetworkPolicyReconciler).Log.WithName("PendingItemQueue")
	i, ok := q.items[id]
	if !ok {
		err := fmt.Errorf("not found")
		log.Error(err, "update", "Name", id)
		return err
	}
	i.UpdatePendingItem(id, q.context, updates...)
	if !checkRun {
		return nil
	}
	run, del := i.RunOrDeletePendingItem(id, q.context)
	if del {
		q.Remove(id)
	}
	if run {
		del = i.RunPendingItem(id, q.context)
		if i.retryCount != nil {
			*i.retryCount--
		}
	}
	if del || (i.retryCount != nil && *i.retryCount <= 0) {
		q.Remove(id)
	}
	return nil
}

// GetRetryCount returns remaining retryCount of item if applicable.
func (q *PendingItemQueue) GetRetryCount(id string) int {
	log := q.context.(*NetworkPolicyReconciler).Log.WithName("PendingItemQueue")
	i, ok := q.items[id]
	if !ok {
		err := fmt.Errorf("not found")
		log.Error(err, "failed to GetRetryCount", "Name", id)
		return -1
	}
	if i.retryCount == nil {
		return -1
	}
	return *i.retryCount
}

// CheckToRun check and run all items on queue.
func (q *PendingItemQueue) CheckToRun(forceRetry bool) {
	if !forceRetry && time.Now().Unix()-lastRetryTime < retryInterval {
		return
	}
	for k, i := range q.items {
		run, del := i.RunOrDeletePendingItem(k, q.context)
		if del {
			q.Remove(k)
		}
		if run {
			if i.retryCount == nil || *i.retryCount > 0 {
				del = i.RunPendingItem(k, q.context)
			}
			if i.retryCount != nil {
				*i.retryCount--
			}
		}
		if del || (i.retryCount != nil && *i.retryCount < 0) {
			q.Remove(k)
		}
	}
	lastRetryTime = time.Now().Unix()
}

var (
	_ PendingItem = &addrSecurityGroup{}
	_ PendingItem = &appliedToSecurityGroup{}
	_ PendingItem = &pendingGroup{}
)

type pendingGroup struct {
	refCnt         *int
	runOnClear     bool
	event          watch.EventType
	addedMembers   map[string]antreanetworking.GroupMember
	removedMembers map[string]antreanetworking.GroupMember
	// Used to clean up internal state only when account is deleted.
	accountId string
	id        string
}

func (p *pendingGroup) RunPendingItem(id string, context interface{}) bool {
	r := context.(*NetworkPolicyReconciler)
	if len(p.addedMembers) == 0 {
		return true
	}
	members := make([]antreanetworking.GroupMember, 0, len(p.addedMembers))
	for _, v := range p.addedMembers {
		members = append(members, v)
	}
	name, memberOnly := getGroupIDFromUniqueName(id)
	err := r.processGroup(name, p.event, memberOnly, members, nil)
	if p.refCnt != nil {
		return true
	}
	return err == nil
}

func (p *pendingGroup) UpdatePendingItem(id string, context interface{}, updates ...interface{}) {
	r := context.(*NetworkPolicyReconciler)
	log := r.Log.WithName("PendingGroup")
	if refCnt, ok := updates[0].(int); ok {
		p.runOnClear = updates[1].(bool)
		*p.refCnt += refCnt
		log.V(1).Info("Update reference", "Name", id, "Cnt", *p.refCnt, "RunOnClear", p.runOnClear)
		return
	}

	event := updates[0].(watch.EventType)
	if len(p.event) == 0 {
		p.event = event
	}
	if event == watch.Deleted {
		p.addedMembers = nil
		p.removedMembers = nil
		p.event = ""
	} else if event == watch.Added || event == watch.Modified {
		if p.addedMembers == nil {
			p.addedMembers = make(map[string]antreanetworking.GroupMember)
		}
		if p.removedMembers == nil {
			p.removedMembers = make(map[string]antreanetworking.GroupMember)
		}
		added := updates[1].([]antreanetworking.GroupMember)
		removed := updates[2].([]antreanetworking.GroupMember)
		for _, i := range removed {
			key := types.NamespacedName{Name: i.ExternalEntity.Name, Namespace: i.ExternalEntity.Namespace}.String()
			if _, ok := p.addedMembers[key]; ok {
				delete(p.addedMembers, key)
			} else {
				p.removedMembers[key] = deepcopy.Copy(i).(antreanetworking.GroupMember)
			}
		}
		for _, i := range added {
			key := types.NamespacedName{Name: i.ExternalEntity.Name, Namespace: i.ExternalEntity.Namespace}.String()
			delete(p.removedMembers, key)
			p.addedMembers[key] = deepcopy.Copy(i).(antreanetworking.GroupMember)
		}
	}
	log.V(1).Info("Update group members", "Name", id, "Event", p.event,
		"AddedMembers", p.addedMembers, "RemovedMembers", p.removedMembers)
}

func (p *pendingGroup) RunOrDeletePendingItem(id string, context interface{}) (run bool, delete bool) {
	r := context.(*NetworkPolicyReconciler)
	log := r.Log.WithName("PendingGroup")
	// if refCnt not specify this is retry item.
	if p.refCnt == nil {
		run = true
		delete = false
	} else if *p.refCnt > 0 || !p.runOnClear {
		run = false
		delete = false
	} else {
		run = true
		delete = true
	}
	log.V(1).Info("Run or delete", "Name", id, "run", run, "delete", delete)
	return
}

func (p *pendingGroup) ClearPendingState() {}

func (s *securityGroupImpl) runPendingItemImpl(c cloudSecurityGroup, memberOnly bool, r *NetworkPolicyReconciler) bool {
	op := *s.retryOp
	s.retryOp = nil
	var err error
	if op == securityGroupOperationAdd {
		err = s.addImpl(c, memberOnly, r)
	} else if op == securityGroupOperationUpdateMembers {
		err = s.updateImpl(c, nil, nil, memberOnly, r)
	} else if op == securityGroupOperationDelete {
		err = s.deleteImpl(c, memberOnly, r)
	} else if op == securityGroupOperationClearMembers || op == securityGroupOperationUpdateRules {
		ag := c.(*appliedToSecurityGroup)
		err = ag.updateAllRules(r)
	}
	if err != nil {
		// TODO
	} else {
		s.retryInProgress = true
	}
	s.retryOp = &op
	return false
}

func (s *securityGroupImpl) RunOrDeletePendingItem(id string, context interface{}) (run bool, delete bool) {
	r := context.(*NetworkPolicyReconciler)
	log := r.Log.WithName("RetryingSecurityGroup")
	if s.retryOp == nil {
		run = false
		delete = true
	} else if s.retryInProgress {
		run = false
		delete = false
	} else {
		run = true
		delete = false
	}
	log.V(1).Info("Run or delete", "Name", id, "run", run, "delete", delete)
	return
}

func (s *securityGroupImpl) UpdatePendingItem(id string, context interface{}, updates ...interface{}) {
	r := context.(*NetworkPolicyReconciler)
	log := r.Log.WithName("RetryingSecurityGroup")
	op := updates[0].(securityGroupOperation)
	if s.retryOp != nil && *s.retryOp == op {
		s.retryInProgress = false
		if s.status == nil {
			s.retryOp = nil
		}
	}
	opStr := "nil"
	if s.retryOp != nil {
		opStr = s.retryOp.String()
	}
	log.V(1).Info("Update security group", "Name", id, "retryOp", opStr, "inProgress", s.retryInProgress)
}

// ClearPendingState clears retryOp state when removing pending security group item.
func (s *securityGroupImpl) ClearPendingState() {
	s.retryOp = nil
}

func (a *addrSecurityGroup) RunPendingItem(_ string, context interface{}) bool {
	r := context.(*NetworkPolicyReconciler)
	return a.runPendingItemImpl(a, true, r)
}

func (a *appliedToSecurityGroup) RunPendingItem(_ string, context interface{}) bool {
	r := context.(*NetworkPolicyReconciler)
	return a.runPendingItemImpl(a, false, r)
}
