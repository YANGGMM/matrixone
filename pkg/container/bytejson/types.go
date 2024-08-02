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

package bytejson

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/pingcap/errors"
)

type subPathType byte
type pathFlag byte

type ByteJson struct {
	Data []byte
	Type TpCode
}

type subPathIndices struct {
	tp  byte
	num int
}
type subPathRangeExpr struct {
	start *subPathIndices
	end   *subPathIndices
}

type subPath struct {
	key    string
	idx    *subPathIndices
	iRange *subPathRangeExpr
	tp     subPathType
}
type Path struct {
	paths []subPath
	flag  pathFlag
}
type pathGenerator struct {
	pathStr string
	pos     int
}

type UnnestResult map[string][]byte

const (
	numberIndices byte = iota + 1
	lastIndices
	lastKey    = "last"
	lastKeyLen = 4
	toKey      = "to"
	toKeyLen   = 2
)

const (
	subPathIdxALL = -1
	subPathIdxErr = -2
)

const (
	subPathDoubleStar subPathType = iota + 1
	subPathIdx
	subPathKey
	subPathRange
)
const (
	pathFlagSingleStar pathFlag = iota + 1
	pathFlagDoubleStar
)

type TpCode byte

const (
	TpCodeObject     TpCode = 0x01
	TpCodeArray      TpCode = 0x03
	TpCodeLiteral    TpCode = 0x04
	TpCodeInt64      TpCode = 0x09
	TpCodeUint64     TpCode = 0x0a
	TpCodeFloat64    TpCode = 0x0b
	TpCodeString     TpCode = 0x0c
	TpCodeOpaque     TpCode = 0x0d
	TypeCodeDate     TpCode = 0x0e
	TypeCodeDateTime TpCode = 0x0f
	TpCodeTimeStmap  TpCode = 0x10
	TpCodeDuring     TpCode = 0x11
)

// JSONTypeCode indicates JSON type.
type JSONTypeCode = byte

const (
	// JSONTypeCodeObject indicates the JSON is an object.
	JSONTypeCodeObject JSONTypeCode = 0x01
	// JSONTypeCodeArray indicates the JSON is an array.
	JSONTypeCodeArray JSONTypeCode = 0x03
	// JSONTypeCodeLiteral indicates the JSON is a literal.
	JSONTypeCodeLiteral JSONTypeCode = 0x04
	// JSONTypeCodeInt64 indicates the JSON is a signed integer.
	JSONTypeCodeInt64 JSONTypeCode = 0x09
	// JSONTypeCodeUint64 indicates the JSON is a unsigned integer.
	JSONTypeCodeUint64 JSONTypeCode = 0x0a
	// JSONTypeCodeFloat64 indicates the JSON is a double float number.
	JSONTypeCodeFloat64 JSONTypeCode = 0x0b
	// JSONTypeCodeString indicates the JSON is a string.
	JSONTypeCodeString JSONTypeCode = 0x0c
	// JSONTypeCodeOpaque indicates the JSON is a opaque
	JSONTypeCodeOpaque JSONTypeCode = 0x0d
	// JSONTypeCodeDate indicates the JSON is a opaque
	JSONTypeCodeDate JSONTypeCode = 0x0e
	// JSONTypeCodeDatetime indicates the JSON is a opaque
	JSONTypeCodeDatetime JSONTypeCode = 0x0f
	// JSONTypeCodeTimestamp indicates the JSON is a opaque
	JSONTypeCodeTimestamp JSONTypeCode = 0x10
	// JSONTypeCodeDuration indicates the JSON is a opaque
	JSONTypeCodeDuration JSONTypeCode = 0x11
)

// var jsonSafeSet = [utf8.RuneSelf]bool{
// 	' ':      true,
// 	'!':      true,
// 	'"':      false,
// 	'#':      true,
// 	'$':      true,
// 	'%':      true,
// 	'&':      true,
// 	'\'':     true,
// 	'(':      true,
// 	')':      true,
// 	'*':      true,
// 	'+':      true,
// 	',':      true,
// 	'-':      true,
// 	'.':      true,
// 	'/':      true,
// 	'0':      true,
// 	'1':      true,
// 	'2':      true,
// 	'3':      true,
// 	'4':      true,
// 	'5':      true,
// 	'6':      true,
// 	'7':      true,
// 	'8':      true,
// 	'9':      true,
// 	':':      true,
// 	';':      true,
// 	'<':      true,
// 	'=':      true,
// 	'>':      true,
// 	'?':      true,
// 	'@':      true,
// 	'A':      true,
// 	'B':      true,
// 	'C':      true,
// 	'D':      true,
// 	'E':      true,
// 	'F':      true,
// 	'G':      true,
// 	'H':      true,
// 	'I':      true,
// 	'J':      true,
// 	'K':      true,
// 	'L':      true,
// 	'M':      true,
// 	'N':      true,
// 	'O':      true,
// 	'P':      true,
// 	'Q':      true,
// 	'R':      true,
// 	'S':      true,
// 	'T':      true,
// 	'U':      true,
// 	'V':      true,
// 	'W':      true,
// 	'X':      true,
// 	'Y':      true,
// 	'Z':      true,
// 	'[':      true,
// 	'\\':     false,
// 	']':      true,
// 	'^':      true,
// 	'_':      true,
// 	'`':      true,
// 	'a':      true,
// 	'b':      true,
// 	'c':      true,
// 	'd':      true,
// 	'e':      true,
// 	'f':      true,
// 	'g':      true,
// 	'h':      true,
// 	'i':      true,
// 	'j':      true,
// 	'k':      true,
// 	'l':      true,
// 	'm':      true,
// 	'n':      true,
// 	'o':      true,
// 	'p':      true,
// 	'q':      true,
// 	'r':      true,
// 	's':      true,
// 	't':      true,
// 	'u':      true,
// 	'v':      true,
// 	'w':      true,
// 	'x':      true,
// 	'y':      true,
// 	'z':      true,
// 	'{':      true,
// 	'|':      true,
// 	'}':      true,
// 	'~':      true,
// 	'\u007f': true,
// }

