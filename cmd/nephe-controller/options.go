// Copyright 2023 Antrea Authors.
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

package main

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v2"

	"antrea.io/nephe/pkg/config"
)

type Options struct {
	// The path of configuration file.
	configFile string
	// The configuration object
	config *config.ControllerConfig
}

// newOptions returns a new options object.
func newOptions() *Options {
	return &Options{
		config: &config.ControllerConfig{},
	}
}

// complete all the required options.
func (o *Options) complete() error {
	if len(o.configFile) > 0 {
		if err := o.loadConfigFromFile(); err != nil {
			return err
		}
	}
	if err := o.validate(); err != nil {
		return err
	}
	o.setDefaults()
	return nil
}

// loadConfigFromFile reads the configuration file and loads it into options.
func (o *Options) loadConfigFromFile() error {
	data, err := os.ReadFile(o.configFile)
	if err != nil {
		return err
	}
	return yaml.UnmarshalStrict(data, &o.config)
}

// validate checks if the configuration options are valid.
func (o *Options) validate() error {
	if len(o.config.CloudResourcePrefix) > 0 {
		reg := regexp.MustCompile(`^[a-zA-Z0-9]+(-?[a-zA-Z0-9])*$`)
		if !reg.MatchString(o.config.CloudResourcePrefix) {
			return fmt.Errorf("invalid CloudResourcePrefix %v. Only alphanumeric and '-' characters are allowed. "+
				"Special character '-' is only allowed at the middle", o.config.CloudResourcePrefix)
		}
	}

	if o.config.CloudSyncInterval != 0 && o.config.CloudSyncInterval < config.MinimumCloudSyncInterval {
		return fmt.Errorf("invalid CloudSyncInterval %v, CloudSyncInterval should be >= %v seconds",
			o.config.CloudSyncInterval, config.MinimumCloudSyncInterval)
	}
	return nil
}

// setDefaults sets the configuration to default value if they are not set.
func (o *Options) setDefaults() {
	if len(o.config.CloudResourcePrefix) == 0 {
		o.config.CloudResourcePrefix = config.DefaultCloudResourcePrefix
	}
	if o.config.CloudSyncInterval == 0 {
		o.config.CloudSyncInterval = config.DefaultCloudSyncInterval
	}
}
