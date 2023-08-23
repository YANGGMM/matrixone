// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package frontend

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/container/bytejson"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/defines"
	"github.com/matrixorigin/matrixone/pkg/fileservice"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers/tree"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"strconv"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
)

const (
	escape byte = '"'
)

type ExportConfig struct {
	// configs from user input
	userConfig *tree.ExportParam

	// curFileSize
	curFileSize uint64
	rows        uint64
	fileCnt     uint
	columnFlag  []bool   // column : force quote or not
	symbol      [][]byte // field separator of line initialized before export
	// default flush size
	defaultBufSize int64
	outputStr      []byte
	lineSize       uint64

	writtenCsvBytes uint64

	//file service & buffer for the line
	seFileService bool

	diskFileConfig
	fsConfig
}

func initExportConfig(ctx context.Context, ec *ExportConfig, mrs *MysqlResultSet, bufSize int64) error {
	var err error
	//1. basic init
	ec.defaultBufSize = bufSize
	ec.defaultBufSize *= 1024 * 1024
	n := (int)(mrs.GetColumnCount())
	if n <= 0 {
		return moerr.NewInternalError(ctx, "the column count is zero")
	}
	// field separator
	ec.symbol = make([][]byte, n)
	for i := 0; i < n-1; i++ {
		ec.symbol[i] = []byte(ec.userConfig.Fields.Terminated)
	}
	//line terminated
	ec.symbol[n-1] = []byte(ec.userConfig.Lines.TerminatedBy)
	//force quote column flag
	ec.columnFlag = make([]bool, len(mrs.Name2Index))
	for i := 0; i < len(ec.userConfig.ForceQuote); i++ {
		col, ok := mrs.Name2Index[ec.userConfig.ForceQuote[i]]
		if ok {
			ec.columnFlag[col] = true
		}
	}

	//2. init first file
	if err = openNewFile(ctx, ec, mrs); err != nil {
		return err
	}
	return err
}

// needExportToFile checks needing to export into file or not
func (ec *ExportConfig) needExportToFile() bool {
	return ec != nil && ec.userConfig != nil && ec.userConfig.Outfile
}

// sendBatch sends the batch from pipeline to the export routine
func (ec *ExportConfig) sendBatch(bat *batch.Batch) {
	if bat != nil {
		ec.batchChan <- bat
	}
}

// initAsyncExport starts the async export routine
func initAsyncExport(ctx context.Context, ses *Session, ec *ExportConfig) {
	if !ec.first {
		ec.first = true
		ec.toDiskFile = true
		ec.batchChan = make(chan *batch.Batch)
		ec.asyncGroup2 = new(errgroup.Group)
		ec.asyncGroup2.Go(func() error {
			return doExport(ctx, ses, ec)
		})
	}
}

// doExport the export function
func doExport(ctx context.Context, ses *Session, ec *ExportConfig) error {
	var err error
	var quit bool
	var bat *batch.Batch
	var ok bool
	for {
		select {
		case <-ctx.Done():
			quit = true
		case bat, ok = <-ec.batchChan:
			if !ok {
				quit = true
			}
		}
		if quit {
			break
		}

		if bat == nil {
			continue
		}

		oq := NewOutputQueue(ctx, ses, len(bat.Vecs), nil, ec)
		err = writeBatch(ctx, ses, oq, bat)
		if err != nil {
			return err
		}

		err = oq.flush()
		if err != nil {
			return err
		}
	}

	return err
}

// writeBatch writes data batch into file
func writeBatch(ctx context.Context, ses *Session, oq *outputQueue, bat *batch.Batch) error {
	var err error
	n := bat.Vecs[0].Length()
	defer func() {
		bat.Clean(ses.GetMemPool())
	}()
	for j := 0; j < n; j++ { //row index
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		//do not need copy bytes here. because the batch has been copied.
		_, err = extractRowFromEveryVector(ses, bat, j, oq, false)
		if err != nil {
			return err
		}
		//if oq.showStmtType == ShowTableStatus {
		//	row2 := make([]interface{}, len(row))
		//	copy(row2, row)
		//	ses.AppendData(row2)
		//}
	}
	return err
}

// fsConfig writes into fileservice for save query result
type fsConfig struct {
	fileService fileservice.FileService
	lineBuffer  *bytes.Buffer
	ctx         context.Context
	asyncReader *io.PipeReader
	asyncWriter *io.PipeWriter
	asyncGroup  *errgroup.Group
}

// diskFileConfig writes into disk file
type diskFileConfig struct {
	first bool

	// file handler
	file *os.File

	// bufio.writer
	writer *bufio.Writer

	// DiskFile can be seeked and truncated.
	toDiskFile bool

	batchChan   chan *batch.Batch
	asyncGroup2 *errgroup.Group
}

