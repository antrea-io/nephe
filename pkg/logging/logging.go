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

package logging

import (
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	zap2 "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type Logger = logr.Logger

var (
	mutex    sync.Mutex
	debugLog = false
	loggers  = make(map[string]Logger)
)

func GetLogger(name string) Logger {
	key := fmt.Sprintf("%s-debug=%v", name, debugLog)

	mutex.Lock()
	defer mutex.Unlock()

	logger, found := loggers[key]
	if !found {
		if debugLog {
			logger = zap.New(UseDevMode()).WithName(name)
		} else {
			logger = zap.New(UseProdMode()).WithName(name)
		}
		loggers[key] = logger
		return logger
	}

	return logger
}

func SetDebugLog(enableDebugLog bool) {
	debugLog = enableDebugLog
}

func UseDevMode() zap.Opts {
	return func(o *zap.Options) {
		o.Development = true
		o.TimeEncoder = zapcore.ISO8601TimeEncoder
		o.ZapOpts = append(o.ZapOpts, zap2.AddCaller())
	}
}

func UseProdMode() zap.Opts {
	return func(o *zap.Options) {
		o.Development = false
		o.TimeEncoder = zapcore.ISO8601TimeEncoder
	}
}
