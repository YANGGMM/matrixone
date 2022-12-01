// Copyright 2021 - 2022 Matrix Origin
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

package logservice

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/cockroachdb/errors"
	"github.com/lni/dragonboat/v4"
	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/common/morpc"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	pb "github.com/matrixorigin/matrixone/pkg/pb/logservice"
	"github.com/matrixorigin/matrixone/pkg/util/trace"
)

const (
	defaultWriteSocketSize = 64 * 1024
)

// IsTempError returns a boolean value indicating whether the specified error
// is a temp error that worth to be retried, e.g. timeouts, temp network
// issues. Non-temp error caused by program logics rather than some external
// factors.
func IsTempError(err error) bool {
	return isTempError(err)
}

type ClientFactory func() (Client, error)

// Client is the Log Service Client interface exposed to the DN.
type Client interface {
	// Close closes the client.
	Close() error
	// Config returns the specified configuration when creating the client.
	Config() ClientConfig
	// GetLogRecord returns a new LogRecord instance with its Data field enough
	// to hold payloadLength bytes of payload. The layout of the Data field is
	// 4 bytes of record type (pb.UserEntryUpdate) + 8 bytes DN replica ID +
	// payloadLength bytes of actual payload.
	GetLogRecord(payloadLength int) pb.LogRecord
	// Append appends the specified LogRecrd into the Log Service. On success, the
	// assigned Lsn will be returned. For the specified LogRecord, only its Data
	// field is used with all other fields ignored by Append(). Once returned, the
	// pb.LogRecord can be reused.
	Append(ctx context.Context, rec pb.LogRecord) (Lsn, error)
	// Read reads the Log Service from the specified Lsn position until the
	// returned LogRecord set reachs the specified maxSize in bytes. The returned
	// Lsn indicates the next Lsn to use to resume the read, or it means
	// everything available has been read when it equals to the specified Lsn.
	// The returned pb.LogRecord records will have their Lsn and Type fields set,
	// the Lsn field is the Lsn assigned to the record while the Type field tells
	// whether the record is an internal record generated by the Log Service itself
	// or appended by the user.
	Read(ctx context.Context, firstLsn Lsn, maxSize uint64) ([]pb.LogRecord, Lsn, error)
	// Truncate truncates the Log Service log at the specified Lsn with Lsn
	// itself included. This allows the Log Service to free up storage capacities
	// for future appends, all future reads must start after the specified Lsn
	// position.
	Truncate(ctx context.Context, lsn Lsn) error
	// GetTruncatedLsn returns the largest Lsn value that has been specified for
	// truncation.
	GetTruncatedLsn(ctx context.Context) (Lsn, error)
	// GetTSOTimestamp requests a total of count unique timestamps from the TSO and
	// return the first assigned such timestamp, that is TSO timestamps
	// [returned value, returned value + count] will be owned by the caller.
	GetTSOTimestamp(ctx context.Context, count uint64) (uint64, error)
}

type managedClient struct {
	cfg    ClientConfig
	client *client
}

var _ Client = (*managedClient)(nil)

// NewClient creates a Log Service client. Each returned client can be used
// to synchronously issue requests to the Log Service. To send multiple requests
// to the Log Service in parallel, multiple clients should be created and used
// to do so.
func NewClient(ctx context.Context, cfg ClientConfig) (Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &managedClient{cfg: cfg, client: client}, nil
}

func (c *managedClient) Close() error {
	if c.client != nil {
		return c.client.close()
	}
	return nil
}

func (c *managedClient) Config() ClientConfig {
	return c.cfg
}

func (c *managedClient) GetLogRecord(payloadLength int) pb.LogRecord {
	data := make([]byte, headerSize+8+payloadLength)
	binaryEnc.PutUint32(data, uint32(pb.UserEntryUpdate))
	binaryEnc.PutUint64(data[headerSize:], c.cfg.DNReplicaID)
	return pb.LogRecord{Data: data}
}

func (c *managedClient) Append(ctx context.Context, rec pb.LogRecord) (Lsn, error) {
	for {
		if err := c.prepareClient(ctx); err != nil {
			return 0, err
		}
		v, err := c.client.append(ctx, rec)
		if err != nil {
			c.resetClient()
		}
		if c.isRetryableError(err) {
			continue
		}
		return v, err
	}
}

