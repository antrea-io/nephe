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

	cloudcommon "antrea.io/nephe/pkg/cloudprovider/cloudapi/common"
	"antrea.io/nephe/pkg/cloudprovider/securitygroup"
)

type SecurityGroupImpl struct{}

func init() {
	securitygroup.CloudSecurityGroup = &SecurityGroupImpl{}
}

// getCloudInterfaceForCloudResource fetches Cloud Interface using Cloud Provider Type present in CloudResource.
func getCloudInterfaceForCloudResource(securityGroupIdentifier *securitygroup.CloudResource) (cloudcommon.CloudInterface, error) {
	providerType := cloudcommon.ProviderType(securityGroupIdentifier.CloudProvider)
	cloudInterface, err := GetCloudInterface(providerType)
	if err != nil {
		corePluginLogger().Error(err, "get cloud interface failed", "providerType", providerType)
		return nil, fmt.Errorf("virtual private cloud [%v] not managed by supported clouds", securityGroupIdentifier.Vpc)
	}
	return cloudInterface, nil
}

func (sg *SecurityGroupImpl) CreateSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(securityGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		_, err = cloudInterface.CreateSecurityGroup(securityGroupIdentifier, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *SecurityGroupImpl) UpdateSecurityGroupRules(appliedToGroupIdentifier *securitygroup.CloudResource,
	addRules, rmRules, allRules []*securitygroup.CloudRule) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(appliedToGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.UpdateSecurityGroupRules(appliedToGroupIdentifier, addRules, rmRules, allRules)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *SecurityGroupImpl) UpdateSecurityGroupMembers(securityGroupIdentifier *securitygroup.CloudResource,
	members []*securitygroup.CloudResource, membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(securityGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.UpdateSecurityGroupMembers(securityGroupIdentifier, members, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *SecurityGroupImpl) DeleteSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(securityGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.DeleteSecurityGroup(securityGroupIdentifier, membershipOnly)
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
