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

package memtable

import (
	"matrixone/pkg/container/vector"
	"matrixone/pkg/logutil"
	"matrixone/pkg/vm/engine/aoe/storage/layout/dataio"
)

type memTableWriter struct {
	memTable *memTable
}

func (mw *memTableWriter) Flush() (err error) {
	bat := mw.memTable.iblk.GetFullBatch()
	defer bat.Close()
	var vecs []*vector.Vector
	for idx, _ := range bat.GetAttrs() {
		node := bat.GetVectorByAttr(idx)
		vecs = append(vecs, node.CopyToVector())
	}
	bw := dataio.NewBlockWriter(vecs, mw.memTable.meta, mw.memTable.meta.Segment.Table.Conf.Dir)
	bw.SetPreExecutor(func() {
		logutil.Infof(" %s | memTable | Flushing", bw.GetFileName())
	})
	bw.SetPostExecutor(func() {
		logutil.Infof(" %s | memTable | Flushed", bw.GetFileName())
	})
	return bw.Execute()
}
