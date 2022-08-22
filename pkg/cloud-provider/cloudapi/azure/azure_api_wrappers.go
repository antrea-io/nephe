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

package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/azure-sdk-for-go/services/resourcegraph/mgmt/2021-03-01/resourcegraph"
	"github.com/Azure/go-autorest/autorest"
)

type azureNwIntfWrapper interface {
	createOrUpdate(ctx context.Context, resourceGroupName string, networkInterfaceName string,
		parameters network.Interface) (network.Interface, error)
	listAllComplete(ctx context.Context) ([]network.Interface, error)
}
type azureNwIntfWrapperImpl struct {
	nwIntfAPIClient network.InterfacesClient
}

func (nwIntf *azureNwIntfWrapperImpl) createOrUpdate(ctx context.Context, resourceGroupName string, networkIntfName string,
	parameters network.Interface) (network.Interface, error) {
	var nwInterface network.Interface
	nwIntfClient := nwIntf.nwIntfAPIClient
	future, err := nwIntfClient.CreateOrUpdate(ctx, resourceGroupName, networkIntfName, parameters)
	if err != nil {
		return nwInterface, fmt.Errorf("cannot create %v, reason: %v", networkIntfName, err)
	}

	err = future.WaitForCompletionRef(ctx, nwIntfClient.Client)
	if err != nil {
		return nwInterface, fmt.Errorf("cannot get network-interface create or update future response: %v", err)
	}

	return future.Result(nwIntfClient)
}
func (nwIntf *azureNwIntfWrapperImpl) listAllComplete(ctx context.Context) ([]network.Interface, error) {
	listResultIterator, err := nwIntf.nwIntfAPIClient.ListAllComplete(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list network interfaces, reason %v", err)
	}

	var networkInterfaces []network.Interface
	for ; listResultIterator.NotDone(); err = listResultIterator.NextWithContext(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate list of network interface, reason %v", err)
		}
		networkInterfaces = append(networkInterfaces, listResultIterator.Value())
	}

	return networkInterfaces, nil
}

type azureNsgWrapper interface {
	createOrUpdate(ctx context.Context, resourceGroupName string, networkSecurityGroupName string,
		parameters network.SecurityGroup) (nsg network.SecurityGroup, err error)
	get(ctx context.Context, resourceGroupName string, networkSecurityGroupName string, expand string) (result network.SecurityGroup,
		err error)
	delete(ctx context.Context, resourceGroupName string, networkSecurityGroupName string) error
	listAllComplete(ctx context.Context) ([]network.SecurityGroup, error)
}
type azureNsgWrapperImpl struct {
	nsgAPIClient network.SecurityGroupsClient
}

func (sg *azureNsgWrapperImpl) createOrUpdate(ctx context.Context, resourceGroupName string, networkSecurityGroupName string,
	parameters network.SecurityGroup) (network.SecurityGroup, error) {
	var nsg network.SecurityGroup
	nsgClient := sg.nsgAPIClient
	future, err := nsgClient.CreateOrUpdate(ctx, resourceGroupName, networkSecurityGroupName, parameters)
	if err != nil {
		return nsg, fmt.Errorf("cannot create %v, reason: %v", networkSecurityGroupName, err)
	}

	err = future.WaitForCompletionRef(ctx, nsgClient.Client)
	if err != nil {
		return nsg, fmt.Errorf("cannot get nsg create or update future response: %v", err)
	}

	return future.Result(nsgClient)
}
func (sg *azureNsgWrapperImpl) get(ctx context.Context, resourceGroupName string, networkSecurityGroupName string,
	expand string) (result network.SecurityGroup, err error) {
	return sg.nsgAPIClient.Get(ctx, resourceGroupName, networkSecurityGroupName, expand)
}
func (sg *azureNsgWrapperImpl) delete(ctx context.Context, resourceGroupName string, networkSecurityGroupName string) error {
	nsgClient := sg.nsgAPIClient
	future, err := nsgClient.Delete(ctx, resourceGroupName, networkSecurityGroupName)
	if err != nil {
		detailError := err.(autorest.DetailedError)
		if detailError.StatusCode != 404 {
			return fmt.Errorf("cannot delete nsg %v, reason: %v", networkSecurityGroupName, err)
		}
		return nil
	}

	err = future.WaitForCompletionRef(ctx, nsgClient.Client)
	if err != nil {
		return fmt.Errorf("cannot get nsg delete future response: %v", err)
	}

	return nil
}
func (sg *azureNsgWrapperImpl) listAllComplete(ctx context.Context) ([]network.SecurityGroup, error) {
	listResultIterator, err := sg.nsgAPIClient.ListAllComplete(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list network security groups, reason %v", err)
	}

	var nsgs []network.SecurityGroup
	for ; listResultIterator.NotDone(); err = listResultIterator.NextWithContext(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate list of security groups, reason %v", err)
		}
		nsgs = append(nsgs, listResultIterator.Value())
	}

	return nsgs, nil
}

