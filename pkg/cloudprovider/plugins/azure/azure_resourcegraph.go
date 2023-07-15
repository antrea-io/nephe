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
	"math"

	resourcegraph "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"

	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
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
	baseClient, err := resourcegraph.NewClient(p.cred, nil)
	if err != nil {
		return nil, err
	}

	return &azureResourceGraphWrapperImpl{resourceGraphAPIClient: baseClient}, nil
}

func invokeResourceGraphQuery(resourceGraphAPIClient azureResourceGraphWrapper, query *string,
	subscriptions []*string) ([]interface{}, int64, error) {
	var data []interface{}
	var currentRecords int64
	var totalRecords int64 = math.MaxInt64
	var pageSize = int32(internal.MaxCloudResourceResponse)

	resultFmt := resourcegraph.ResultFormatObjectArray
	requestOptions := resourcegraph.QueryRequestOptions{
		ResultFormat: &resultFmt,
		Top:          &pageSize,
	}

	request := resourcegraph.QueryRequest{
		Subscriptions: subscriptions,
		Query:         query,
		Options:       &requestOptions,
		Facets:        nil,
	}

	// invoke resource graph API till all pages from response are fetched.
	for currentRecords < totalRecords {
		results, queryErr := resourceGraphAPIClient.resources(context.Background(), request)
		if queryErr == nil {
			if results.Data != nil {
				data = append(data, results.Data.([]interface{})...)
			}
			currentRecords += *results.Count
			totalRecords = *results.TotalRecords
			request.Options.SkipToken = results.SkipToken
		} else {
			return nil, 0, queryErr
		}
	}

	return data, currentRecords, nil
}