var openFile = os.OpenFile

var flush = func(ep *ExportConfig) error {
	if !ep.seFileService {
		return ep.writer.Flush()
	}
	return nil
}

var seek = func(ep *ExportConfig) (int64, error) {
	if !ep.seFileService {
		return ep.file.Seek(int64(ep.curFileSize-ep.lineSize), io.SeekStart)
	}
	return 0, nil
}

var read = func(ep *ExportConfig) (int, error) {
	if !ep.seFileService {
		ep.outputStr = make([]byte, ep.lineSize)
		return ep.file.Read(ep.outputStr)
	} else {
		ep.outputStr = make([]byte, ep.lineSize)
		copy(ep.outputStr, ep.lineBuffer.Bytes())
		ep.lineBuffer.Reset()
		return int(ep.lineSize), nil
	}
}

var truncate = func(ep *ExportConfig) error {
	if !ep.seFileService {
		return ep.file.Truncate(int64(ep.curFileSize - ep.lineSize))
	} else {
		return nil
	}
}

var closeFile = func(ep *ExportConfig) error {
	var err error
	if !ep.seFileService {
		ep.fileCnt++
		if err = flush(ep); err != nil {
			return err
		}
		return ep.file.Close()
	} else {
		ep.fileCnt++
		err = ep.asyncWriter.Close()
		if err != nil {
			return err
		}
		err = ep.asyncGroup.Wait()
		if err != nil {
			return err
		}
		err = ep.asyncReader.Close()
		if err != nil {
			return err
		}
		ep.asyncReader = nil
		ep.asyncWriter = nil
		ep.asyncGroup = nil
		return err
	}
}

var write = func(ep *ExportConfig, output []byte) (int, error) {
	if !ep.seFileService {
		return ep.writer.Write(output)
	} else {
		return ep.lineBuffer.Write(output)
	}
}

var endOfLine = func(ep *ExportConfig) (int, error) {
	if ep.seFileService {
		n, err := ep.asyncWriter.Write(ep.lineBuffer.Bytes())
		if err != nil {
			err2 := ep.asyncWriter.CloseWithError(err)
			if err2 != nil {
				return 0, err2
			}
		}
		ep.lineBuffer.Reset()
		return n, err
	}
	return 0, nil
}

var openNewFile = func(ctx context.Context, ep *ExportConfig, mrs *MysqlResultSet) error {
	lineSize := ep.lineSize
	var err error
	ep.curFileSize = 0
	if !ep.seFileService {
		filePath := genExportFilePath(ep.userConfig.FilePath, ep.fileCnt)
		ep.file, err = openFile(filePath, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0o666)
		if err != nil {
			return err
		}
		ep.writer = bufio.NewWriterSize(ep.file, int(ep.defaultBufSize))
	} else {
		//default 1MB
		if ep.lineBuffer == nil {
			ep.lineBuffer = &bytes.Buffer{}
		} else {
			ep.lineBuffer.Reset()
		}
		ep.asyncReader, ep.asyncWriter = io.Pipe()
		filePath := genExportFilePath(ep.userConfig.FilePath, ep.fileCnt)

		asyncWriteFunc := func() error {
			vec := fileservice.IOVector{
				FilePath: filePath,
				Entries: []fileservice.IOEntry{
					{
						ReaderForWrite: ep.asyncReader,
						Size:           -1,
					},
				},
			}
			err := ep.fileService.Write(ctx, vec)
			if err != nil {
				err2 := ep.asyncReader.CloseWithError(err)
				if err2 != nil {
					return err2
				}
			}
			return err
		}

		ep.asyncGroup = new(errgroup.Group)
		ep.asyncGroup.Go(asyncWriteFunc)
	}
	if ep.userConfig.Header {
		var header string
		n := len(mrs.Columns)
		if n == 0 {
			return nil
		}
		for i := 0; i < n-1; i++ {
			header += mrs.Columns[i].Name() + ep.userConfig.Fields.Terminated
		}
		header += mrs.Columns[n-1].Name() + ep.userConfig.Lines.TerminatedBy
		if ep.userConfig.MaxFileSize != 0 && uint64(len(header)) >= ep.userConfig.MaxFileSize {
			return moerr.NewInternalError(ctx, "the header line size is over the maxFileSize")
		}
		if err := writeFile(ep, []byte(header)); err != nil {
			return err
		}
		if _, err := endOfLine(ep); err != nil {
			return err
		}
	}
	if lineSize != 0 {
		ep.lineSize = 0
		ep.rows = 0
		if err := writeFile(ep, ep.outputStr); err != nil {
			return err
		}
	}
	return nil
}

