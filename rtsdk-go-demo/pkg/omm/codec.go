package omm

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"
)

// Wire format: simplified RSSL-like binary framing
// [4B total len][1B msg type][1B domain][4B streamID][1B flags][8B seqnum][8B send_ts]
// [2B name_len][name][2B svc_len][svc][1B stream_state][1B data_state][2B text_len][text]
// [2B field_count][per field: 2B fid, 1B type, 8B value or 2B+N string]

const (
	flagSolicited byte = 0x01
	flagComplete  byte = 0x02
)

func Encode(msg *Message) ([]byte, error) {
	size := 1 + 1 + 4 + 1 + 8 + 8
	size += 2 + len(msg.Name) + 2 + len(msg.Service)
	size += 1 + 1 + 2 + len(msg.State.Text)
	size += 2
	for _, f := range msg.Fields {
		size += 2 + 1
		switch f.Type {
		case FieldTypeReal, FieldTypeInt, FieldTypeTime:
			size += 8
		case FieldTypeString:
			size += 2 + len(f.StrVal)
		}
	}

	buf := make([]byte, 4+size)
	binary.BigEndian.PutUint32(buf[0:4], uint32(size))

	off := 4
	buf[off] = byte(msg.Type); off++
	buf[off] = byte(msg.Domain); off++
	binary.BigEndian.PutUint32(buf[off:off+4], uint32(msg.StreamID)); off += 4

	var flags byte
	if msg.Solicited { flags |= flagSolicited }
	if msg.Complete  { flags |= flagComplete }
	buf[off] = flags; off++

	binary.BigEndian.PutUint64(buf[off:off+8], msg.SeqNum); off += 8
	binary.BigEndian.PutUint64(buf[off:off+8], uint64(msg.SendTimeNs)); off += 8

	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(msg.Name))); off += 2
	copy(buf[off:], msg.Name); off += len(msg.Name)

	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(msg.Service))); off += 2
	copy(buf[off:], msg.Service); off += len(msg.Service)

	buf[off] = byte(msg.State.Stream); off++
	buf[off] = byte(msg.State.Data); off++
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(msg.State.Text))); off += 2
	copy(buf[off:], msg.State.Text); off += len(msg.State.Text)

	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(msg.Fields))); off += 2
	for _, f := range msg.Fields {
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(f.ID)); off += 2
		buf[off] = byte(f.Type); off++
		switch f.Type {
		case FieldTypeReal:
			binary.BigEndian.PutUint64(buf[off:off+8], math.Float64bits(f.RealVal)); off += 8
		case FieldTypeInt:
			binary.BigEndian.PutUint64(buf[off:off+8], uint64(f.IntVal)); off += 8
		case FieldTypeTime:
			binary.BigEndian.PutUint64(buf[off:off+8], uint64(f.TimeVal.UnixNano())); off += 8
		case FieldTypeString:
			binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(f.StrVal))); off += 2
			copy(buf[off:], f.StrVal); off += len(f.StrVal)
		}
	}
	return buf[:off], nil
}

func Decode(r io.Reader) (*Message, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	frameLen := binary.BigEndian.Uint32(lenBuf[:])
	if frameLen > 10*1024*1024 {
		return nil, fmt.Errorf("frame too large: %d bytes", frameLen)
	}

	data := make([]byte, frameLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read frame body: %w", err)
	}

	msg := &Message{}
	off := 0
	msg.Type = MsgType(data[off]); off++
	msg.Domain = DomainType(data[off]); off++
	msg.StreamID = int32(binary.BigEndian.Uint32(data[off:off+4])); off += 4

	flags := data[off]; off++
	msg.Solicited = flags&flagSolicited != 0
	msg.Complete = flags&flagComplete != 0

	msg.SeqNum = binary.BigEndian.Uint64(data[off:off+8]); off += 8
	msg.SendTimeNs = int64(binary.BigEndian.Uint64(data[off:off+8])); off += 8

	nameLen := int(binary.BigEndian.Uint16(data[off:off+2])); off += 2
	msg.Name = string(data[off:off+nameLen]); off += nameLen

	svcLen := int(binary.BigEndian.Uint16(data[off:off+2])); off += 2
	msg.Service = string(data[off:off+svcLen]); off += svcLen

	msg.State.Stream = StreamState(data[off]); off++
	msg.State.Data = DataState(data[off]); off++
	stLen := int(binary.BigEndian.Uint16(data[off:off+2])); off += 2
	msg.State.Text = string(data[off:off+stLen]); off += stLen

	fieldCount := int(binary.BigEndian.Uint16(data[off:off+2])); off += 2
	msg.Fields = make(FieldList, fieldCount)
	for i := 0; i < fieldCount; i++ {
		f := &msg.Fields[i]
		f.ID = FieldID(int16(binary.BigEndian.Uint16(data[off:off+2]))); off += 2
		f.Type = FieldType(data[off]); off++
		switch f.Type {
		case FieldTypeReal:
			f.RealVal = math.Float64frombits(binary.BigEndian.Uint64(data[off:off+8])); off += 8
		case FieldTypeInt:
			f.IntVal = int64(binary.BigEndian.Uint64(data[off:off+8])); off += 8
		case FieldTypeTime:
			ns := int64(binary.BigEndian.Uint64(data[off:off+8]))
			f.TimeVal = time.Unix(0, ns); off += 8
		case FieldTypeString:
			sLen := int(binary.BigEndian.Uint16(data[off:off+2])); off += 2
			f.StrVal = string(data[off:off+sLen]); off += sLen
		}
	}
	return msg, nil
}
