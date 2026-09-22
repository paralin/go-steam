package gamecoordinator

import (
	"io"

	protobuf "github.com/aperturerobotics/protobuf-go-lite"
	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/steamlang"
)

// IGCMsg carries one coordinator message and its request correlation identities.
type IGCMsg interface {
	Serializer
	IsProto() bool
	GetAppId() uint32
	GetMsgType() uint32

	GetTargetJobId() JobId
	SetTargetJobId(JobId)
	GetSourceJobId() JobId
	SetSourceJobId(JobId)
}

// GCMsgProtobuf binds generated lite codecs to the Steam GC envelope.
type GCMsgProtobuf struct {
	// AppId selects the receiving game coordinator.
	AppId uint32
	// Header carries the message and request correlation identities.
	Header *MsgGCHdrProtoBuf
	// Body supplies the generated binary codec.
	Body protobuf.Message
}

// NewGCMsgProtobuf constructs a coordinator envelope around a generated lite message.
func NewGCMsgProtobuf(appId, msgType uint32, body protobuf.Message) *GCMsgProtobuf {
	hdr := NewMsgGCHdrProtoBuf()
	hdr.Msg = msgType
	return &GCMsgProtobuf{
		AppId:  appId,
		Header: hdr,
		Body:   body,
	}
}

// IsProto reports whether the envelope uses protobuf framing.
func (g *GCMsgProtobuf) IsProto() bool {
	return true
}

// GetAppId returns the coordinator application identity.
func (g *GCMsgProtobuf) GetAppId() uint32 {
	return g.AppId
}

// GetMsgType returns the coordinator message identity.
func (g *GCMsgProtobuf) GetMsgType() uint32 {
	return g.Header.Msg
}

// GetTargetJobId returns the recipient’s request correlation identity.
func (g *GCMsgProtobuf) GetTargetJobId() JobId {
	return JobId(g.Header.Proto.GetJobidTarget())
}

// SetTargetJobId addresses a reply to the recipient’s request.
func (g *GCMsgProtobuf) SetTargetJobId(job JobId) {
	g.Header.Proto.JobidTarget = new(uint64(job))
}

// GetSourceJobId returns the sender’s request correlation identity.
func (g *GCMsgProtobuf) GetSourceJobId() JobId {
	return JobId(g.Header.Proto.GetJobidSource())
}

// SetSourceJobId sets the identity to be echoed by the coordinator.
func (g *GCMsgProtobuf) SetSourceJobId(job JobId) {
	g.Header.Proto.JobidSource = new(uint64(job))
}

// Serialize writes the generated header followed by its message body.
func (g *GCMsgProtobuf) Serialize(w io.Writer) error {
	err := g.Header.Serialize(w)
	if err != nil {
		return err
	}
	body, err := g.Body.MarshalVT()
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// GCMsg retains the non-protobuf coordinator envelope.
type GCMsg struct {
	// AppId selects the receiving game coordinator.
	AppId uint32
	// MsgType identifies the binary payload layout.
	MsgType uint32
	// Header carries legacy request correlation identities.
	Header *MsgGCHdr
	// Body writes the binary payload.
	Body Serializer
}

// NewGCMsg constructs an envelope for a legacy binary message.
func NewGCMsg(appId, msgType uint32, body Serializer) *GCMsg {
	return &GCMsg{
		AppId:   appId,
		MsgType: msgType,
		Header:  NewMsgGCHdr(),
		Body:    body,
	}
}

// GetMsgType returns the coordinator message identity.
func (g *GCMsg) GetMsgType() uint32 {
	return g.MsgType
}

// GetAppId returns the coordinator application identity.
func (g *GCMsg) GetAppId() uint32 {
	return g.AppId
}

// IsProto reports whether the envelope uses protobuf framing.
func (g *GCMsg) IsProto() bool {
	return false
}

// GetTargetJobId returns the recipient’s request correlation identity.
func (g *GCMsg) GetTargetJobId() JobId {
	return JobId(g.Header.TargetJobID)
}

// SetTargetJobId addresses a reply to the recipient’s request.
func (g *GCMsg) SetTargetJobId(job JobId) {
	g.Header.TargetJobID = uint64(job)
}

// GetSourceJobId returns the sender’s request correlation identity.
func (g *GCMsg) GetSourceJobId() JobId {
	return JobId(g.Header.SourceJobID)
}

// SetSourceJobId sets the identity to be echoed by the coordinator.
func (g *GCMsg) SetSourceJobId(job JobId) {
	g.Header.SourceJobID = uint64(job)
}

// Serialize writes the generated header followed by its message body.
func (g *GCMsg) Serialize(w io.Writer) error {
	err := g.Header.Serialize(w)
	if err != nil {
		return err
	}
	err = g.Body.Serialize(w)
	return err
}
