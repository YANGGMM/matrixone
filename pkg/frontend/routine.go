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

package frontend

import (
	"fmt"
	"github.com/fagongzi/goetty"
	pConfig "github.com/matrixorigin/matrixcube/components/prophet/config"
	"matrixone/pkg/logutil"
	"net"
	"time"
)

// Routine handles requests.
// Read requests from the IOSession layer,
// use the executor to handle requests, and response them.
type Routine struct {
	//protocol layer
	protocol *MysqlProtocol

	//execution layer
	executor CmdExecutor

	//io data
	io goetty.IOSession

	//the related session
	ses *Session

	// whether the handshake succeeded
	established bool

	// current username
	user string

	// current db name
	db string

	//epoch gc handler
	pdHook *PDCallbackImpl

	//channel of request
	requestChan chan *Request

	//channel of notify
	notifyChan chan interface{}
}

func (routine *Routine) GetClientProtocol() Protocol {
	return routine.protocol
}

func (routine *Routine) GetCmdExecutor() CmdExecutor {
	return routine.executor
}

func (routine *Routine) GetSession() *Session {
	return routine.ses
}

func (routine *Routine) GetPDCallback() pConfig.ContainerHeartbeatDataProcessor {
	return routine.pdHook
}

func (routine *Routine) getConnID() uint32 {
	return routine.protocol.ConnectionID()
}

/*
After the handshake with the client is done, the routine goes into processing loop.
 */
func (routine *Routine) Loop() {
	var req *Request = nil
	var err error
	var resp *Response
	for{
		quit := false
		select {
		case <- routine.notifyChan:
			quit = true
		case req = <- routine.requestChan:
		}

		if quit{
			break
		}

		reqBegin := time.Now()
		if resp, err = routine.executor.ExecRequest(req); err != nil {
			logutil.Errorf("routine execute request failed. error:%v \n", err)
		}

		if resp != nil {
			if err = routine.protocol.SendResponse(resp); err != nil {
				logutil.Errorf("routine send response failed %v. error:%v ", resp, err)
			}
		}

		if routine.ses.Pu.SV.GetRecordTimeElapsedOfSqlRequest() {
			logutil.Infof("connection id %d , the time of handling the request %s", routine.io.ID(), time.Since(reqBegin).String())
		}
	}
}

/*
When the io is closed, the Quit will be called.
 */
func (routine *Routine) Quit() {
	_ = routine.io.Close()
	close(routine.notifyChan)
	if routine.executor != nil {
		routine.executor.Close()
	}
}

// Peer gets the address [Host:Port] of the client
func (routine *Routine) Peer() (string, string) {
	addr := routine.io.RemoteAddr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		logutil.Errorf("get peer host:port failed. error:%v ", err)
		return "failed", "0"
	}
	return host, port
}

func (routine *Routine) ChangeDB(db string) error {
	//TODO: check meta data
	if _, err := routine.ses.Pu.StorageEngine.Database(db); err != nil {
		//echo client. no such database
		return NewMysqlError(ER_BAD_DB_ERROR, db)
	}
	oldDB := routine.db
	routine.db = db

	logutil.Infof("User %s change database from [%s] to [%s]\n", routine.user, oldDB, routine.db)

	return nil
}

func (routine *Routine) handleHandshake(payload []byte) error {
	if len(payload) < 2 {
		return fmt.Errorf("received a broken response packet")
	}

	protocol := routine.protocol
	var authResponse []byte
	if capabilities, _, ok := protocol.io.ReadUint16(payload, 0); !ok {
		return fmt.Errorf("read capabilities from response packet failed")
	} else if uint32(capabilities)&CLIENT_PROTOCOL_41 != 0 {
		var resp41 response41
		var ok bool
		var err error
		if ok, resp41, err = protocol.analyseHandshakeResponse41(payload); !ok {
			return err
		}

		authResponse = resp41.authResponse
		protocol.capability = DefaultCapability & resp41.capabilities

		if nameAndCharset, ok := collationID2CharsetAndName[int(resp41.collationID)]; !ok {
			return fmt.Errorf("get collationName and charset failed")
		} else {
			protocol.collationID = int(resp41.collationID)
			protocol.collationName = nameAndCharset.collationName
			protocol.charset = nameAndCharset.charset
		}

		protocol.maxClientPacketSize = resp41.maxPacketSize
		protocol.username = resp41.username
		routine.user = resp41.username
		routine.db = resp41.database
	} else {
		var resp320 response320
		var ok bool
		var err error
		if ok, resp320, err = protocol.analyseHandshakeResponse320(payload); !ok {
			return err
		}

		authResponse = resp320.authResponse
		protocol.capability = DefaultCapability & resp320.capabilities
		protocol.collationID = int(Utf8mb4CollationID)
		protocol.collationName = "utf8mb4_general_ci"
		protocol.charset = "utf8mb4"

		protocol.maxClientPacketSize = resp320.maxPacketSize
		protocol.username = resp320.username
		routine.user = resp320.username
		routine.db = resp320.database
	}

	if err := protocol.authenticateUser(authResponse); err != nil {
		fail := errorMsgRefer[ER_ACCESS_DENIED_ERROR]
		_ = protocol.sendErrPacket(fail.errorCode, fail.sqlStates[0], "Access denied for user")
		return err
	}

	err := protocol.sendOKPacket(0, 0, 0, 0, "")
	if err != nil {
		return err
	}
	logutil.Infof("SWITCH ESTABLISHED to true")
	routine.established = true
	return nil
}

func NewRoutine(rs goetty.IOSession, protocol *MysqlProtocol, executor CmdExecutor, session *Session) *Routine {
	ri := &Routine{
		protocol:    protocol,
		executor:    executor,
		ses:         session,
		io:          rs,
		established: false,
		requestChan: make(chan *Request,1),
		notifyChan: make(chan interface{}),
	}

	if protocol != nil {
		protocol.SetRoutine(ri)
	}

	if executor != nil {
		executor.SetRoutine(ri)
	}

	//async process request
	go ri.Loop()

	return ri
}
