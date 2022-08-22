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

package aws

import (
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	"antrea.io/nephe/apis/crd/v1alpha1"
)

// aws instance resource filter keys.
const (
	awsFilterKeyVPCID         = "vpc-id"
	awsFilterKeyVMID          = "instance-id"
	awsFilterKeyVMName        = "tag:Name"
	awsFilterKeyGroupName     = "group-name"
	awsFilterKeyInstanceState = "instance-state-code"

	// Not supported by aws, internal use only.
	awsCustomFilterKeyVPCName = "vpc-name"
)

var (
	awsInstanceStateRunningCode      = "16"
	awsInstanceStateShuttingDownCode = "32"
	awsInstanceStateStoppingCode     = "64"
	awsInstanceStateStoppedCode      = "80"
)

// convertSelectorToEC2InstanceFilters converts vm selector to aws filters.
func convertSelectorToEC2InstanceFilters(selector *v1alpha1.CloudEntitySelector) ([][]*ec2.Filter, bool) {
	if selector == nil {
		return nil, false
	}
	if selector.Spec.VMSelector == nil {
		return nil, true
	}

	return buildEc2Filters(selector.Spec.VMSelector), true
}

// buildEc2Filters builds ec2 filters for VirtualMachineSelector.
func buildEc2Filters(vmSelector []v1alpha1.VirtualMachineSelector) [][]*ec2.Filter {
	vpcIDsWithVpcIDOnlyMatches := make(map[string]struct{})
	var vpcIDWithOtherMatches []v1alpha1.VirtualMachineSelector
	var vmIDOnlyMatches []v1alpha1.EntityMatch
	var vmNameOnlyMatches []v1alpha1.EntityMatch
	var vpcNameOnlyMatches []v1alpha1.VirtualMachineSelector

	// vpcMatch contains VpcID and vmMatch contains nil:
	// vpcIDsWithVpcIDOnlyMatches map contains the corresponding vmSelector section.
	// ec2.Filter is created for fetching all virtual machines in the vpc.
	// vpcMatch contains VpcID and vmMatch contains vmID/vmName:
	// vpcIDWithOtherMatches slice contains the corresponding vmSelector section.
	// As vmMatch is a slice, it can contain multiple sections(with matchID/matchName),
	// hence ec2.Filter is created for each combination of vpcID and vmMatch section
	// to select matching vms(using vmMatch match criteria) in the vpcID configured.
	// vpcMatch contains nil and vmMatch contains only vmId:
	// vmIDOnlyMatches slice contains the specific vmMatch section(EntityMatch).
	// ec2.Filter is created to match only vms matching the matchID.
	// vpcMatch contains nil and vmMatch contains only vmName:
	// vmNameOnlyMatches slice contains the specific vmMatch section(EntityMatch).
	// ec2.Filter is created to match only vms matching the matchName.

	for _, match := range vmSelector {
		isVpcIDPresent := false
		isVpcNamePresent := false

		networkMatch := match.VpcMatch
		if networkMatch != nil {
			if len(strings.TrimSpace(networkMatch.MatchID)) > 0 {
				isVpcIDPresent = true
			}
			if len(strings.TrimSpace(networkMatch.MatchName)) > 0 {
				isVpcNamePresent = true
			}
		}

		// select all entry found. No need to process any other matches.
		if !isVpcIDPresent && len(match.VMMatch) == 0 && !isVpcNamePresent {
			return nil
		}

		// select all for a vpc ID entry found. keep track of these vpc IDs and skip any other matches with these vpc IDs
		// as match-all overrides any specific (vmID or vmName based) matches.
		if isVpcIDPresent && len(match.VMMatch) == 0 {
			vpcIDsWithVpcIDOnlyMatches[networkMatch.MatchID] = struct{}{}
		}

		// vpc name only matches.
		if isVpcNamePresent && len(match.VMMatch) == 0 && !isVpcIDPresent {
			vpcNameOnlyMatches = append(vpcNameOnlyMatches, match)
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
				//vpcID only match supersedes vpcID with other matches
				if _, found := vpcIDsWithVpcIDOnlyMatches[networkMatch.MatchID]; found {
					break
				}
				vpcIDWithOtherMatches = append(vpcIDWithOtherMatches, match)
				// As vmSelector is already added to vpcIDWithOtherMatches, no need to process other vmMatch sections
				break
			}

			// vm id only matches.
			if isVMIDPresent && !isVMNamePresent && !isVpcIDPresent {
				vmIDOnlyMatches = append(vmIDOnlyMatches, vmmatch)
			}

			// vm name only matches.
			if isVMNamePresent && !isVMIDPresent && !isVpcIDPresent {
				vmNameOnlyMatches = append(vmNameOnlyMatches, vmmatch)
			}
		}
	}

	awsPluginLogger().Info("selector stats", "VpcIdOnlyMatch", len(vpcIDsWithVpcIDOnlyMatches),
		"VpcIdWithOtherMatches", len(vpcIDWithOtherMatches), "VmIdOnlyMatches", len(vmIDOnlyMatches),
		"VmNameOnlyMatches", len(vmNameOnlyMatches), "VpcNameOnlyMatches", len(vpcNameOnlyMatches))

	var allEc2Filters [][]*ec2.Filter

	vpcIDOnlyEc2Filter := buildAwsEc2FilterForVpcIDOnlyMatches(vpcIDsWithVpcIDOnlyMatches)
	if vpcIDOnlyEc2Filter != nil {
		vpcIDOnlyEc2Filter = append(vpcIDOnlyEc2Filter, buildEc2FilterForValidInstanceStates())
		allEc2Filters = append(allEc2Filters, vpcIDOnlyEc2Filter)
	}

	vpcIDWithOtherEc2Filter := buildAwsEc2FilterForVpcIDWithOtherMatches(vpcIDWithOtherMatches, vpcIDsWithVpcIDOnlyMatches)
	if vpcIDWithOtherEc2Filter != nil {
		allEc2Filters = append(allEc2Filters, vpcIDWithOtherEc2Filter...)
	}

	vmIDOnlyEc2Filter := buildAwsEc2FilterForVMIDOnlyMatches(vmIDOnlyMatches)
	if vmIDOnlyEc2Filter != nil {
		allEc2Filters = append(allEc2Filters, vmIDOnlyEc2Filter)
	}

	vmNameOnlyEc2Filter := buildAwsEc2FilterForVMNameOnlyMatches(vmNameOnlyMatches)
	if vmNameOnlyEc2Filter != nil {
		allEc2Filters = append(allEc2Filters, vmNameOnlyEc2Filter)
	}

	vpcNameOnlyEc2Filter := buildAwsEc2FilterForVPCNameOnlyMatches(vpcNameOnlyMatches)
	if vpcNameOnlyEc2Filter != nil {
		allEc2Filters = append(allEc2Filters, vpcNameOnlyEc2Filter)
	}
	return allEc2Filters
}