type azureAsgWrapper interface {
	createOrUpdate(ctx context.Context, resourceGroupName string, applicationSecurityGroupName string,
		parameters network.ApplicationSecurityGroup) (network.ApplicationSecurityGroup, error)
	get(ctx context.Context, resourceGroupName string, applicationSecurityGroupName string) (network.ApplicationSecurityGroup, error)
	listComplete(ctx context.Context, resourceGroupName string) ([]network.ApplicationSecurityGroup, error)
	listAllComplete(ctx context.Context) ([]network.ApplicationSecurityGroup, error)
	delete(ctx context.Context, resourceGroupName string, applicationSecurityGroupName string) error
}
type azureAsgWrapperImpl struct {
	asgAPIClient network.ApplicationSecurityGroupsClient
}

func (asg *azureAsgWrapperImpl) createOrUpdate(ctx context.Context, resourceGroupName string,
	applicationSecurityGroupName string, parameters network.ApplicationSecurityGroup) (network.ApplicationSecurityGroup, error) {
	var appsg network.ApplicationSecurityGroup
	asgClient := asg.asgAPIClient
	future, err := asgClient.CreateOrUpdate(ctx, resourceGroupName, applicationSecurityGroupName, parameters)
	if err != nil {
		return appsg, fmt.Errorf("cannot create asg %v, reason: %v", applicationSecurityGroupName, err)
	}

	err = future.WaitForCompletionRef(ctx, asgClient.Client)
	if err != nil {
		return appsg, fmt.Errorf("cannot get asg create or update future response: %v", err)
	}

	return future.Result(asgClient)
}
func (asg *azureAsgWrapperImpl) get(ctx context.Context, resourceGroupName string,
	applicationSecurityGroupName string) (network.ApplicationSecurityGroup, error) {
	return asg.asgAPIClient.Get(ctx, resourceGroupName, applicationSecurityGroupName)
}
func (asg *azureAsgWrapperImpl) listComplete(ctx context.Context, resourceGroupName string) ([]network.ApplicationSecurityGroup, error) {
	listResultIterator, err := asg.asgAPIClient.ListComplete(ctx, resourceGroupName)
	if err != nil {
		return nil, fmt.Errorf("failed to list of asgs for resource-group: %v, reason %v", resourceGroupName, err)
	}

	var asgs []network.ApplicationSecurityGroup
	for ; listResultIterator.NotDone(); err = listResultIterator.NextWithContext(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate list of asgs for resource-group: %v, reason %v", resourceGroupName, err)
		}
		asgs = append(asgs, listResultIterator.Value())
	}

	return asgs, nil
}
func (asg *azureAsgWrapperImpl) listAllComplete(ctx context.Context) ([]network.ApplicationSecurityGroup, error) {
	listResultIterator, err := asg.asgAPIClient.ListAllComplete(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list application security groups, reason %v", err)
	}

	var asgs []network.ApplicationSecurityGroup
	for ; listResultIterator.NotDone(); err = listResultIterator.NextWithContext(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate list of application security groups, reason %v", err)
		}
		asgs = append(asgs, listResultIterator.Value())
	}

	return asgs, nil
}
func (asg *azureAsgWrapperImpl) delete(ctx context.Context, resourceGroupName string, applicationSecurityGroupName string) error {
	asgClient := asg.asgAPIClient
	future, err := asgClient.Delete(ctx, resourceGroupName, applicationSecurityGroupName)
	if err != nil {
		detailError := err.(autorest.DetailedError)
		if detailError.StatusCode != 404 {
			return fmt.Errorf("cannot delete asg %v, reason: %v", applicationSecurityGroupName, err)
		}
		return nil
	}

	err = future.WaitForCompletionRef(ctx, asgClient.Client)
	if err != nil {
		return fmt.Errorf("cannot get asg delete future response: %v", err)
	}

	return nil
}

type azureResourceGraphWrapper interface {
	resources(ctx context.Context, query resourcegraph.QueryRequest) (result resourcegraph.QueryResponse, err error)
}
type azureResourceGraphWrapperImpl struct {
	resourceGraphAPIClient resourcegraph.BaseClient
}

func (rg *azureResourceGraphWrapperImpl) resources(ctx context.Context, query resourcegraph.QueryRequest) (
	result resourcegraph.QueryResponse, err error) {
	return rg.resourceGraphAPIClient.Resources(ctx, query)
}

type azureVirtualNetworksWrapper interface {
	listAllComplete(ctx context.Context) ([]network.VirtualNetwork, error)
}
type azureVirtualNetworksWrapperImpl struct {
	virtualNetworksClient network.VirtualNetworksClient
}

func (vnet *azureVirtualNetworksWrapperImpl) listAllComplete(ctx context.Context) ([]network.VirtualNetwork, error) {
	listResultIterator, err := vnet.virtualNetworksClient.ListAllComplete(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list virtual networks, reason %v", err)
	}

	var VNListResultIterators []network.VirtualNetwork
	for ; listResultIterator.NotDone(); err = listResultIterator.NextWithContext(ctx) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate list of virtual networks, reason %v", err)
		}
		VNListResultIterators = append(VNListResultIterators, listResultIterator.Value())
	}

	return VNListResultIterators, nil
}
