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
	"bytes"
	"fmt"
	"text/template"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2021-03-01/compute"
	"github.com/mitchellh/mapstructure"
)

type virtualMachineTable struct {
	ID                *string
	Name              *string
	Properties        *compute.VirtualMachineProperties
	NetworkInterfaces []*networkInterface
	Tags              map[string]*string
	Status            *string
	VnetID            *string
}
type networkInterface struct {
	ID         *string
	Name       *string
	MacAddress *string
	PrivateIps []*string
	PublicIps  []*string
	Tags       map[string]*string
	VnetID     *string
}

type vmTableQueryParameters struct {
	SubscriptionIDs *string
	TenantIDs       *string
	Locations       *string
	VnetIDs         *string
	VMNames         *string
	VMIDs           *string
}

const (
	vmsTableQueryTemplate = "Resources" +
		"| where type =~ 'microsoft.compute/virtualmachines'" +
		"| extend subscriptionIdLowerCase = tolower(subscriptionId)" +
		"{{ if .SubscriptionIDs }} " +
		"| where subscriptionIdLowerCase in ({{ .SubscriptionIDs }}) " +
		"{{ end }}" +
		"| extend tenantIdLowerCase = tolower(tenantId)" +
		"{{ if .TenantIDs }} " +
		"| where tenantIdLowerCase in ({{ .TenantIDs }}) " +
		"{{ end }}" +
		"| extend locationLowerCase = tolower(location)" +
		"{{ if .Locations }} " +
		"| where locationLowerCase in ({{ .Locations }})" +
		"{{ end }}" +
		"| extend name = tolower(name)" +
		"{{ if .VMNames}} " +
		"| where name in ({{ .VMNames }})" +
		"{{ end }}" +
		"| extend id = tolower(id)" +
		"{{ if .VMIDs}} " +
		"| where id in ({{ .VMIDs }})" +
		"{{ end }}" +
		"| mvexpand nic = properties.networkProfile.networkInterfaces" +
		"| extend nicId = tolower(tostring(nic.id))" +
		"| join kind = innerunique (" +
		"	Resources" +
		"	| where type =~ 'microsoft.network/networkinterfaces'" +
		"	| extend macAddress = properties.macAddress" +
		"	| mvexpand ipconfig = properties.ipConfigurations" +
		"	| extend vnetIdArray = array_slice(split(ipconfig.properties.subnet.id, \"/\"), 0, 8)" +
		"	| extend vnetId = tolower(strcat_array(vnetIdArray, \"/\"))" +
		"	{{ if .VnetIDs }} " +
		"	| where vnetId in ({{ .VnetIDs }}) " +
		"	{{ end }}" +
		"	| extend publicIpId = tolower(tostring(ipconfig.properties.publicIPAddress.id))" +
		"	| extend nicPrivateIp = ipconfig.properties.privateIPAddress" +
		"	| join kind = leftouter (" +
		"		Resources" +
		"		| where type =~ 'microsoft.network/publicipaddresses'" +
		"		| project publicIpId = tolower(id), nicPublicIp = properties.ipAddress" +
		"	) on publicIpId" +
		"	| summarize nicTags = any(tags), macAddress = any(macAddress), vnetId = any(vnetId), " +
		"nicPublicIps = make_list(nicPublicIp), nicPrivateIps = make_list(nicPrivateIp) by id, name" +
		"	| project nicId = tolower(id), nicName = name, nicPublicIps, nicPrivateIps, vnetId, macAddress, nicTags" +
		") on nicId" +
		"| extend networkInterfaceDetails = pack(\"id\", nicId, \"name\", nicName, \"macAddress\", macAddress, \"privateIps\"," +
		"nicPrivateIps, \"publicIps\", nicPublicIps, \"tags\", nicTags, \"vnetId\", vnetId)" +
		"| summarize vnetId = any(vnetId), properties = make_bag(properties), tags = make_bag(tags), " +
		"networkInterfaces = make_list(networkInterfaceDetails) by id, name" +
		"| project id, name, properties, status=properties.extended.instanceView.powerState.code, networkInterfaces, tags, vnetId"
)

func getVirtualMachineTable(resourceGraphAPIClient azureResourceGraphWrapper, query *string,
	subscriptions []string) ([]*virtualMachineTable, int64, error) {
	data, count, err := invokeResourceGraphQuery(resourceGraphAPIClient, query, subscriptions)
	if err != nil {
		return nil, 0, err
	}
	if data == nil {
		return []*virtualMachineTable{}, 0, nil
	}

	var virtualMachines []*virtualMachineTable
	virtualMachineRows := data.([]interface{})
	for _, virtualMachineRow := range virtualMachineRows {
		var virtualMachine virtualMachineTable
		err = mapstructure.Decode(virtualMachineRow, &virtualMachine)
		if err != nil {
			return nil, 0, err
		}
		virtualMachines = append(virtualMachines, &virtualMachine)
	}

	return virtualMachines, count, nil
}

