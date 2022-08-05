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

package txnengine

import (
	"bytes"
	"context"
	"encoding/gob"

	logservicepb "github.com/matrixorigin/matrixone/pkg/pb/logservice"
	"github.com/matrixorigin/matrixone/pkg/pb/metadata"
	"github.com/matrixorigin/matrixone/pkg/pb/txn"
	"github.com/matrixorigin/matrixone/pkg/txn/rpc"
)

func doTxnRequest[
	Resp any,
	Req any,
](
	ctx context.Context,
	e *Engine,
	reqFunc func(context.Context, []txn.TxnRequest) (*rpc.SendResult, error),
	selectNodes func([]logservicepb.DNNode) []logservicepb.DNNode,
	op uint32,
	req Req,
) (
	resps []Resp,
	err error,
) {

	clusterDetails, err := e.getClusterDetails()
	if err != nil {
		return nil, err
	}
	nodes := selectNodes(clusterDetails.DNNodes)

	requests := make([]txn.TxnRequest, 0, len(nodes))
	for _, node := range nodes {
		requests = append(requests, txn.TxnRequest{
			CNRequest: &txn.CNOpRequest{
				OpCode:  op,
				Payload: mustEncodePayload(req),
				Target: metadata.DNShard{
					Address: node.ServiceAddress,
				},
			},
		})
	}

	result, err := reqFunc(ctx, requests)
	if err != nil {
		return
	}
	if err = errorFromTxnResponses(result.Responses); err != nil {
		return
	}

	for _, res := range result.Responses {
		var resp Resp
		if err = gob.NewDecoder(bytes.NewReader(res.CNOpResponse.Payload)).Decode(&resp); err != nil {
			return
		}
		resps = append(resps, resp)
	}

	return
}

func allNodes(nodes []logservicepb.DNNode) []logservicepb.DNNode {
	return nodes
}

func firstNode(nodes []logservicepb.DNNode) []logservicepb.DNNode {
	return nodes[:1]
}

func theseNodes(nodes []logservicepb.DNNode) func(nodes []logservicepb.DNNode) []logservicepb.DNNode {
	return func(_ []logservicepb.DNNode) []logservicepb.DNNode {
		return nodes
	}
}
