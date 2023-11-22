// Copyright 2022 Matrix Origin
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

package util

import (
	"sync"
	"sync/atomic"
)

type node struct {
	//
	nexts []*node
	//
	key, val any
	//
	sync.RWMutex
}

type ConcurrentSkipList struct {
	//
	cap atomic.Int32
	//
	DeleteMutex sync.RWMutex
	//
	keyToMutex sync.Map
	//
	head *node
	//
	nodeCache sync.Pool
	//
	compareFunc func(key1, key2 any) bool
}

func NewConcurrentSkipList(compareFunc func(key1, key2 any) bool) *ConcurrentSkipList {
	return &ConcurrentSkipList{
		head: &node{
			nexts: make([]*node, 1),
		},
		nodeCache: sync.Pool{
			New: func() any {
				return &node{}
			},
		},
		compareFunc: compareFunc,
	}
}

// 根据 key 删除跳表中对应的 key-value
func (c *ConcurrentSkipList) Del(key any) {
	c.DeleteMutex.Lock()
	defer c.DeleteMutex.Unlock()

	var deleteNode *node
	move := c.head
	for level := len(c.head.nexts) - 1; level >= 0; level-- {
		for move.nexts[level] != nil && c.compareFunc(move.nexts[level].key, key) {
			move = move.nexts[level]
		}

	}
}
