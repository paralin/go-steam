package gamecoordinator

import (
	"bytes"

	protobuf "github.com/aperturerobotics/protobuf-go-lite"
	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
)

// GCPacket retains a parsed coordinator header and undecoded message bytes.
type GCPacket struct {
	AppId       uint32
	MsgType     uint32
	IsProto     bool
	GCName      string
	Body        []byte
	TargetJobId JobId
}

// NewGCPacket parses protobuf or legacy headers while preserving job correlation.
func NewGCPacket(wrapper *CMsgGCClient) (*GCPacket, error) {
	packet := &GCPacket{
		AppId:   wrapper.GetAppid(),
		MsgType: wrapper.GetMsgtype(),
		GCName:  wrapper.GetGcname(),
	}

	r := bytes.NewReader(wrapper.GetPayload())
	if IsProto(wrapper.GetMsgtype()) {
		packet.MsgType = packet.MsgType & EMsgMask
		packet.IsProto = true

		header := NewMsgGCHdrProtoBuf()
		err := header.Deserialize(r)
		if err != nil {
			return nil, err
		}
		packet.TargetJobId = JobId(header.Proto.GetJobidTarget())
	} else {
		header := NewMsgGCHdr()
		err := header.Deserialize(r)
		if err != nil {
			return nil, err
		}
		packet.TargetJobId = JobId(header.TargetJobID)
	}

	body := make([]byte, r.Len())
	r.Read(body)
	packet.Body = body

	return packet, nil
}

// ReadProtoMsg replaces the supplied message using its generated lite decoder.
func (g *GCPacket) ReadProtoMsg(body protobuf.Message) {
	body.Reset()
	_ = body.UnmarshalVT(g.Body)
}

// ReadMsg decodes the retained binary body into the supplied message.
func (g *GCPacket) ReadMsg(body MessageBody) {
	body.Deserialize(bytes.NewReader(g.Body))
}