func (c *managedClient) Read(ctx context.Context,
	firstLsn Lsn, maxSize uint64) ([]pb.LogRecord, Lsn, error) {
	for {
		if err := c.prepareClient(ctx); err != nil {
			return nil, 0, err
		}
		recs, v, err := c.client.read(ctx, firstLsn, maxSize)
		if err != nil {
			c.resetClient()
		}
		if c.isRetryableError(err) {
			continue
		}
		return recs, v, err
	}
}

func (c *managedClient) Truncate(ctx context.Context, lsn Lsn) error {
	for {
		if err := c.prepareClient(ctx); err != nil {
			return err
		}
		err := c.client.truncate(ctx, lsn)
		if err != nil {
			c.resetClient()
		}
		if c.isRetryableError(err) {
			continue
		}
		return err
	}
}

func (c *managedClient) GetTruncatedLsn(ctx context.Context) (Lsn, error) {
	for {
		if err := c.prepareClient(ctx); err != nil {
			return 0, err
		}
		v, err := c.client.getTruncatedLsn(ctx)
		if err != nil {
			c.resetClient()
		}
		if c.isRetryableError(err) {
			continue
		}
		return v, err
	}
}

func (c *managedClient) GetTSOTimestamp(ctx context.Context, count uint64) (uint64, error) {
	for {
		if err := c.prepareClient(ctx); err != nil {
			return 0, err
		}
		v, err := c.client.getTSOTimestamp(ctx, count)
		if err != nil {
			c.resetClient()
		}
		if c.isRetryableError(err) {
			continue
		}
		return v, err
	}
}

func (c *managedClient) isRetryableError(err error) bool {
	/*
		old code, obviously strange
		if errors.Is(err, dragonboat.ErrTimeout) {
			return false
		}
		return errors.Is(err, dragonboat.ErrShardNotFound)
	*/

	// Dragonboat error leaked here
	if errors.Is(err, dragonboat.ErrShardNotFound) {
		return true
	}
	return moerr.IsMoErrCode(err, moerr.ErrDragonboatShardNotFound)
}

func (c *managedClient) resetClient() {
	if c.client != nil {
		cc := c.client
		c.client = nil
		if err := cc.close(); err != nil {
			logutil.Error("failed to close client", zap.Error(err))
		}
	}
}

func (c *managedClient) prepareClient(ctx context.Context) error {
	if c.client != nil {
		return nil
	}
	cc, err := newClient(ctx, c.cfg)
	if err != nil {
		return err
	}
	c.client = cc
	return nil
}

type client struct {
	cfg      ClientConfig
	client   morpc.RPCClient
	addr     string
	pool     *sync.Pool
	respPool *sync.Pool
}

func newClient(ctx context.Context, cfg ClientConfig) (*client, error) {
	client, err := connectToLogService(ctx, cfg.ServiceAddresses, cfg)
	if client != nil && err == nil {
		return client, nil
	}
	if len(cfg.DiscoveryAddress) > 0 {
		return connectToLogServiceByReverseProxy(ctx, cfg.DiscoveryAddress, cfg)
	}
	if err != nil {
		return nil, err
	}
	return nil, moerr.NewLogServiceNotReady(ctx)
}

func connectToLogServiceByReverseProxy(ctx context.Context,
	discoveryAddress string, cfg ClientConfig) (*client, error) {
	si, ok, err := GetShardInfo(discoveryAddress, cfg.LogShardID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, moerr.NewLogServiceNotReady(ctx)
	}
	addresses := make([]string, 0)
	leaderAddress, ok := si.Replicas[si.ReplicaID]
	if ok {
		addresses = append(addresses, leaderAddress)
	}
	for replicaID, address := range si.Replicas {
		if replicaID != si.ReplicaID {
			addresses = append(addresses, address)
		}
	}
	return connectToLogService(ctx, addresses, cfg)
}

