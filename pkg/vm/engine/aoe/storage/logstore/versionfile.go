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

package logstore

import (
	"matrixone/pkg/logutil"
	"os"
)

type VersionFile struct {
	*os.File
	Version uint64
	Size    int64
}

func (vf *VersionFile) Truncate(size int64) error {
	if err := vf.File.Truncate(size); err != nil {
		return err
	}
	vf.Size = size
	return nil
}

func (vf *VersionFile) Destroy() error {
	if err := vf.Close(); err != nil {
		return err
	}
	name := vf.Name()
	logutil.Infof("Removing version file: %s", name)
	err := os.Remove(name)
	return err
}
