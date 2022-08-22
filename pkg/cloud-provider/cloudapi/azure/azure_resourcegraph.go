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

	"github.com/Azure/azure-sdk-for-go/services/resourcegraph/mgmt/2021-03-01/resourcegraph"
)

const (
	// error message(s).
	subscriptionIDsNotFoundErrorMsg = "subscription ID(s) required for the query"
	tenantIDsNotFoundErrorMsg       = "tenant ID(s) required for the query"
	locationsNotFoundErrorMsg       = "location/region(s) required for the query"
	vnetIDsNotFoundErrorMsg         = "vnet ID(s) required for the query"
	vmIDsNotFoundErrorMsg           = "vm ID(s) required for the query"
	vmNamesNotFoundErrorMsg         = "vm name(s) required for the query"
	vmIDorNameNotFoundErrorMsg      = "vm ID(s) or name(s) required for the query"
)

// resourceGraph returns resource-graph SDK apiClient.
func (p *azureServiceSdkConfigProvider) resourceGraph() (azureResourceGraphWrapper, error) {
	baseClient := resourcegraph.New()
	baseClient.Authorizer = p.authorizer
	return &azureResourceGraphWrapperImpl{resourceGraphAPIClient: baseClient}, nil
}

func invokeResourceGraphQuery(resourceGraphAPIClient azureResourceGraphWrapper, query *string,
	subscriptions []string) (interface{}, int64, error) {
	requestOptions := resourcegraph.QueryRequestOptions{
		ResultFormat: resourcegraph.ResultFormatObjectArray,
	}

	request := resourcegraph.QueryRequest{
		Subscriptions: &subscriptions,
		Query:         query,
		Options:       &requestOptions,
		Facets:        nil,
	}

	results, queryErr := resourceGraphAPIClient.resources(context.Background(), request)
	if queryErr == nil {
		return results.Data, *results.TotalRecords, nil
	}
	return nil, 0, queryErr
}