func connectToLogService(ctx context.Context,
	targets []string, cfg ClientConfig) (*client, error) {
	if len(targets) == 0 {
		return nil, nil
	}

	pool := &sync.Pool{}
	pool.New = func() interface{} {
		return &RPCRequest{pool: pool}
	}
	respPool := &sync.Pool{}
	respPool.New = func() interface{} {
		return &RPCResponse{pool: respPool}
	}
	c := &client{
		cfg:      cfg,
		pool:     pool,
		respPool: respPool,
	}
	var e error
	addresses := append([]string{}, targets...)
	rand.Shuffle(len(cfg.ServiceAddresses), func(i, j int) {
		addresses[i], addresses[j] = addresses[j], addresses[i]
	})
	for _, addr := range addresses {
		cc, err := getRPCClient(ctx, addr, c.respPool, c.cfg.MaxMessageSize, cfg.Tag)
		if err != nil {
			e = err
			continue
		}
		c.addr = addr
		c.client = cc
		if cfg.ReadOnly {
			if err := c.connectReadOnly(ctx); err == nil {
				return c, nil
			} else {
				if err := c.close(); err != nil {
					logutil.Error("failed to close the client", zap.Error(err))
				}
				e = err
			}
		} else {
			// TODO: add a test to check whether it works when there is no truncated
			// LSN known to the logservice.
			if err := c.connectReadWrite(ctx); err == nil {
				return c, nil
			} else {
				if err := c.close(); err != nil {
					logutil.Error("failed to close the client", zap.Error(err))
				}
				e = err
			}
		}
	}
	return nil, e
}

func (c *client) close() error {
	return c.client.Close()
}

func (c *client) append(ctx context.Context, rec pb.LogRecord) (Lsn, error) {
	if c.readOnly() {
		return 0, moerr.NewInvalidInput(ctx, "incompatible client")
	}
	// TODO: check piggybacked hint on whether we are connected to the leader node
	return c.doAppend(ctx, rec)
}

func (c *client) read(ctx context.Context,
	firstLsn Lsn, maxSize uint64) ([]pb.LogRecord, Lsn, error) {
	return c.doRead(ctx, firstLsn, maxSize)
}

func (c *client) truncate(ctx context.Context, lsn Lsn) error {
	if c.readOnly() {
		return moerr.NewInvalidInput(ctx, "incompatible client")
	}
	return c.doTruncate(ctx, lsn)
}

func (c *client) getTruncatedLsn(ctx context.Context) (Lsn, error) {
	return c.doGetTruncatedLsn(ctx)
}

func (c *client) getTSOTimestamp(ctx context.Context, count uint64) (uint64, error) {
	return c.tsoRequest(ctx, count)
}

func (c *client) readOnly() bool {
	return c.cfg.ReadOnly
}

func (c *client) connectReadWrite(ctx context.Context) error {
	if c.readOnly() {
		panic(moerr.NewInvalidInput(ctx, "incompatible client"))
	}
	return c.connect(ctx, pb.CONNECT)
}

func (c *client) connectReadOnly(ctx context.Context) error {
	return c.connect(ctx, pb.CONNECT_RO)
}

func (c *client) request(ctx context.Context,
	mt pb.MethodType, payload []byte, lsn Lsn,
	maxSize uint64) (pb.Response, []pb.LogRecord, error) {
	ctx, span := trace.Debug(ctx, "client.request")
	defer span.End()
	req := pb.Request{
		Method: mt,
		LogRequest: pb.LogRequest{
			ShardID: c.cfg.LogShardID,
			DNID:    c.cfg.DNReplicaID,
			Lsn:     lsn,
			MaxSize: maxSize,
		},
	}
	r := c.pool.Get().(*RPCRequest)
	defer r.Release()
	r.Request = req
	r.payload = payload
	future, err := c.client.Send(ctx, c.addr, r)
	if err != nil {
		return pb.Response{}, nil, err
	}
	defer future.Close()
	msg, err := future.Get()
	if err != nil {
		return pb.Response{}, nil, err
	}
	response, ok := msg.(*RPCResponse)
	if !ok {
		panic("unexpected response type")
	}
	resp := response.Response
	defer response.Release()
	var recs pb.LogRecordResponse
	if len(response.payload) > 0 {
		MustUnmarshal(&recs, response.payload)
	}
	err = toError(ctx, response.Response)
	if err != nil {
		return pb.Response{}, nil, err
	}
	return resp, recs.Records, nil
}

