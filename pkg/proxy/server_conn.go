// Copyright 2021 - 2023 Matrix Origin
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

package proxy

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/fagongzi/goetty/v2"
	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/config"
	"github.com/matrixorigin/matrixone/pkg/frontend"
)

// serverBaseConnID is the base connection ID for server.
var serverBaseConnID uint32 = 1000

// ServerConn is the connection to the backend server.
type ServerConn interface {
	// RawConn return the raw connection.
	RawConn() net.Conn
	// HandleHandshake handles the handshake communication with CN server.
	// handshakeResp is a auth packet received from client.
	HandleHandshake(handshakeResp *frontend.Packet) (*frontend.Packet, error)
	// Close closes the connection to CN server.
	Close() error
}

// serverConn is the connection between proxy and CN server.
type serverConn struct {
	// cnServer is the backend CN server.
	cnServer *CNServer
	// conn is the raw TCP connection between proxy and server.
	conn goetty.IOSession
	// connID records the connection ID.
	connID uint32
	// mysqlProto is used to build handshake info.
	mysqlProto *frontend.MysqlProtocolImpl
	// rebalancer is used to track connections between proxy and server.
	rebalancer *rebalancer
	// tun is the tunnel which this server connection belongs to.
	tun *tunnel
}

var _ ServerConn = (*serverConn)(nil)

// newServerConn creates a connection to CN server.
func newServerConn(cn *CNServer, tun *tunnel, r *rebalancer) (ServerConn, error) {
	c, err := cn.Connect()
	if err != nil {
		return nil, err
	}
	s := &serverConn{
		cnServer:   cn,
		conn:       c,
		connID:     nextServerConnID(),
		rebalancer: r,
		tun:        tun,
	}
	// Track the connection.
	r.connManager.connect(s.cnServer, tun)

	fp := config.FrontendParameters{}
	fp.SetDefaultValues()
	s.mysqlProto = frontend.NewMysqlClientProtocol(s.connID, c, 0, &fp)
	return s, nil
}

// wrappedConn wraps the connection to disconnect from connection manager.
type wrappedConn struct {
	net.Conn
	sync.Once
	closeFn func()
}

// Close closes the wrapped connection, which calls closeFn to disconnect
// from the connection manager.
func (w *wrappedConn) Close() error {
	if w != nil && w.closeFn != nil {
		w.Once.Do(w.closeFn)
	}
	return w.Conn.Close()
}

// RawConn implements the ServerConn interface.
func (s *serverConn) RawConn() net.Conn {
	if s != nil {
		if s.cnServer != nil {
			return &wrappedConn{
				Conn: s.conn.RawConn(),
				closeFn: func() {
					s.rebalancer.connManager.disconnect(s.cnServer, s.tun)
				},
			}
		}
		return s.conn.RawConn()
	}
	return nil
}

// HandleHandshake implements the ServerConn interface.
func (s *serverConn) HandleHandshake(handshakeResp *frontend.Packet) (*frontend.Packet, error) {
	// Step 1, read initial handshake from CN server.
	if err := s.readInitialHandshake(); err != nil {
		return nil, err
	}
	// Step 2, write the handshake response to CN server, which is
	// received from client earlier.
	r, err := s.writeHandshakeResp(handshakeResp)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Close implements the ServerConn interface.
func (s *serverConn) Close() error {
	// Un-track the connection.
	s.rebalancer.connManager.disconnect(s.cnServer, s.tun)
	return nil
}

// readPacket reads packet from CN server, usually used in handshake phase.
func (s *serverConn) readPacket() (*frontend.Packet, error) {
	msg, err := s.conn.Read(goetty.ReadOptions{})
	if err != nil {
		return nil, err
	}
	packet, ok := msg.(*frontend.Packet)
	if !ok {
		return nil, moerr.NewInternalErrorNoCtx("message is not a Packet")
	}
	return packet, nil
}

// nextServerConnID increases baseConnID by 1 and returns the result.
func nextServerConnID() uint32 {
	return atomic.AddUint32(&serverBaseConnID, 1)
}
