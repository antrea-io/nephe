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
	"sort"
	"strings"

	"antrea.io/nephe/apis/crd/v1alpha1"
)

func convertSelectorToComputeQuery(selector *v1alpha1.CloudEntitySelector, subscriptionIDs []string,
	tenantIDs []string, locations []string) ([]*string, bool) {
	if selector == nil {
		return nil, false
	}
	if selector.Spec.VMSelector == nil {
		return nil, true
	}

	allQueryStrings, err := buildQueries(selector.Spec.VMSelector, subscriptionIDs, tenantIDs, locations)
	if err != nil {
		azurePluginLogger().Error(err, "selector conversion to query failed",
			"selectorName", selector.Name, "selectorNamespace", selector.Namespace)
		return nil, false
	}

	return allQueryStrings, true
}

func buildQueries(vmSelector []v1alpha1.VirtualMachineSelector, subscriptionIDs []string, tenantIDs []string,
	locations []string) ([]*string, error) {
	vpcIDsWithVpcIDOnlyMatches := make(map[string]struct{})
	var vpcIDWithOtherMatches []v1alpha1.VirtualMachineSelector
	var vmIDOnlyMatches []v1alpha1.EntityMatch
	var vmIDAndVMNameMatches []v1alpha1.EntityMatch
	var vmNameOnlyMatches []v1alpha1.EntityMatch

	// vpcMatch contains VpcID and vmMatch contains nil:
	// vpcIDsWithVpcIDOnlyMatches map contains the corresponding vmSelector section.
	// Azure query is created for fetching all virtual machines in the vnet matching VpcID.
	// vpcMatch contains VpcID and vmMatch contains vmID/vmName:
	// vpcIDWithOtherMatches slice contains the corresponding vmSelector section.
	// For each index(EntityMatch) in vmMatch, a query created along with vpcID for fetching
	// instances belonging to the vpcID configured.
	// vpcMatch contains nil and vmMatch contains only vmId:
	// vmIDOnlyMatches slice contains the specific vmMatch section(EntityMatch).
	// Azure query is created to match only vms matching the matchID.
	// vpcMatch contains nil and vmMatch contains only vmName:
	// vmNameOnlyMatches slice contains the specific vmMatch section(EntityMatch).
	// Azure query is created to match only vms matching the matchName.

	for _, match := range vmSelector {
		isVpcIDPresent := false

		networkMatch := match.VpcMatch
		if networkMatch != nil {
			if len(strings.TrimSpace(networkMatch.MatchID)) > 0 {
				isVpcIDPresent = true
			}
		}
		// select all entry found. No need to process any other matches
		if !isVpcIDPresent && len(match.VMMatch) == 0 {
			return nil, nil
		}

		// select all for a vpc ID entry found. keep track of these vpc IDs and skip any other matches with these vpc IDs
		// as match-all overrides any specific (vmID or vmName based) matches
		if isVpcIDPresent && len(match.VMMatch) == 0 {
			vpcIDsWithVpcIDOnlyMatches[networkMatch.MatchID] = struct{}{}
		}

		for _, vmmatch := range match.VMMatch {
			isVMIDPresent := false
			isVMNamePresent := false
			if len(strings.TrimSpace(vmmatch.MatchID)) > 0 {
				isVMIDPresent = true
			}
			if len(strings.TrimSpace(vmmatch.MatchName)) > 0 {
				isVMNamePresent = true
			}

			if isVpcIDPresent && (isVMIDPresent || isVMNamePresent) {
				if _, found := vpcIDsWithVpcIDOnlyMatches[networkMatch.MatchID]; found {
					//vpcID only match supersedes vpcID with other matches
					break
				}
				vpcIDWithOtherMatches = append(vpcIDWithOtherMatches, match)
				// As vmSelector is already added to vpcIDWithOtherMatches, no need to process other vmMatch sections
				break
			}

			// vm id only matches
			if isVMIDPresent && !isVMNamePresent && !isVpcIDPresent {
				vmIDOnlyMatches = append(vmIDOnlyMatches, vmmatch)
			}

			// vm id and vm name matches
			if isVMIDPresent && isVMNamePresent && !isVpcIDPresent {
				vmIDAndVMNameMatches = append(vmIDAndVMNameMatches, vmmatch)
			}

			// vm name only matches
			if isVMNamePresent && !isVMIDPresent && !isVpcIDPresent {
				vmNameOnlyMatches = append(vmNameOnlyMatches, vmmatch)
			}
		}
	}

	azurePluginLogger().Info("selector stats", "VpcIdOnlyMatch", len(vpcIDsWithVpcIDOnlyMatches),
		"VpcIdWithOtherMatches", len(vpcIDWithOtherMatches), "VmIdOnlyMatches", len(vmIDOnlyMatches),
		"VmIdAndVmNameMatches", len(vmIDAndVMNameMatches), "VmNameOnlyMatches", len(vmNameOnlyMatches))

	var allQueries []*string

	vpcIDOnlyQuery, err := buildQueryForVpcIDOnlyMatches(vpcIDsWithVpcIDOnlyMatches, subscriptionIDs, tenantIDs, locations)
	if err != nil {
		return nil, err
	}
	if vpcIDOnlyQuery != nil {
		allQueries = append(allQueries, vpcIDOnlyQuery)
	}

	vpcIDWithOtherQuery, err := buildFilterForVpcIDWithOtherMatches(vpcIDWithOtherMatches, vpcIDsWithVpcIDOnlyMatches,
		subscriptionIDs, tenantIDs, locations)
	if err != nil {
		return nil, err
	}
	allQueries = append(allQueries, vpcIDWithOtherQuery...)

	vmNameOnlyQuery, err := buildQueryForVMNameOnlyMatches(vmNameOnlyMatches, subscriptionIDs, tenantIDs, locations)
	if err != nil {
		return nil, err
	}
	if vmNameOnlyQuery != nil {
		allQueries = append(allQueries, vmNameOnlyQuery)
	}

	vmIDOnlyQuery, err := buildQueryForVMIDOnlyMatches(vmIDOnlyMatches, subscriptionIDs, tenantIDs, locations)
	if err != nil {
		return nil, err
	}
	if vmIDOnlyQuery != nil {
		allQueries = append(allQueries, vmIDOnlyQuery)
	}

	return allQueries, nil
}