func getVMsByVnetIDsMatchQuery(vnetIDs []string, subscriptionIDs []string, tenantIDs []string,
	locations []string) (*string, error) {
	commaSeparatedVnetIDs := convertStrSliceToLowercaseCommaSeparatedStr(vnetIDs)
	if len(commaSeparatedVnetIDs) == 0 {
		return nil, fmt.Errorf(vnetIDsNotFoundErrorMsg)
	}

	commaSeparatedSubscriptionIDs := convertStrSliceToLowercaseCommaSeparatedStr(subscriptionIDs)
	if len(commaSeparatedSubscriptionIDs) == 0 {
		return nil, fmt.Errorf(subscriptionIDsNotFoundErrorMsg)
	}

	commaSeparatedTenantIDs := convertStrSliceToLowercaseCommaSeparatedStr(tenantIDs)
	if len(commaSeparatedTenantIDs) == 0 {
		return nil, fmt.Errorf(tenantIDsNotFoundErrorMsg)
	}

	commaSeparatedLocations := convertStrSliceToLowercaseCommaSeparatedStr(locations)
	if len(commaSeparatedLocations) == 0 {
		return nil, fmt.Errorf(locationsNotFoundErrorMsg)
	}

	queryParams := &vmTableQueryParameters{
		SubscriptionIDs: &commaSeparatedSubscriptionIDs,
		TenantIDs:       &commaSeparatedTenantIDs,
		Locations:       &commaSeparatedLocations,
		VnetIDs:         &commaSeparatedVnetIDs,
	}

	queryString, err := buildVmsTableQueryWithParams("getVMsByVnetIDsMatchQuery", queryParams)
	if err != nil {
		return nil, err
	}
	return queryString, nil
}

func getVMsByVMNamesMatchQuery(vmNames []string, subscriptionIDs []string, tenantIDs []string,
	locations []string) (*string, error) {
	commaSeparatedVMNames := convertStrSliceToLowercaseCommaSeparatedStr(vmNames)
	if len(commaSeparatedVMNames) == 0 {
		return nil, fmt.Errorf(vmNamesNotFoundErrorMsg)
	}

	commaSeparatedSubscriptionIDs := convertStrSliceToLowercaseCommaSeparatedStr(subscriptionIDs)
	if len(commaSeparatedSubscriptionIDs) == 0 {
		return nil, fmt.Errorf(subscriptionIDsNotFoundErrorMsg)
	}

	commaSeparatedTenantIDs := convertStrSliceToLowercaseCommaSeparatedStr(tenantIDs)
	if len(commaSeparatedTenantIDs) == 0 {
		return nil, fmt.Errorf(tenantIDsNotFoundErrorMsg)
	}

	commaSeparatedLocations := convertStrSliceToLowercaseCommaSeparatedStr(locations)
	if len(commaSeparatedLocations) == 0 {
		return nil, fmt.Errorf(locationsNotFoundErrorMsg)
	}

	queryParams := &vmTableQueryParameters{
		SubscriptionIDs: &commaSeparatedSubscriptionIDs,
		TenantIDs:       &commaSeparatedTenantIDs,
		Locations:       &commaSeparatedLocations,
		VMNames:         &commaSeparatedVMNames,
	}

	queryString, err := buildVmsTableQueryWithParams("getVMsByVMNamesMatchQuery", queryParams)
	if err != nil {
		return nil, err
	}
	return queryString, nil
}

func getVMsByVMIDsMatchQuery(vmIDs []string, subscriptionIDs []string, tenantIDs []string,
	locations []string) (*string, error) {
	commaSeparatedVMIDs := convertStrSliceToLowercaseCommaSeparatedStr(vmIDs)
	if len(commaSeparatedVMIDs) == 0 {
		return nil, fmt.Errorf(vmIDsNotFoundErrorMsg)
	}

	commaSeparatedSubscriptionIDs := convertStrSliceToLowercaseCommaSeparatedStr(subscriptionIDs)
	if len(commaSeparatedSubscriptionIDs) == 0 {
		return nil, fmt.Errorf(subscriptionIDsNotFoundErrorMsg)
	}

	commaSeparatedTenantIDs := convertStrSliceToLowercaseCommaSeparatedStr(tenantIDs)
	if len(commaSeparatedTenantIDs) == 0 {
		return nil, fmt.Errorf(tenantIDsNotFoundErrorMsg)
	}

	commaSeparatedLocations := convertStrSliceToLowercaseCommaSeparatedStr(locations)
	if len(commaSeparatedLocations) == 0 {
		return nil, fmt.Errorf(locationsNotFoundErrorMsg)
	}

	queryParams := &vmTableQueryParameters{
		SubscriptionIDs: &commaSeparatedSubscriptionIDs,
		TenantIDs:       &commaSeparatedTenantIDs,
		Locations:       &commaSeparatedLocations,
		VMIDs:           &commaSeparatedVMIDs,
	}

	queryString, err := buildVmsTableQueryWithParams("getVMsByVMIDsMatchQuery", queryParams)
	if err != nil {
		return nil, err
	}
	return queryString, nil
}

