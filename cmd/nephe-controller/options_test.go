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
	"testing"

	"github.com/stretchr/testify/assert"

	"antrea.io/nephe/pkg/config"
)

func TestOptions(t *testing.T) {
	tests := []struct {
		name        string
		config      *config.ControllerConfig
		expectedErr string
	}{
		{
			name: "Invalid CloudResourcePrefix",
			config: &config.ControllerConfig{
				CloudResourcePrefix: "-anp",
				CloudSyncInterval:   70,
			},
			expectedErr: "invalid CloudResourcePrefix",
		}, {
			name: "Invalid CloudSyncInterval",
			config: &config.ControllerConfig{
				CloudResourcePrefix: "anp",
				CloudSyncInterval:   30,
			},
			expectedErr: "invalid CloudSyncInterval",
		}, {
			name:        "Empty config",
			config:      &config.ControllerConfig{},
			expectedErr: "",
		},
		{
			name: "Valid input",
			config: &config.ControllerConfig{
				CloudResourcePrefix: "anp",
				CloudSyncInterval:   70,
			},
			expectedErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{config: tt.config}
			err := o.complete()
			if tt.expectedErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.expectedErr)
			}
		})
	}
}