const (
	headerSize   = 8 // element size + data size.
	docSizeOff   = 4 //
	keyEntrySize = 6 // keyOff +  keyLen
	keyOriginOff = 4 // offset -> uint32
	valTypeSize  = 1 // TpCode -> byte
	valEntrySize = 5 // TpCode + offset-or-inline-value
	numberSize   = 8 // float64|int64|uint64
)

const (
	LiteralNull byte = iota + 1
	LiteralTrue
	LiteralFalse
)

var (
	//hexChars = "0123456789abcdef"
	endian = binary.LittleEndian
)

var (
	Null = ByteJson{Type: TpCodeLiteral, Data: []byte{LiteralNull}}
)

var (
	escapedChars = map[byte]byte{
		'"': '"',
		'b': '\b',
		'f': '\f',
		'n': '\n',
		'r': '\r',
		't': '\t',
	}
)

var jsonZero = CreateByteJSON(uint64(0))

func CreateByteJSON(in any) ByteJson {
	bj, err := CreateByteJsonWithCheck(in)
	if err != nil {
		panic(err)
	}
	return bj
}

func CreateByteJsonWithCheck(in any) (ByteJson, error) {
	typeCode, buf, err := appendByteJSON(nil, in)
	if err != nil {
		return ByteJson{}, err
	}
	bj := ByteJson{TypeCode: typeCode, Value: buf}
	// GetElemDepth always returns +1.
	if bj.GetElemDepth()-1 > maxJSONDepth {
		return ByteJson{}, ErrJSONDocumentTooDeep
	}
	return bj, nil
}

func appendByteJSON(buf []byte, in any) (TpCode, []byte, error) {
	var typeCode byte
	var err error
	switch x := in.(type) {
	case nil:
		typeCode = TpCodeLiteral
		buf = append(buf, JSONLiteralNil)
	case bool:
		typeCode = JSONTypeCodeLiteral
		if x {
			buf = append(buf, JSONLiteralTrue)
		} else {
			buf = append(buf, JSONLiteralFalse)
		}
	case int64:
		typeCode = JSONTypeCodeInt64
		buf = appendBinaryUint64(buf, uint64(x))
	case uint64:
		typeCode = JSONTypeCodeUint64
		buf = appendBinaryUint64(buf, x)
	case float64:
		typeCode = JSONTypeCodeFloat64
		buf = appendBinaryFloat64(buf, x)
	case json.Number:
		typeCode, buf, err = appendBinaryNumber(buf, x)
		if err != nil {
			return typeCode, nil, errors.Trace(err)
		}
	case string:
		typeCode = JSONTypeCodeString
		buf = appendBinaryString(buf, x)
	case BinaryJSON:
		typeCode = x.TypeCode
		buf = append(buf, x.Value...)
	case []any:
		typeCode = JSONTypeCodeArray
		buf, err = appendBinaryArray(buf, x)
		if err != nil {
			return typeCode, nil, errors.Trace(err)
		}
	case map[string]any:
		typeCode = JSONTypeCodeObject
		buf, err = appendBinaryObject(buf, x)
		if err != nil {
			return typeCode, nil, errors.Trace(err)
		}
	case Opaque:
		typeCode = JSONTypeCodeOpaque
		buf = appendBinaryOpaque(buf, x)
	case Time:
		typeCode = JSONTypeCodeDate
		if x.Type() == mysql.TypeDatetime {
			typeCode = JSONTypeCodeDatetime
		} else if x.Type() == mysql.TypeTimestamp {
			typeCode = JSONTypeCodeTimestamp
		}
		buf = appendBinaryUint64(buf, uint64(x.CoreTime()))
	case Duration:
		typeCode = JSONTypeCodeDuration
		buf = appendBinaryUint64(buf, uint64(x.Duration))
		buf = appendBinaryUint32(buf, uint32(x.Fsp))
	default:
		msg := fmt.Sprintf(unknownTypeErrorMsg, reflect.TypeOf(in))
		err = errors.New(msg)
	}
	return typeCode, buf, err
}
