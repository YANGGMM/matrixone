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

package orderedcodec

import (
	"bytes"
	"errors"
	"github.com/matrixorigin/matrixone/pkg/container/types"
)

var (
	errorNoEnoughBytesForDecoding = errors.New("there is no enough bytes for decoding")
	errorIsNotNull = errors.New("it is not the null encoding")
	errorUVarintLengthIsWrong = errors.New("wrong uvarint length")
	errorNoBytesPrefix = errors.New("missing bytes prefix")
	errorIncompleteBytesWithZero = errors.New("bytes without zero - incomplete bytes")
	errorIncompleteBytesWithSuffix = errors.New("bytes without suffix byte - incomplete bytes")
	errorWrongEscapedBytes = errors.New("missing second byte of escaping")
	errorUnmatchedValueType = errors.New("unmatched value type")
)

//DecodeKey decodes
func (od *OrderedDecoder) DecodeKey(data []byte)([]byte, *DecodedItem,error){
	if data == nil || len(data) < 1 {
		return data,nil,errorNoEnoughBytesForDecoding
	}
	dataAfterNull,decodeItem,err := od.IsNull(data)
	if err == nil {
		return dataAfterNull,decodeItem,nil
	}
	if (data[0] & encodingPrefixForIntegerMinimum) ==
			encodingPrefixForIntegerMinimum {
		return od.DecodeUint64(data)
	}else if data[0] == encodingPrefixForBytes {
		return od.DecodeBytes(data)
	}else{
		return nil, nil, errorDoNotComeHere
	}
	return nil, nil, nil
}

// isNll decodes the NULL and returns the bytes after the null.
func (od *OrderedDecoder) IsNull(data []byte) ([]byte,*DecodedItem,error) {
	if data == nil || len(data) < 1 {
		return data,nil,errorNoEnoughBytesForDecoding
	}
	if data[0] != nullEncoding {
		return data,nil,errorIsNotNull
	}
	return data[1:], NewDecodeItem(nil,VALUE_TYPE_NULL,0,0,1), nil
}

// DecodeUint64  decodes the uint64 with the variable length encoding
// and returns the bytes after the uint64
func (od *OrderedDecoder) DecodeUint64(data []byte)([]byte,*DecodedItem,error) {
	if data == nil || len(data) < 1 {
		return nil,nil,errorNoEnoughBytesForDecoding
	}
	//get length from the first byte
	l := int(data[0]) - encodingPrefixForIntegerZero
	//skip the first byte
	data = data[1:]
	if l <= encodingPrefixForSplit {//[0,109]
		return data,NewDecodeItem(uint64(l),VALUE_TYPE_UINT64,0,0,1),nil
	}
	// >= 109
	l -= encodingPrefixForSplit
	if l < 0 || l > 8{
		return nil,nil,errorUVarintLengthIsWrong
	}
	if len(data) < l {
		return nil, nil, errorNoEnoughBytesForDecoding
	}

	value := uint64(0)
	for _, b := range data[:l] {
		value <<= 8
		value |= uint64(b)
	}
	return data[l:], NewDecodeItem(value,VALUE_TYPE_UINT64,0,0,l+1), nil
}

// DecodeBytes decodes the bytes from the encoded bytes.
func (od *OrderedDecoder) DecodeBytes(data []byte)([]byte,*DecodedItem,error) {
	return od.decodeBytes(data,nil)
}

// decodeBytes decodes the bytes from the encoded bytes.
func (od *OrderedDecoder) decodeBytes(data []byte,value []byte)([]byte,*DecodedItem,error) {
	if data == nil || len(data) < 1 {
		return nil,nil,errorNoEnoughBytesForDecoding
	}
	if data[0] != encodingPrefixForBytes {
		return nil, nil, errorNoBytesPrefix
	}

	//skip bytes prefix
	data = data[1:]

	l := 0

	for  {
		p := bytes.IndexByte(data,byteToBeEscaped)
		if p == -1 {
			return nil, nil, errorIncompleteBytesWithZero
		}

		//without suffix byte
		if p == len(data) - 1 {
			return nil, nil, errorIncompleteBytesWithSuffix
		}

		nextByte := data[p+1]
		if nextByte == byteForBytesEnding {//ending bytes
			l += p + 2
			value = append(value,data[:p]...)
			return data[p+2:], NewDecodeItem(value,VALUE_TYPE_BYTES,0,0,l), nil
		}
		if nextByte != byteEscapedToSecondByte {
			return nil, nil, errorWrongEscapedBytes
		}

		//handle escaping
		l += p + 2
		value = append(value,data[:p]...)
		value = append(value, byteToBeEscaped)
		data = data[p+2:]
	}
	return nil, nil, errorDoNotComeHere
}

// DecodeString decodes string from the encoded bytes
func (od *OrderedDecoder) DecodeString(data []byte)([]byte,*DecodedItem,error) {
	data2,di,err := od.DecodeBytes(data)
	if err != nil {
		return nil, nil, err
	}
	di.ValueType = VALUE_TYPE_STRING
	bt := di.Value.([]byte)
	di.Value = string(bt)
	return data2,di,err
}

func NewOrderedDecoder() *OrderedDecoder {
	return &OrderedDecoder{}
}

func (di *DecodedItem) GetInt8() (int8,error) {
	if di.ValueType != VALUE_TYPE_INT8 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(int8); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetInt16() (int16,error) {
	if di.ValueType != VALUE_TYPE_INT16 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(int16); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetInt32() (int32,error) {
	if di.ValueType != VALUE_TYPE_INT32 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(int32); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetInt64() (int64,error) {
	if di.ValueType != VALUE_TYPE_INT64 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(int64); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetUint8() (uint8,error) {
	if di.ValueType != VALUE_TYPE_UINT8 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(uint8); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetUint16() (uint16,error) {
	if di.ValueType != VALUE_TYPE_UINT16 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(uint16); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetUint32() (uint32,error) {
	if di.ValueType != VALUE_TYPE_UINT32 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(uint32); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetUint64() (uint64,error) {
	if di.ValueType != VALUE_TYPE_UINT64 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(uint64); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetFloat32() (float32,error) {
	if di.ValueType != VALUE_TYPE_FLOAT32 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(float32); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetFloat64() (float64,error) {
	if di.ValueType != VALUE_TYPE_FLOAT64 {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(float64); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetBytes() ([]byte,error) {
	if di.ValueType == VALUE_TYPE_BYTES {
		if v,ok := di.Value.([]byte); !ok {
			return nil, errorUnmatchedValueType
		}else{
			return v,nil
		}
	}else if di.ValueType == VALUE_TYPE_STRING {
		if v, ok := di.Value.(string); !ok {
			return nil, errorUnmatchedValueType
		} else {
			return []byte(v), nil
		}
	}
	return nil, errorUnmatchedValueType
}

func (di *DecodedItem) GetDate() (types.Date,error) {
	if di.ValueType != VALUE_TYPE_DATE {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(types.Date); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}

func (di *DecodedItem) GetDatetime() (types.Datetime,error) {
	if di.ValueType != VALUE_TYPE_DATETIME {
		return 0, errorUnmatchedValueType
	}
	if v,ok := di.Value.(types.Datetime); !ok {
		return 0, errorUnmatchedValueType
	}else{
		return v,nil
	}
}