func buildAwsEc2FilterForVpcIDOnlyMatches(vpcIDsWithVpcIDOnlyMatches map[string]struct{}) []*ec2.Filter {
	if len(vpcIDsWithVpcIDOnlyMatches) == 0 {
		return nil
	}

	var filters []*ec2.Filter
	var vpcIDs []*string

	for vpcID := range vpcIDsWithVpcIDOnlyMatches {
		vpcIDs = append(vpcIDs, aws.String(vpcID))
	}

	sort.Slice(vpcIDs, func(i, j int) bool {
		return strings.Compare(*vpcIDs[i], *vpcIDs[j]) < 0
	})
	filter := &ec2.Filter{
		Name:   aws.String(awsFilterKeyVPCID),
		Values: vpcIDs,
	}
	filters = append(filters, filter)

	return filters
}

func buildAwsEc2FilterForVpcIDWithOtherMatches(vpcIDWithOtherMatches []v1alpha1.VirtualMachineSelector,
	vpcIDsWithVpcIDOnlyMatches map[string]struct{}) [][]*ec2.Filter {
	if len(vpcIDWithOtherMatches) == 0 {
		return nil
	}

	var allFilters [][]*ec2.Filter
	for _, match := range vpcIDWithOtherMatches {
		vpcID := match.VpcMatch.MatchID
		if _, found := vpcIDsWithVpcIDOnlyMatches[vpcID]; found {
			continue
		}

		vpcIDFtiler := &ec2.Filter{
			Name:   aws.String(awsFilterKeyVPCID),
			Values: []*string{aws.String(vpcID)},
		}

		for _, vmMatch := range match.VMMatch {
			var filters []*ec2.Filter
			filters = append(filters, vpcIDFtiler)
			vmID := vmMatch.MatchID
			if len(strings.TrimSpace(vmID)) > 0 {
				vmIDsFilter := &ec2.Filter{
					Name:   aws.String(awsFilterKeyVMID),
					Values: []*string{aws.String(vmID)},
				}
				filters = append(filters, vmIDsFilter)
			}

			vmName := vmMatch.MatchName
			if len(strings.TrimSpace(vmName)) > 0 {
				vmIDsFilter := &ec2.Filter{
					Name:   aws.String(awsFilterKeyVMName),
					Values: []*string{aws.String(vmName)},
				}
				filters = append(filters, vmIDsFilter)
			}
			filters = append(filters, buildEc2FilterForValidInstanceStates())
			allFilters = append(allFilters, filters)
		}
	}
	return allFilters
}

