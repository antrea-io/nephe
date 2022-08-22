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
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
)

const (
	awsComputeServiceNameEC2 = internal.CloudServiceName("EC2")
)

// awsServiceClientCreateInterface provides interface to create aws service clients.
type awsServiceClientCreateInterface interface {
	compute() (awsEC2Wrapper, error)
	// Add any aws service (like rds, elb etc) apiClient creation methods here
}

// awsServiceSdkConfigProvider provides config required to create aws service (ec2) clients.
// Implements awsServiceClientCreateInterface interface
// NOTE: Currently supporting only static credentials based clients.
type awsServiceSdkConfigProvider struct {
	session *session.Session
}

// awsServicesHelper.
type awsServicesHelper interface {
	newServiceSdkConfigProvider(accCfg *awsAccountConfig) (awsServiceClientCreateInterface, error)
}

type awsServicesHelperImpl struct{}

// newServiceSdkConfigProvider returns config to create aws services clients.
func (h *awsServicesHelperImpl) newServiceSdkConfigProvider(accConfig *awsAccountConfig) (awsServiceClientCreateInterface, error) {
	var creds *credentials.Credentials

	if len(accConfig.RoleArn) != 0 {
		// use role base access if role provided
		// new session using worker node role, it should have AssumeRole permissions to the Customer's role ARN resource
		sess, err := session.NewSession(&aws.Config{
			Region:                        &accConfig.region,
			CredentialsChainVerboseErrors: aws.Bool(true),
		})
		if err != nil {
			return nil, fmt.Errorf("unable to initialize AWS session: %v", err)
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
		creds = credentials.NewStaticCredentials(accConfig.AccessKeyID, accConfig.AccessKeySecret, "")
	}

	awsConfig := &aws.Config{
		Region:                        &accConfig.region,
		Credentials:                   creds,
		CredentialsChainVerboseErrors: aws.Bool(true),
	}

	sess, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize AWS session: %v", err)
	}
	configProvider := &awsServiceSdkConfigProvider{
		session: sess,
	}
	return configProvider, nil
}

func newAwsServiceConfigs(accountNamespacedName *types.NamespacedName, accCredentials interface{}, awsSpecificHelper interface{}) (
	[]internal.CloudServiceInterface, error) {
	awsServicesHelper := awsSpecificHelper.(awsServicesHelper)
	awsAccountCredentials := accCredentials.(*awsAccountConfig)

	var serviceConfigs []internal.CloudServiceInterface

	awsServiceClientCreator, err := awsServicesHelper.newServiceSdkConfigProvider(awsAccountCredentials)
	if err != nil {
		return nil, err
	}

	ec2Service, err := newEC2ServiceConfig(accountNamespacedName.String(), awsServiceClientCreator)
	if err != nil {
		return nil, err
	}
	serviceConfigs = append(serviceConfigs, ec2Service)

	return serviceConfigs, nil
}