func getVMsBySubscriptionIDsAndTenantIDsAndLocationsMatchQuery(subscriptionIDs []string, tenantIDs []string,
	locations []string) (*string, error) {
	commaSeparatedSubscriptionIDs := convertStrSliceToLowercaseCommaSeparatedStr(subscriptionIDs)
	if len(commaSeparatedSubscriptionIDs) == 0 {
		return nil, fmt.Errorf(subscriptionIDsNotFoundErrorMsg)
	}

	commaSeparatedTenantIDs := convertStrSliceToLowercaseCommaSeparatedStr(tenantIDs)
	if len(commaSeparatedTenantIDs) == 0 {
		return nil, fmt.Errorf(tenantIDsNotFoundErrorMsg)
	}

	commaSeparatedLocations := convertStrSliceToLowercaseCommaSeparatedStr(locations)
	if len(commaSeparatedLocations) == 0 {
		return nil, fmt.Errorf(locationsNotFoundErrorMsg)
	}

	queryParams := &vmTableQueryParameters{
		SubscriptionIDs: &commaSeparatedSubscriptionIDs,
		TenantIDs:       &commaSeparatedTenantIDs,
		Locations:       &commaSeparatedLocations,
	}

	queryString, err := buildVmsTableQueryWithParams("getVMsBySubscriptionIDsAndTenantIDsAndLocationsMatchQuery", queryParams)
	if err != nil {
		return nil, err
	}
	return queryString, nil
}

func getVMsByVnetAndOtherMatchesQuery(vnetIDs []string, vmNames []string, vmIDs []string, subscriptionIDs []string,
	tenantIDs []string, locations []string) (*string, error) {
	var queryParams *vmTableQueryParameters
	commaSeparatedSubscriptionIDs := convertStrSliceToLowercaseCommaSeparatedStr(subscriptionIDs)
	if len(commaSeparatedSubscriptionIDs) == 0 {
		return nil, fmt.Errorf(subscriptionIDsNotFoundErrorMsg)
	}

	commaSeparatedTenantIDs := convertStrSliceToLowercaseCommaSeparatedStr(tenantIDs)
	if len(commaSeparatedTenantIDs) == 0 {
		return nil, fmt.Errorf(tenantIDsNotFoundErrorMsg)
	}

	commaSeparatedLocations := convertStrSliceToLowercaseCommaSeparatedStr(locations)
	if len(commaSeparatedLocations) == 0 {
		return nil, fmt.Errorf(locationsNotFoundErrorMsg)
	}

	commaSeparatedVnetIDs := convertStrSliceToLowercaseCommaSeparatedStr(vnetIDs)
	if len(commaSeparatedVnetIDs) == 0 {
		return nil, fmt.Errorf(vnetIDsNotFoundErrorMsg)
	}

	commaSeparatedVMIDs := convertStrSliceToLowercaseCommaSeparatedStr(vmIDs)

	commaSeparatedVMNames := convertStrSliceToLowercaseCommaSeparatedStr(vmNames)

	if len(commaSeparatedVMIDs) == 0 && len(commaSeparatedVMNames) == 0 {
		return nil, fmt.Errorf(vmIDorNameNotFoundErrorMsg)
	}
	if len(commaSeparatedVMNames) == 0 {
		queryParams = &vmTableQueryParameters{
			SubscriptionIDs: &commaSeparatedSubscriptionIDs,
			TenantIDs:       &commaSeparatedTenantIDs,
			Locations:       &commaSeparatedLocations,
			VMIDs:           &commaSeparatedVMIDs,
			VnetIDs:         &commaSeparatedVnetIDs,
		}
	} else {
		queryParams = &vmTableQueryParameters{
			SubscriptionIDs: &commaSeparatedSubscriptionIDs,
			TenantIDs:       &commaSeparatedTenantIDs,
			Locations:       &commaSeparatedLocations,
			VMNames:         &commaSeparatedVMNames,
			VnetIDs:         &commaSeparatedVnetIDs,
		}
	}

	queryString, err := buildVmsTableQueryWithParams("getVMsByVMIDsMatchQuery", queryParams)
	if err != nil {
		return nil, err
	}
	return queryString, nil
}

func buildVmsTableQueryWithParams(name string, queryParams *vmTableQueryParameters) (*string, error) {
	var vmTableData bytes.Buffer
	queryTemplate, err := template.New(name).Parse(vmsTableQueryTemplate)
	if err != nil {
		return nil, err
	}

	err = queryTemplate.Execute(&vmTableData, queryParams)
	if err != nil {
		return nil, err
	}

	queryString := vmTableData.String()
	return &queryString, nil
}
