/*
 *
 *  * Licensed to the Apache Software Foundation (ASF) under one or more
 *  * contributor license agreements.  See the NOTICE file distributed with
 *  * this work for additional information regarding copyright ownership.
 *  * The ASF licenses this file to You under the Apache License, Version 2.0
 *  * (the "License"); you may not use this file except in compliance with
 *  * the License.  You may obtain a copy of the License at
 *  *
 *  *     http://www.apache.org/licenses/LICENSE-2.0
 *  *
 *  * Unless required by applicable law or agreed to in writing, software
 *  * distributed under the License is distributed on an "AS IS" BASIS,
 *  * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  * See the License for the specific language governing permissions and
 *  * limitations under the License.
 *
 */

package credis

import (
	"bytes"
	"io"
	"strconv"

	"github.com/IceFireDB/IceFireDB-PubSub/pkg/codis/bufio2"
	"github.com/IceFireDB/IceFireDB-PubSub/pkg/codis/errors"
)

const (
	minItoa = -128
	maxItoa = 32768
)

var (
	itoaOffset [maxItoa - minItoa + 1]uint32
	itoaBuffer string
)

func init() {
	var b bytes.Buffer
	for i := range itoaOffset {
		itoaOffset[i] = uint32(b.Len())
		b.WriteString(strconv.Itoa(i + minItoa))
	}
	itoaBuffer = b.String()
}

func itoa(i int64) string {
	if i >= minItoa && i <= maxItoa {
		beg := itoaOffset[i-minItoa]
		if i == maxItoa {
			return itoaBuffer[beg:]
		}
		end := itoaOffset[i-minItoa+1]
		return itoaBuffer[beg:end]
	}
	return strconv.FormatInt(i, 10)
}

type Encoder struct {
	bw *bufio2.Writer

	Err error
}

var ErrFailedEncoder = errors.New("use of failed encoder")

func NewEncoder(w io.Writer) *Encoder {
	return NewEncoderBuffer(bufio2.NewWriterSize(w, 8192))
}

func NewEncoderSize(w io.Writer, size int) *Encoder {
	return NewEncoderBuffer(bufio2.NewWriterSize(w, size))
}

func NewEncoderBuffer(bw *bufio2.Writer) *Encoder {
	return &Encoder{bw: bw}
}

func (e *Encoder) Encode(r *Resp, flush bool) error {
	if e.Err != nil {
		return errors.Trace(ErrFailedEncoder)
	}
	if err := e.encodeResp(r); err != nil {
		e.Err = err
	} else if flush {
		e.Err = errors.Trace(e.bw.Flush())
	}
	return e.Err
}

func (e *Encoder) EncodeMultiBulk(multi []*Resp, flush bool) error {
	if e.Err != nil {
		return errors.Trace(ErrFailedEncoder)
	}
	if err := e.encodeMultiBulk(multi); err != nil {
		e.Err = err
	} else if flush {
		e.Err = errors.Trace(e.bw.Flush())
	}
	return e.Err
}

func (e *Encoder) Flush() error {
	if e.Err != nil {
		return errors.Trace(ErrFailedEncoder)
	}
	if err := e.bw.Flush(); err != nil {
		e.Err = errors.Trace(err)
	}
	return e.Err
}

func Encode(w io.Writer, r *Resp) error {
	return NewEncoder(w).Encode(r, true)
}

func EncodeToBytes(r *Resp) ([]byte, error) {
	b := &bytes.Buffer{}
	if err := Encode(b, r); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (e *Encoder) encodeResp(r *Resp) error {
	if err := e.bw.WriteByte(byte(r.Type)); err != nil {
		return errors.Trace(err)
	}
	switch r.Type {
	default:
		return errors.Errorf("bad resp type %s", r.Type)
	case TypeString, TypeError, TypeInt:
		return e.encodeTextBytes(r.Value)
	case TypeBulkBytes:
		return e.encodeBulkBytes(r.Value)
	case TypeArray:
		return e.encodeArray(r.Array)
	}
}

func (e *Encoder) encodeMultiBulk(multi []*Resp) error {
	if err := e.bw.WriteByte(byte(TypeArray)); err != nil {
		return errors.Trace(err)
	}
	return e.encodeArray(multi)
}

func (e *Encoder) encodeTextBytes(b []byte) error {
	if _, err := e.bw.Write(b); err != nil {
		return errors.Trace(err)
	}
	if _, err := e.bw.WriteString("\r\n"); err != nil {
		return errors.Trace(err)
	}
	return nil
}

func (e *Encoder) encodeTextString(s string) error {
	if _, err := e.bw.WriteString(s); err != nil {
		return errors.Trace(err)
	}
	if _, err := e.bw.WriteString("\r\n"); err != nil {
		return errors.Trace(err)
	}
	return nil
}

func (e *Encoder) encodeInt(v int64) error {
	return e.encodeTextString(itoa(v))
}

func (e *Encoder) encodeBulkBytes(b []byte) error {
	if b == nil {
		return e.encodeInt(-1)
	} else {
		if err := e.encodeInt(int64(len(b))); err != nil {
			return err
		}
		return e.encodeTextBytes(b)
	}
}

func (e *Encoder) encodeArray(array []*Resp) error {
	if array == nil {
		return e.encodeInt(-1)
	} else {
		if err := e.encodeInt(int64(len(array))); err != nil {
			return err
		}
		for _, r := range array {
			if err := e.encodeResp(r); err != nil {
				return err
			}
		}
		return nil
	}
}
