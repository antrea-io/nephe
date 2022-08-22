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

package cloudprovider

import (
	"fmt"
	"sync"

	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

type SecurityGroupImpl struct{}

func init() {
	securitygroup.CloudSecurityGroup = &SecurityGroupImpl{}
}

func getCloudInterfaceForCloudResource(addressGroupIdentifier *securitygroup.CloudResourceID) (cloudcommon.CloudInterface, error) {
	var cloudInterfaceForResource cloudcommon.CloudInterface

	providerTypes := GetSupportedCloudProviderTypes()
	for _, providerType := range providerTypes {
		cloudInterface, err := GetCloudInterface(providerType)
		if err != nil {
			corePluginLogger().Error(err, "get cloud interface failed", "providerType", providerType)
			continue
		}
		if cloudInterface.IsVirtualPrivateCloudPresent(addressGroupIdentifier.Vpc) {
			cloudInterfaceForResource = cloudInterface
			break
		}
	}
	if cloudInterfaceForResource != nil {
		return cloudInterfaceForResource, nil
	}
	return nil, fmt.Errorf("virtual private cloud [%v] not managed by supported clouds [%v]", addressGroupIdentifier.Vpc, providerTypes)
}

func (sg *SecurityGroupImpl) CreateSecurityGroup(addressGroupIdentifier *securitygroup.CloudResourceID, membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(addressGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		_, err = cloudInterface.CreateSecurityGroup(addressGroupIdentifier, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *SecurityGroupImpl) UpdateSecurityGroupMembers(addressGroupIdentifier *securitygroup.CloudResourceID,
	members []*securitygroup.CloudResource, membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(addressGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.UpdateSecurityGroupMembers(addressGroupIdentifier, members, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *SecurityGroupImpl) UpdateSecurityGroupRules(addressGroupIdentifier *securitygroup.CloudResourceID,
	ingressRules []*securitygroup.IngressRule, egressRules []*securitygroup.EgressRule) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(addressGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.UpdateSecurityGroupRules(addressGroupIdentifier, ingressRules, egressRules)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *SecurityGroupImpl) DeleteSecurityGroup(addressGroupIdentifier *securitygroup.CloudResourceID, membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(addressGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.DeleteSecurityGroup(addressGroupIdentifier, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *SecurityGroupImpl) GetSecurityGroupSyncChan() <-chan securitygroup.SynchronizationContent {
	retCh := make(chan securitygroup.SynchronizationContent)

	go func() {
		defer close(retCh)

		var wg sync.WaitGroup
		ch := make(chan []securitygroup.SynchronizationContent)
		providerTypes := GetSupportedCloudProviderTypes()

		wg.Add(len(providerTypes))
		go func() {
			wg.Wait()
			close(ch)
		}()

		for _, providerType := range providerTypes {
			cloudInterface, err := GetCloudInterface(providerType)
			if err != nil {
				wg.Done()
				continue
			}

			go func() {
				ch <- cloudInterface.GetEnforcedSecurity()
				wg.Done()
			}()
		}

		for val := range ch {
			for _, sg := range val {
				retCh <- sg
			}
		}
	}()

	return retCh
}
