// Copyright 2023 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
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
	"strings"
	"text/template"

	armsm "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/mitchellh/mapstructure"
)

type nsgTable struct {
	ID            *string
	Name          *string
	ResourceGroup *string
	Location      *string
	Properties    *armsm.SecurityGroupPropertiesFormat
	VnetID        *string
}

type nsgTableQueryParameters struct {
	VnetIDs *string
}

const (
	nsgTableQueryTemplate = "resources" +
		"| where type =~ 'microsoft.network/networkInterfaces' and isnotnull(properties.networkSecurityGroup)" +
		"| mvexpand ipconfig = properties.ipConfigurations" +
		"| extend vnetIdArray = array_slice(split(ipconfig.properties.subnet.id, \"/\"), 0, 8)" +
		"| extend vnetId = tolower(strcat_array(vnetIdArray, \"/\"))" +
		"{{ if .VnetIDs }} " +
		"| where vnetId in ({{ .VnetIDs }}) " +
		"{{ end }}" +
		"| extend ids = tostring(properties.networkSecurityGroup.id)" +
		"| join kind=innerunique  (" +
		"resources" +
		"|  where type =~ 'microsoft.network/networksecuritygroups'" +
		"|  extend ids = tostring(id) ) on ids" +
		"|  project id = id1, name = name1, properties = properties1, location = location1, resourceGroup = resourceGroup1, vnetId"
)

func getNsgTable(resourceGraphAPIClient azureResourceGraphWrapper, query *string,
	subscriptions []*string) ([]*nsgTable, int64, error) {
	data, count, err := invokeResourceGraphQuery(resourceGraphAPIClient, query, subscriptions)
	if err != nil {
		return nil, 0, err
	}
	if data == nil {
		return []*nsgTable{}, 0, nil
	}

	var nsgs []*nsgTable
	for _, nsgRow := range data {
		var nsg nsgTable
		err = mapstructure.Decode(nsgRow, &nsg)
		if err != nil {
			return nil, 0, err
		}
		nsgs = append(nsgs, &nsg)
	}

	return nsgs, count, nil
}

func buildNsgTableQueryWithParams(name string, queryParams *nsgTableQueryParameters) (*string, error) {
	var nsgTableData bytes.Buffer
	queryTemplate, err := template.New(name).Parse(nsgTableQueryTemplate)
	if err != nil {
		return nil, err
	}

	err = queryTemplate.Execute(&nsgTableData, queryParams)
	if err != nil {
		return nil, err
	}

	queryString := nsgTableData.String()
	return &queryString, nil
}

func getNsgByVnetIDsMatchQuery(vnetIDs map[string]struct{}) (*string, error) {
	commaSeparatedVnetIDs := convertVnetStrSliceToLowercaseCommaSeparatedStr(vnetIDs)
	if len(commaSeparatedVnetIDs) == 0 {
		return nil, fmt.Errorf(vnetIDsNotFoundErrorMsg)
	}

	queryParams := &nsgTableQueryParameters{
		VnetIDs: &commaSeparatedVnetIDs,
	}

	queryString, err := buildNsgTableQueryWithParams("getNsgByVnetIDsMatchQuery", queryParams)
	if err != nil {
		return nil, err
	}
	return queryString, nil
}

func convertVnetStrSliceToLowercaseCommaSeparatedStr(vnetIDs map[string]struct{}) string {
	var lowerCase []string
	for vnetID := range vnetIDs {
		if len(vnetID) > 0 {
			lowerCase = append(lowerCase, strings.ToLower(vnetID))
		}
	}
	if len(lowerCase) == 0 {
		return ""
	}
	tokens := strings.Split(fmt.Sprintf("%q", lowerCase), " ")
	return strings.Trim(strings.Join(tokens, ", "), "[]")
}
