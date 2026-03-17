package admin

import (
	"fmt"
	"os"
	"example.com/rtsdk-go-demo/pkg/omm"
)

const (
	LoginStreamID     int32 = 1
	DirectoryStreamID int32 = 2
	DictionaryStreamID int32 = 3
	FirstItemStreamID int32 = 5
)

func MakeLoginRequest(appName string) *omm.Message {
	hostname, _ := os.Hostname()
	if hostname == "" { hostname = "localhost" }
	return &omm.Message{
		Type: omm.MsgTypeRequest, Domain: omm.DomainLogin,
		StreamID: LoginStreamID, Name: fmt.Sprintf("%s@%s", appName, hostname),
		Fields: omm.FieldList{omm.StrField(omm.FID_DSPLY_NAME, appName)},
	}
}

func MakeLoginRefresh(userName string) *omm.Message {
	return &omm.Message{
		Type: omm.MsgTypeRefresh, Domain: omm.DomainLogin,
		StreamID: LoginStreamID, Name: userName,
		State: omm.State{Stream: omm.StreamOpen, Data: omm.DataOk, Text: "Login accepted"},
		Solicited: true, Complete: true,
	}
}

func MakeDirectoryRequest() *omm.Message {
	return &omm.Message{
		Type: omm.MsgTypeRequest, Domain: omm.DomainDirectory,
		StreamID: DirectoryStreamID,
	}
}

func MakeDirectoryRefresh(svc omm.ServiceInfo) *omm.Message {
	return &omm.Message{
		Type: omm.MsgTypeRefresh, Domain: omm.DomainDirectory,
		StreamID: DirectoryStreamID, Name: svc.Name, Service: svc.Name,
		State: omm.State{Stream: omm.StreamOpen, Data: omm.DataOk, Text: "Source Directory Refresh Complete"},
		Fields: omm.FieldList{
			omm.StrField(omm.FID_DSPLY_NAME, svc.Name),
			omm.IntField(omm.FID_ACVOL_1, int64(svc.ID)),
		},
		Solicited: true, Complete: true,
	}
}

func MakeItemRequest(streamID int32, itemName, serviceName string) *omm.Message {
	return &omm.Message{
		Type: omm.MsgTypeRequest, Domain: omm.DomainMarketPrice,
		StreamID: streamID, Name: itemName, Service: serviceName,
	}
}

func MakeItemClose(streamID int32, domain omm.DomainType) *omm.Message {
	return &omm.Message{Type: omm.MsgTypeClose, Domain: domain, StreamID: streamID}
}
