// Package omm implements a simplified Open Message Model (OMM) compatible with
// the concepts in LSEG's Real-Time SDK. It provides message types, domain models,
// field definitions, and binary codec for demonstration purposes.
package omm

import (
	"fmt"
	"time"
)

// Domain Types — mirror RTSDK RDM domain model identifiers
type DomainType uint8

const (
	DomainLogin       DomainType = 1
	DomainDirectory   DomainType = 4
	DomainDictionary  DomainType = 5
	DomainMarketPrice DomainType = 6
	DomainMarketByOrder DomainType = 7
	DomainMarketByPrice DomainType = 8
	DomainSymbolList  DomainType = 10
)

func (d DomainType) String() string {
	switch d {
	case DomainLogin:       return "Login"
	case DomainDirectory:   return "Directory"
	case DomainDictionary:  return "Dictionary"
	case DomainMarketPrice: return "MarketPrice"
	case DomainMarketByOrder: return "MarketByOrder"
	case DomainMarketByPrice: return "MarketByPrice"
	case DomainSymbolList:  return "SymbolList"
	default: return fmt.Sprintf("Domain(%d)", d)
	}
}

// Message Types — mirror RTSDK OMM message model
type MsgType uint8

const (
	MsgTypeRequest MsgType = 1
	MsgTypeRefresh MsgType = 2
	MsgTypeUpdate  MsgType = 3
	MsgTypeStatus  MsgType = 4
	MsgTypeClose   MsgType = 5
	MsgTypePost    MsgType = 6
	MsgTypeAck     MsgType = 7
	MsgTypeGeneric MsgType = 8
)

func (m MsgType) String() string {
	names := map[MsgType]string{
		MsgTypeRequest: "RequestMsg", MsgTypeRefresh: "RefreshMsg",
		MsgTypeUpdate: "UpdateMsg", MsgTypeStatus: "StatusMsg",
		MsgTypeClose: "CloseMsg", MsgTypePost: "PostMsg",
		MsgTypeAck: "AckMsg", MsgTypeGeneric: "GenericMsg",
	}
	if n, ok := names[m]; ok { return n }
	return fmt.Sprintf("MsgType(%d)", m)
}

// Stream/Data States — mirror RTSDK OmmState
type StreamState uint8
const (
	StreamOpen        StreamState = 1
	StreamNonStreaming StreamState = 2
	StreamClosed      StreamState = 3
	StreamRedirected  StreamState = 4
)

type DataState uint8
const (
	DataOk       DataState = 1
	DataSuspect  DataState = 2
	DataNoChange DataState = 3
)

type State struct {
	Stream StreamState
	Data   DataState
	Text   string
}

func (s State) String() string {
	sn := map[StreamState]string{1:"Open",2:"NonStreaming",3:"Closed",4:"Redirected"}
	dn := map[DataState]string{1:"Ok",2:"Suspect",3:"NoChange"}
	if s.Text != "" { return fmt.Sprintf("%s/%s/%s", sn[s.Stream], dn[s.Data], s.Text) }
	return fmt.Sprintf("%s/%s", sn[s.Stream], dn[s.Data])
}

// Field IDs — subset of RDMFieldDictionary (real FID numbers)
type FieldID int16
const (
	FID_BID        FieldID = 22
	FID_ASK        FieldID = 25
	FID_LAST       FieldID = 6
	FID_BIDSIZE    FieldID = 30
	FID_ASKSIZE    FieldID = 31
	FID_ACVOL_1    FieldID = 32
	FID_NETCHNG_1  FieldID = 11
	FID_HIGH_1     FieldID = 12
	FID_LOW_1      FieldID = 13
	FID_OPEN_PRC   FieldID = 19
	FID_TRDTIM_1   FieldID = 5
	FID_QUOTIM     FieldID = 1025
	FID_DSPLY_NAME FieldID = 3
)

func FieldName(fid FieldID) string {
	names := map[FieldID]string{
		FID_BID:"BID", FID_ASK:"ASK", FID_LAST:"TRDPRC_1",
		FID_BIDSIZE:"BIDSIZE", FID_ASKSIZE:"ASKSIZE", FID_ACVOL_1:"ACVOL_1",
		FID_NETCHNG_1:"NETCHNG_1", FID_HIGH_1:"HIGH_1", FID_LOW_1:"LOW_1",
		FID_OPEN_PRC:"OPEN_PRC", FID_TRDTIM_1:"TRDTIM_1", FID_QUOTIM:"QUOTIM",
		FID_DSPLY_NAME:"DSPLY_NAME",
	}
	if n, ok := names[fid]; ok { return n }
	return fmt.Sprintf("FID_%d", fid)
}

// Field value types — mirrors RWF typed primitives
type FieldType uint8
const (
	FieldTypeReal   FieldType = 1
	FieldTypeInt    FieldType = 2
	FieldTypeString FieldType = 3
	FieldTypeTime   FieldType = 4
)

type Field struct {
	ID      FieldID
	Type    FieldType
	RealVal float64
	IntVal  int64
	StrVal  string
	TimeVal time.Time
}

func (f Field) String() string {
	switch f.Type {
	case FieldTypeReal:   return fmt.Sprintf("%s=%.4f", FieldName(f.ID), f.RealVal)
	case FieldTypeInt:    return fmt.Sprintf("%s=%d", FieldName(f.ID), f.IntVal)
	case FieldTypeString: return fmt.Sprintf("%s=%q", FieldName(f.ID), f.StrVal)
	case FieldTypeTime:   return fmt.Sprintf("%s=%s", FieldName(f.ID), f.TimeVal.Format("15:04:05"))
	default: return fmt.Sprintf("%s=?", FieldName(f.ID))
	}
}

func RealField(id FieldID, val float64) Field { return Field{ID: id, Type: FieldTypeReal, RealVal: val} }
func IntField(id FieldID, val int64) Field    { return Field{ID: id, Type: FieldTypeInt, IntVal: val} }
func StrField(id FieldID, val string) Field   { return Field{ID: id, Type: FieldTypeString, StrVal: val} }
func TimeField(id FieldID, val time.Time) Field { return Field{ID: id, Type: FieldTypeTime, TimeVal: val} }

// FieldList — the primary OMM container for Market Price data
type FieldList []Field

func (fl FieldList) Get(id FieldID) *Field {
	for i := range fl {
		if fl[i].ID == id { return &fl[i] }
	}
	return nil
}

// Message — the unified OMM message envelope
type Message struct {
	Type      MsgType
	Domain    DomainType
	StreamID  int32
	Name      string
	Service   string
	State     State
	Fields    FieldList
	Solicited bool
	Complete  bool
	SeqNum    uint64
	SendTimeNs int64
	RecvTimeNs int64
}

func (m *Message) String() string {
	s := fmt.Sprintf("%s [stream=%d domain=%s", m.Type, m.StreamID, m.Domain)
	if m.Name != "" { s += fmt.Sprintf(" name=%q", m.Name) }
	if m.Service != "" { s += fmt.Sprintf(" service=%q", m.Service) }
	if m.Type == MsgTypeRefresh || m.Type == MsgTypeStatus { s += fmt.Sprintf(" state=%s", m.State) }
	s += "]"
	if len(m.Fields) > 0 {
		s += "\n"
		for _, f := range m.Fields { s += fmt.Sprintf("    %s\n", f) }
	}
	return s
}

// ServiceInfo — used in Directory domain responses
type ServiceInfo struct {
	Name  string
	ID    uint16
	State string
}

func DefaultService() ServiceInfo {
	return ServiceInfo{Name: "DIRECT_FEED", ID: 1, State: "Up"}
}
