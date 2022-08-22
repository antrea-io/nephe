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

	"github.com/mitchellh/mapstructure"
)

type networkInterfaceTable struct {
	ID                          *string
	Name                        *string
	VnetID                      *string
	VirtualMachineID            *string
	NetworkSecurityGroupID      *string
	ApplicationSecurityGroupIDs []*string
	Tags                        []map[string]*string
}

type nwIntfTableQueryParameters struct {
	SubscriptionIDs *string
	TenantIDs       *string
	Locations       *string
	VnetIDs         *string
}

const (
	nwtIntfTableQueryTemplate = "Resources" +
		"| where type =~ 'microsoft.network/networkinterfaces'" +
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
		"| extend virtualMachineID = properties.virtualMachine.id" +
		"| extend networkSecurityGroupID = properties.networkSecurityGroup.id" +
		"| mvexpand ipconfig = properties.ipConfigurations" +
		"| extend applicationSecurityGroup = ipconfig.properties.applicationSecurityGroups" +
		"| extend primary = ipconfig.properties.primary" +
		"| where primary =~ 'true'" +
		"| extend vnetIdArray = array_slice(split(ipconfig.properties.subnet.id, \"/\"), 0, 8)" +
		"| extend vnetId = tolower(strcat_array(vnetIdArray, \"/\"))" +
		"{{ if .VnetIDs }} " +
		"| where vnetId in ({{ .VnetIDs }}) " +
		"{{ end }}" +
		"| mvexpand applicationSecurityGroup" +
		"| extend appId = applicationSecurityGroup.id" +
		"| summarize applicationSecurityGroupIDs=make_list(appId), tags=make_set(tags) by id, name, " +
		"tostring(virtualMachineID), tostring(networkSecurityGroupID), tostring(vnetId)"
)

func getNetworkInterfaceTable(resourceGraphAPIClient azureResourceGraphWrapper, query *string,
	subscriptions []string) ([]*networkInterfaceTable, int64, error) {
	data, count, err := invokeResourceGraphQuery(resourceGraphAPIClient, query, subscriptions)
	if err != nil {
		return nil, 0, err
	}
	if data == nil {
		return []*networkInterfaceTable{}, 0, nil
	}

	var networkInterfaces []*networkInterfaceTable
	networkInterfaceRows := data.([]interface{})
	for _, networkInterfaceRow := range networkInterfaceRows {
		var networkInterface networkInterfaceTable
		err = mapstructure.Decode(networkInterfaceRow, &networkInterface)
		if err != nil {
			return nil, 0, err
		}
		networkInterfaces = append(networkInterfaces, &networkInterface)
	}

	return networkInterfaces, count, nil
}

func getNwIntfsByVnetIDsAndSubscriptionIDsAndTenantIDsAndLocationsMatchQuery(vnetIDs []string, subscriptionIDs []string,
	tenantIDs []string, locations []string) (*string, error) {
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

	queryParams := &nwIntfTableQueryParameters{
		SubscriptionIDs: &commaSeparatedSubscriptionIDs,
		TenantIDs:       &commaSeparatedTenantIDs,
		Locations:       &commaSeparatedLocations,
		VnetIDs:         &commaSeparatedVnetIDs,
	}

	queryString, err := buildNwIntfTableQueryWithParams("getNwIntfsByVnetIDsAndSubscriptionIDsAndTenantIDsAndLocationsMatchQuery",
		queryParams)
	if err != nil {
		return nil, err
	}
	return queryString, nil
}

func buildNwIntfTableQueryWithParams(name string, queryParams *nwIntfTableQueryParameters) (*string, error) {
	var nwIntfTableData bytes.Buffer
	queryTemplate, err := template.New(name).Parse(nwtIntfTableQueryTemplate)
	if err != nil {
		return nil, err
	}

	err = queryTemplate.Execute(&nwIntfTableData, queryParams)
	if err != nil {
		return nil, err
	}

	queryString := nwIntfTableData.String()
	return &queryString, nil
}