func (c *client) tsoRequest(ctx context.Context, count uint64) (uint64, error) {
	ctx, span := trace.Debug(ctx, "client.tsoRequest")
	defer span.End()
	req := pb.Request{
		Method: pb.TSO_UPDATE,
		TsoRequest: &pb.TsoRequest{
			Count: count,
		},
	}
	r := c.pool.Get().(*RPCRequest)
	r.Request = req
	future, err := c.client.Send(ctx, c.addr, r)
	if err != nil {
		return 0, err
	}
	defer future.Close()
	msg, err := future.Get()
	if err != nil {
		return 0, err
	}
	response, ok := msg.(*RPCResponse)
	if !ok {
		panic("unexpected response type")
	}
	resp := response.Response
	defer response.Release()
	err = toError(ctx, response.Response)
	if err != nil {
		return 0, err
	}
	return resp.TsoResponse.Value, nil
}

func (c *client) connect(ctx context.Context, mt pb.MethodType) error {
	_, _, err := c.request(ctx, mt, nil, 0, 0)
	return err
}

func (c *client) doAppend(ctx context.Context, rec pb.LogRecord) (Lsn, error) {
	resp, _, err := c.request(ctx, pb.APPEND, rec.Data, 0, 0)
	if err != nil {
		return 0, err
	}
	return resp.LogResponse.Lsn, nil
}

func (c *client) doRead(ctx context.Context,
	firstLsn Lsn, maxSize uint64) ([]pb.LogRecord, Lsn, error) {
	resp, recs, err := c.request(ctx, pb.READ, nil, firstLsn, maxSize)
	if err != nil {
		return nil, 0, err
	}
	return recs, resp.LogResponse.LastLsn, nil
}

func (c *client) doTruncate(ctx context.Context, lsn Lsn) error {
	_, _, err := c.request(ctx, pb.TRUNCATE, nil, lsn, 0)
	return err
}

func (c *client) doGetTruncatedLsn(ctx context.Context) (Lsn, error) {
	resp, _, err := c.request(ctx, pb.GET_TRUNCATE, nil, 0, 0)
	if err != nil {
		return 0, err
	}
	return resp.LogResponse.Lsn, nil
}

func getRPCClient(ctx context.Context, target string, pool *sync.Pool, maxMessageSize int, tag ...string) (morpc.RPCClient, error) {
	mf := func() morpc.Message {
		return pool.Get().(*RPCResponse)
	}

	// construct morpc.BackendOption
	backendOpts := []morpc.BackendOption{
		morpc.WithBackendConnectTimeout(time.Second),
		morpc.WithBackendHasPayloadResponse(),
		morpc.WithBackendLogger(logutil.GetGlobalLogger().Named("hakeeper-client-backend")),
	}
	backendOpts = append(backendOpts, GetBackendOptions(ctx)...)

	// construct morpc.ClientOption
	clientOpts := []morpc.ClientOption{
		morpc.WithClientInitBackends([]string{target}, []int{1}),
		morpc.WithClientMaxBackendPerHost(1),
		morpc.WithClientTag(fmt.Sprintf("hakeeper-client(%s)", tag)),
		morpc.WithClientLogger(logutil.GetGlobalLogger()),
	}
	clientOpts = append(clientOpts, GetClientOptions(ctx)...)

	// we set connection timeout to a constant value so if ctx's deadline is much
	// larger, then we can ensure that all specified potential nodes have a chance
	// to be attempted
	codec := morpc.NewMessageCodec(mf,
		morpc.WithCodecPayloadCopyBufferSize(defaultWriteSocketSize),
		morpc.WithCodecEnableChecksum(),
		morpc.WithCodecMaxBodySize(maxMessageSize))
	bf := morpc.NewGoettyBasedBackendFactory(codec, backendOpts...)
	return morpc.NewClient(bf, clientOpts...)
}