func buildQueryForVpcIDOnlyMatches(vpcIDsWithVpcIDOnlyMatches map[string]struct{}, subscriptionIDs []string, tenantIDs []string,
	locations []string) (*string, error) {
	if len(vpcIDsWithVpcIDOnlyMatches) == 0 {
		return nil, nil
	}

	var vpcIDs []string

	for vpcID := range vpcIDsWithVpcIDOnlyMatches {
		vpcIDs = append(vpcIDs, vpcID)
	}

	sort.Slice(vpcIDs, func(i, j int) bool {
		return strings.Compare(vpcIDs[i], vpcIDs[j]) < 0
	})

	return getVMsByVnetIDsMatchQuery(vpcIDs, subscriptionIDs, tenantIDs, locations)
}

func buildQueryForVMNameOnlyMatches(vmNameOnlyMatches []v1alpha1.EntityMatch, subscriptionIDs []string, tenantIDs []string,
	locations []string) (*string, error) {
	if len(vmNameOnlyMatches) == 0 {
		return nil, nil
	}

	var vmNames []string

	for _, vmMatch := range vmNameOnlyMatches {
		vmNames = append(vmNames, vmMatch.MatchName)
	}

	sort.Slice(vmNames, func(i, j int) bool {
		return strings.Compare(vmNames[i], vmNames[j]) < 0
	})

	return getVMsByVMNamesMatchQuery(vmNames, subscriptionIDs, tenantIDs, locations)
}

func buildQueryForVMIDOnlyMatches(vmIDOnlyMatches []v1alpha1.EntityMatch, subscriptionIDs []string, tenantIDs []string,
	locations []string) (*string, error) {
	if len(vmIDOnlyMatches) == 0 {
		return nil, nil
	}

	var vmIDs []string

	for _, vmMatch := range vmIDOnlyMatches {
		vmIDs = append(vmIDs, vmMatch.MatchID)
	}

	sort.Slice(vmIDs, func(i, j int) bool {
		return strings.Compare(vmIDs[i], vmIDs[j]) < 0
	})

	return getVMsByVMIDsMatchQuery(vmIDs, subscriptionIDs, tenantIDs, locations)
}

func buildFilterForVpcIDWithOtherMatches(vpcIDWithOtherMatches []v1alpha1.VirtualMachineSelector,
	vpcIDsWithVpcIDOnlyMatches map[string]struct{}, subscriptionIDs []string, tenantIDs []string,
	locations []string) ([]*string, error) {
	if len(vpcIDWithOtherMatches) == 0 {
		return nil, nil
	}

	var allQueries []*string
	for _, match := range vpcIDWithOtherMatches {
		var vpcIDs []string
		vpcID := match.VpcMatch.MatchID
		vpcIDs = append(vpcIDs, vpcID)
		if _, found := vpcIDsWithVpcIDOnlyMatches[vpcID]; found {
			continue
		}

		for _, vmMatch := range match.VMMatch {
			// Build query for each vpcMatch and a vmMatch combination.
			// In an EntityMatch either vm name or vm id are configured.
			// When both are configured, it is blocked at webhook level.
			var vmIDs []string
			var vmNames []string
			vmID := vmMatch.MatchID
			if len(strings.TrimSpace(vmID)) > 0 {
				vmIDs = append(vmIDs, vmID)
			}

			vmName := vmMatch.MatchName
			if len(strings.TrimSpace(vmName)) > 0 {
				vmNames = append(vmNames, vmName)
			}

			sort.Slice(vmIDs, func(i, j int) bool {
				return strings.Compare(vmIDs[i], vmIDs[j]) < 0
			})
			sort.Slice(vmNames, func(i, j int) bool {
				return strings.Compare(vmNames[i], vmNames[j]) < 0
			})
			queryString, err := getVMsByVnetAndOtherMatchesQuery(vpcIDs, vmNames, vmIDs, subscriptionIDs, tenantIDs, locations)
			if err != nil {
				return nil, err
			}
			allQueries = append(allQueries, queryString)
		}
	}
	return allQueries, nil
}
