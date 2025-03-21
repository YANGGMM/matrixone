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

package fileservice

import (
	"errors"
	"io"
	"net"
	"regexp"
	"strings"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
)

func IsRetryableError(err error) bool {
	// unexpected EOF
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// net timeout
	if e, ok := err.(net.Error); ok && e.Timeout() {
		return true
	}

	str := err.Error()
	// match exact string
	switch str {
	case "connection reset by peer",
		"connection timed out":
		return true
	}

	// match sub-string
	if strings.Contains(str, "unexpected EOF") ||
		strings.Contains(str, "connection reset by peer") ||
		strings.Contains(str, "connection timed out") ||
		strings.Contains(str, "dial tcp: lookup") ||
		strings.Contains(str, "i/o timeout") ||
		strings.Contains(str, "write: broken pipe") ||
		strings.Contains(str, "TLS handshake timeout") ||
		strings.Contains(str, "replication in progress") || // HDFS error
		strings.Contains(str, "use of closed network connection") {
		return true
	}

	return false
}

type errorStr string

func (e errorStr) Error() string {
	return string(e)
}

type throwError struct {
	err error
}

func throw(err error) {
	panic(throwError{
		err: err,
	})
}

func catch(ptr *error) {
	p := recover()
	if p == nil {
		return
	}
	e, ok := p.(throwError)
	if !ok {
		panic(p)
	} else {
		*ptr = e.err
	}
}

var httpBadLengthPattern = regexp.MustCompile(`transport connection broken: http: ContentLength=[0-9]* with Body length [0-9]*`)

func wrapSizeMismatchErr(p *error) {
	if *p == nil {
		return
	}
	str := (*p).Error()

	if strings.Contains(str, "size does not match") ||
		httpBadLengthPattern.MatchString(str) {
		*p = moerr.NewSizeNotMatchNoCtx("")
	}

}

func isDiskFull(err error) bool {
	if err == nil {
		return false
	}
	str := err.Error()
	return strings.Contains(str, "disk quota exceeded")
}
