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
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	"antrea.io/nephe/pkg/util"
)

// awsServiceClientCreateInterface provides interface to create aws service clients.
type awsServiceClientCreateInterface interface {
	compute() (awsEC2Wrapper, error)
	// Add any aws service (like rds, elb etc.) apiClient creation methods here
}

// awsServiceSdkConfigProvider provides config required to create aws service (ec2) clients.
// Implements awsServiceClientCreateInterface interface
// NOTE: Currently supporting only static credentials based clients.
type awsServiceSdkConfigProvider struct {
	session *session.Session
}

// awsServicesHelper.
type awsServicesHelper interface {
	newServiceSdkConfigProvider(accCfg *awsAccountConfig, region string) (awsServiceClientCreateInterface, error)
}

type awsServicesHelperImpl struct{}

// newServiceSdkConfigProvider returns config to create aws services clients.
func (h *awsServicesHelperImpl) newServiceSdkConfigProvider(accConfig *awsAccountConfig, region string) (
	awsServiceClientCreateInterface, error) {
	var creds *credentials.Credentials
	var err error
	if len(accConfig.RoleArn) != 0 {
		var sess *session.Session
		// If credentials are specified too, create a session with these credentials.
		if len(accConfig.AccessKeyID) != 0 && len(accConfig.AccessKeySecret) != 0 {
			tempCreds := credentials.NewStaticCredentials(accConfig.AccessKeyID, accConfig.AccessKeySecret, accConfig.SessionToken)
			if sess, err = session.NewSession(&aws.Config{
				Region:                        aws.String(region),
				Credentials:                   tempCreds,
				CredentialsChainVerboseErrors: aws.Bool(true),
			}); err != nil {
				return nil, fmt.Errorf("error initializing AWS session: %v", err)
			}
		} else {
			// use role base access if role provided
			// new session using worker node role, it should have AssumeRole permissions to the Customer's role ARN resource
			if sess, err = session.NewSession(&aws.Config{
				Region:                        aws.String(region),
				CredentialsChainVerboseErrors: aws.Bool(true),
			}); err != nil {
				return nil, fmt.Errorf("error initializing AWS session: %v", err)
			}
		}

		// configure to assume customer role and retrieve temporary credentials
		externalID := &accConfig.ExternalID
		if len(accConfig.ExternalID) == 0 {
			externalID = nil
		}
		stsClient := sts.New(sess)
		creds = credentials.NewCredentials(&stscreds.AssumeRoleProvider{
			Client:     stsClient,
			RoleARN:    accConfig.RoleArn,
			ExternalID: externalID,
		})
	} else {
		// use static credentials passed in
		creds = credentials.NewStaticCredentials(accConfig.AccessKeyID, accConfig.AccessKeySecret, accConfig.SessionToken)
	}

	awsConfig := &aws.Config{
		Region:                        aws.String(region),
		Endpoint:                      &accConfig.endpoint,
		Credentials:                   creds,
		CredentialsChainVerboseErrors: aws.Bool(true),
	}

	sess, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing AWS session: %v", err)
	}
	configProvider := &awsServiceSdkConfigProvider{
		session: sess,
	}
	return configProvider, nil
}

// newAwsServiceConfigs creates service configs for each region for an account and returns a region to service config map for successful
// creations and multierr for any errors.
func newAwsServiceConfigs(accountNamespacedName *types.NamespacedName, config interface{}, awsSpecificHelper interface{}) (
	map[string]internal.CloudServiceInterface, error) {
	awsServicesHelper := awsSpecificHelper.(awsServicesHelper)
	awsConfig := config.(*awsAccountConfig)

	// check regions valid and process regions.
	regions, err := processRegions(awsConfig)
	if err != nil {
		return map[string]internal.CloudServiceInterface{}, err
	}
	awsConfig.regions = regions

	// create config provider.
	var retErr error
	configProviders := make(map[string]awsServiceClientCreateInterface)
	for _, region := range awsConfig.regions {
		configProvider, err := awsServicesHelper.newServiceSdkConfigProvider(awsConfig, region)
		retErr = multierr.Append(retErr, err)
		if err == nil {
			configProviders[region] = configProvider
		}
	}

	// create service configs.
	ec2Service, err := newEC2ServiceConfig(*accountNamespacedName, configProviders)
	retErr = multierr.Append(retErr, err)

	return ec2Service, retErr
}

// updateAwsServiceConfigs updates the current region to service config map according to the new map.
func updateAwsServiceConfigs(current, new map[string]internal.CloudServiceInterface) error {
	var retErr error
	var selectors map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector
	for region, s := range current {
		selectors = s.GetResourceFilters()
		newServiceConfig, found := new[region]
		if !found {
			delete(current, region)
		} else {
			// update the service config if new service config is different object from current service config.
			if err := s.UpdateServiceConfig(newServiceConfig); err != nil {
				retErr = multierr.Append(retErr, err)
			}
			delete(new, region)
		}
	}
	for region, s := range new {
		current[region] = s
		// resource selectors from previous service configs needs to be replayed.
		for _, selector := range selectors {
			if err := s.AddResourceFilters(selector); err != nil {
				retErr = multierr.Append(retErr, err)
				delete(current, region)
				break
			}
		}
	}
	return retErr
}

// processRegions check if account regions are all valid and deduplicate regions.
func processRegions(config *awsAccountConfig) ([]string, error) {
	awsRegionMap := endpoints.AwsPartition().Regions()
	existingRegions := make(map[string]struct{})
	retRegions := make([]string, 0, len(config.regions))
	for _, region := range config.regions {
		if _, found := existingRegions[region]; found {
			continue
		}
		if _, found := awsRegionMap[region]; !found {
			allRegions := make([]string, 0, len(awsRegionMap))
			for r := range awsRegionMap {
				allRegions = append(allRegions, r)
			}
			return nil, fmt.Errorf("%s, %s not in supported regions %v", util.ErrorMsgRegion, region, allRegions)
		}
		existingRegions[region] = struct{}{}
		retRegions = append(retRegions, region)
	}
	return retRegions, nil
}
