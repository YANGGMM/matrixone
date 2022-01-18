// Copyright 2021 Matrix Origin
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

	fz "github.com/matrixorigin/matrixone/pkg/chaostesting"
	"github.com/reusee/e4"
)

func TestRun(t *testing.T) {
	defer he(nil, e4.TestingFatal(t))
	NewScope().Fork(
		func() fz.IsTesting {
			return true
		},
	).Call(func(
		execute fz.Execute,
	) {
		ce(execute())
	})
}