func genExportFilePath(filename string, fileCnt uint) string {
	if fileCnt == 0 {
		return filename
	} else {
		return fmt.Sprintf("%s.%d", filename, fileCnt)
	}
}

var formatOutputString = func(oq *outputQueue, tmp, symbol []byte, enclosed byte, flag bool) error {
	var err error
	if flag {
		if err = writeBytesToFile(oq, []byte{enclosed}); err != nil {
			return err
		}
	}
	if err = writeBytesToFile(oq, tmp); err != nil {
		return err
	}
	if flag {
		if err = writeBytesToFile(oq, []byte{enclosed}); err != nil {
			return err
		}
	}
	if err = writeBytesToFile(oq, symbol); err != nil {
		return err
	}
	return nil
}

// writeBytesToFile writes bytes into file.
// if the file size is over the maxFileSize, it will close the file and open a new file.
func writeBytesToFile(oq *outputQueue, output []byte) error {
	if oq.ep.userConfig.MaxFileSize != 0 && oq.ep.curFileSize+uint64(len(output)) > oq.ep.userConfig.MaxFileSize {
		if err := flush(oq.ep); err != nil {
			return err
		}
		if oq.ep.lineSize != 0 && oq.ep.toDiskFile {
			if _, err := seek(oq.ep); err != nil {
				return err
			}
			for {
				if n, err := read(oq.ep); err != nil {
					return err
				} else if uint64(n) == oq.ep.lineSize {
					break
				}
			}
			if err := truncate(oq.ep); err != nil {
				return err
			}
		}
		if err := closeFile(oq.ep); err != nil {
			return err
		}
		if err := openNewFile(oq.ctx, oq.ep, oq.mrs); err != nil {
			return err
		}
	}

	if err := writeFile(oq.ep, output); err != nil {
		return err
	}
	return nil
}

var writeFile = func(ep *ExportConfig, output []byte) error {
	for {
		if n, err := write(ep, output); err != nil {
			return err
		} else if n == len(output) {
			break
		}
	}
	ep.lineSize += uint64(len(output))
	ep.curFileSize += uint64(len(output))
	ep.writtenCsvBytes += uint64(len(output))
	return nil
}

func addEscapeToString(s []byte) []byte {
	pos := make([]int, 0)
	for i := 0; i < len(s); i++ {
		if s[i] == escape {
			pos = append(pos, i)
		}
	}
	if len(pos) == 0 {
		return s
	}
	ret := make([]byte, 0)
	cur := 0
	for i := 0; i < len(pos); i++ {
		ret = append(ret, s[cur:pos[i]]...)
		ret = append(ret, escape)
		cur = pos[i]
	}
	ret = append(ret, s[cur:]...)
	return ret
}