func buildAwsEc2FilterForVMIDOnlyMatches(vmIDOnlyMatches []v1alpha1.EntityMatch) []*ec2.Filter {
	if len(vmIDOnlyMatches) == 0 {
		return nil
	}

	var filters []*ec2.Filter
	var vmIDs []*string

	for _, vmMatch := range vmIDOnlyMatches {
		vmIDs = append(vmIDs, aws.String(vmMatch.MatchID))
	}

	sort.Slice(vmIDs, func(i, j int) bool {
		return strings.Compare(*vmIDs[i], *vmIDs[j]) < 0
	})
	filter := &ec2.Filter{
		Name:   aws.String(awsFilterKeyVMID),
		Values: vmIDs,
	}
	filters = append(filters, filter)
	filters = append(filters, buildEc2FilterForValidInstanceStates())

	return filters
}

func buildAwsEc2FilterForVMNameOnlyMatches(vmNameOnlyMatches []v1alpha1.EntityMatch) []*ec2.Filter {
	if len(vmNameOnlyMatches) == 0 {
		return nil
	}

	var filters []*ec2.Filter
	var vmNames []*string

	for _, vmMatch := range vmNameOnlyMatches {
		vmNames = append(vmNames, aws.String(vmMatch.MatchName))
	}

	sort.Slice(vmNames, func(i, j int) bool {
		return strings.Compare(*vmNames[i], *vmNames[j]) < 0
	})
	filter := &ec2.Filter{
		Name:   aws.String(awsFilterKeyVMName),
		Values: vmNames,
	}
	filters = append(filters, filter)
	filters = append(filters, buildEc2FilterForValidInstanceStates())

	return filters
}

func buildAwsEc2FilterForVPCNameOnlyMatches(vpcNameOnlyMatches []v1alpha1.VirtualMachineSelector) []*ec2.Filter {
	if len(vpcNameOnlyMatches) == 0 {
		return nil
	}

	var filters []*ec2.Filter
	var vpcNames []*string

	for _, match := range vpcNameOnlyMatches {
		vpcNames = append(vpcNames, aws.String(match.VpcMatch.MatchName))
	}

	sort.Slice(vpcNames, func(i, j int) bool {
		return strings.Compare(*vpcNames[i], *vpcNames[j]) < 0
	})
	filter := &ec2.Filter{
		Name:   aws.String(awsCustomFilterKeyVPCName),
		Values: vpcNames,
	}
	filters = append(filters, filter)
	filters = append(filters, buildEc2FilterForValidInstanceStates())

	return filters
}

func buildFilterForVPCIDFromFilterForVPCName(filtersForVPCName []*ec2.Filter, vpcNameToID map[string]string) []*ec2.Filter {
	if len(filtersForVPCName) == 0 {
		return nil
	}

	var filters []*ec2.Filter
	var vpcIDs []*string

	for _, filter := range filtersForVPCName {
		if *filter.Name != awsFilterKeyInstanceState {
			for _, vpcName := range filter.Values {
				vpcIDs = append(vpcIDs, aws.String(vpcNameToID[*vpcName]))
			}
		}
	}

	sort.Slice(vpcIDs, func(i, j int) bool {
		return strings.Compare(*vpcIDs[i], *vpcIDs[j]) < 0
	})
	filter := &ec2.Filter{
		Name:   aws.String(awsFilterKeyVPCID),
		Values: vpcIDs,
	}
	filters = append(filters, filter)
	filters = append(filters, buildEc2FilterForValidInstanceStates())

	return filters
}

func buildAwsEc2FilterForSecurityGroupNameMatches(vpcIDsSet []string, cloudSGNamesSet map[string]struct{}) []*ec2.Filter {
	var filters []*ec2.Filter
	var vpcIDs []*string
	var cloudSGNames []*string

	for _, id := range vpcIDsSet {
		idCopy := aws.String(id)
		vpcIDs = append(vpcIDs, idCopy)
	}

	for name := range cloudSGNamesSet {
		nameCopy := name
		cloudSGNames = append(cloudSGNames, &nameCopy)
	}

	vpcIDFilter := &ec2.Filter{
		Name:   aws.String(awsFilterKeyVPCID),
		Values: vpcIDs,
	}
	securityGroupNamesFilter := &ec2.Filter{
		Name:   aws.String(awsFilterKeyGroupName),
		Values: cloudSGNames,
	}
	filters = append(filters, vpcIDFilter)
	filters = append(filters, securityGroupNamesFilter)

	return filters
}

func buildEc2FilterForValidInstanceStates() *ec2.Filter {
	states := []*string{&awsInstanceStateRunningCode, &awsInstanceStateShuttingDownCode, &awsInstanceStateStoppedCode,
		&awsInstanceStateStoppingCode}

	instanceStateFilter := &ec2.Filter{
		Name:   aws.String(awsFilterKeyInstanceState),
		Values: states,
	}

	return instanceStateFilter
}