// exportDataToCSVFile
func exportDataToCSVFile(oq *outputQueue) error {
	oq.ep.lineSize = 0

	symbol := oq.ep.symbol
	closeby := oq.ep.userConfig.Fields.EnclosedBy
	flag := oq.ep.columnFlag
	for i := uint64(0); i < oq.mrs.GetColumnCount(); i++ {
		column, err := oq.mrs.GetColumn(oq.ctx, i)
		if err != nil {
			return err
		}
		mysqlColumn, ok := column.(*MysqlColumn)
		if !ok {
			return moerr.NewInternalError(oq.ctx, "sendColumn need MysqlColumn")
		}
		if isNil, err := oq.mrs.ColumnIsNull(oq.ctx, 0, i); err != nil {
			return err
		} else if isNil {
			//NULL is output as \N
			if err = formatOutputString(oq, []byte{'\\', 'N'}, symbol[i], closeby, false); err != nil {
				return err
			}
			continue
		}

		switch mysqlColumn.ColumnType() {
		case defines.MYSQL_TYPE_DECIMAL:
			value, err := oq.mrs.GetString(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if err = formatOutputString(oq, []byte(value), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_BOOL:
			value, err := oq.mrs.GetString(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if err = formatOutputString(oq, []byte(value), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_TINY, defines.MYSQL_TYPE_SHORT, defines.MYSQL_TYPE_INT24, defines.MYSQL_TYPE_LONG, defines.MYSQL_TYPE_YEAR:
			value, err := oq.mrs.GetInt64(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if mysqlColumn.ColumnType() == defines.MYSQL_TYPE_YEAR {
				if value == 0 {
					if err = formatOutputString(oq, []byte("0000"), symbol[i], closeby, flag[i]); err != nil {
						return err
					}
				} else {
					oq.resetLineStr()
					oq.lineStr = strconv.AppendInt(oq.lineStr, value, 10)
					if err = formatOutputString(oq, oq.lineStr, symbol[i], closeby, flag[i]); err != nil {
						return err
					}
				}
			} else {
				oq.resetLineStr()
				oq.lineStr = strconv.AppendInt(oq.lineStr, value, 10)
				if err = formatOutputString(oq, oq.lineStr, symbol[i], closeby, flag[i]); err != nil {
					return err
				}
			}
		case defines.MYSQL_TYPE_FLOAT, defines.MYSQL_TYPE_DOUBLE:
			value, err := oq.mrs.GetFloat64(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			oq.lineStr = []byte(fmt.Sprintf("%v", value))
			if err = formatOutputString(oq, oq.lineStr, symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_LONGLONG:
			if uint32(mysqlColumn.Flag())&defines.UNSIGNED_FLAG != 0 {
				if value, err := oq.mrs.GetUint64(oq.ctx, 0, i); err != nil {
					return err
				} else {
					oq.resetLineStr()
					oq.lineStr = strconv.AppendUint(oq.lineStr, value, 10)
					if err = formatOutputString(oq, oq.lineStr, symbol[i], closeby, flag[i]); err != nil {
						return err
					}
				}
			} else {
				if value, err := oq.mrs.GetInt64(oq.ctx, 0, i); err != nil {
					return err
				} else {
					oq.resetLineStr()
					oq.lineStr = strconv.AppendInt(oq.lineStr, value, 10)
					if err = formatOutputString(oq, oq.lineStr, symbol[i], closeby, flag[i]); err != nil {
						return err
					}
				}
			}
		// Binary/varbinary has mysql_type_varchar.
		case defines.MYSQL_TYPE_VARCHAR, defines.MYSQL_TYPE_VAR_STRING, defines.MYSQL_TYPE_STRING,
			defines.MYSQL_TYPE_BLOB, defines.MYSQL_TYPE_TEXT:
			value, err := oq.mrs.GetValue(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			value = addEscapeToString(value.([]byte))
			if err = formatOutputString(oq, value.([]byte), symbol[i], closeby, true); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_DATE:
			value, err := oq.mrs.GetValue(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if err = formatOutputString(oq, []byte(value.(types.Date).String()), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_TIME:
			value, err := oq.mrs.GetValue(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if err = formatOutputString(oq, []byte(value.(types.Time).String()), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_DATETIME:
			value, err := oq.mrs.GetValue(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if err = formatOutputString(oq, []byte(value.(string)), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_TIMESTAMP:
			value, err := oq.mrs.GetString(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if err = formatOutputString(oq, []byte(value), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_JSON:
			value, err := oq.mrs.GetValue(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			jsonStr := value.(bytejson.ByteJson).String()
			if err = formatOutputString(oq, []byte(jsonStr), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		case defines.MYSQL_TYPE_UUID:
			value, err := oq.mrs.GetString(oq.ctx, 0, i)
			if err != nil {
				return err
			}
			if err = formatOutputString(oq, []byte(value), symbol[i], closeby, flag[i]); err != nil {
				return err
			}
		default:
			return moerr.NewInternalError(oq.ctx, "unsupported column type %d ", mysqlColumn.ColumnType())
		}
	}
	oq.ep.rows++
	_, err := endOfLine(oq.ep)
	return err
}

func finishExport(ctx context.Context, ses *Session, ec *ExportConfig) error {
	var err error
	if !ec.seFileService {
		//close chan to notify the writer to quit
		close(ec.batchChan)
		err = ec.asyncGroup2.Wait()
		defer func() {
			ec.asyncGroup2 = nil
			ec.batchChan = nil
		}()
		if err != nil {
			return err
		}

		if err = flush(ec); err != nil {
			return err
		}
		if err = closeFile(ec); err != nil {
			return err
		}
		ses.writeCsvBytes.Add(int64(ec.writtenCsvBytes))
	} else {
		ec.lineBuffer = nil
		ec.outputStr = nil
		if ec.asyncReader != nil {
			_ = ec.asyncReader.Close()
		}
		if ec.asyncWriter != nil {
			_ = ec.asyncWriter.Close()
		}
		ec.fileService = nil
		ec.ctx = nil
	}
	return err
}

func copyBatch(ses *Session, bat *batch.Batch) (*batch.Batch, error) {
	var err error
	bat2 := batch.NewWithSize(len(bat.Vecs))
	for i, vec := range bat.Vecs {
		bat2.Vecs[i], err = vec.Dup(ses.GetMemPool())
		if err != nil {
			return nil, err
		}
	}
	bat2.SetRowCount(bat.RowCount())
	return bat2, err
}
