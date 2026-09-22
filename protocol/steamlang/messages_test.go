package steamlang

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestBinaryHeaderLayout pins job IDs, field widths, defaults, and byte order.
func TestBinaryHeaderLayout(t *testing.T) {
	header := NewMsgHdr()
	header.Msg = EMsg_ChannelEncryptRequest
	header.SourceJobID = 0x0102030405060708
	var wire bytes.Buffer
	if err := header.Serialize(&wire); err != nil {
		t.Fatal(err)
	}

	if got, want := hex.EncodeToString(wire.Bytes()), "17050000ffffffffffffffff0807060504030201"; got != want {
		t.Fatalf("header bytes: %s, want %s", got, want)
	}
	var decoded MsgHdr
	if err := decoded.Deserialize(&wire); err != nil {
		t.Fatal(err)
	}
	if decoded != *header {
		t.Fatalf("decoded header: %+v", decoded)
	}
}

// TestFixedBufferDecode consumes fixed arrays from a zero-value message.
func TestFixedBufferDecode(t *testing.T) {
	message := NewMsgClientNewLoginKey()
	message.UniqueID = 7
	copy(message.LoginKey, "abcdefghijklmnopqrst")
	var wire bytes.Buffer
	if err := message.Serialize(&wire); err != nil {
		t.Fatal(err)
	}

	var decoded MsgClientNewLoginKey
	if err := decoded.Deserialize(&wire); err != nil {
		t.Fatal(err)
	}
	if decoded.UniqueID != 7 || !bytes.Equal(decoded.LoginKey, message.LoginKey) || wire.Len() != 0 {
		t.Fatalf("fixed buffer did not consume the message: %+v", decoded)
	}
	if err := (&MsgClientNewLoginKey{LoginKey: []byte{1}}).Serialize(&wire); err == nil {
		t.Fatal("accepted an invalid fixed buffer length")
	}
}

// TestProtobufHeaderReuse replaces optional fields rather than merging messages.
func TestProtobufHeaderReuse(t *testing.T) {
	header := NewMsgGCHdrProtoBuf()
	header.Msg = 4004
	jobID := uint64(1<<60 + 42)
	header.Proto.JobidSource = &jobID
	var wire bytes.Buffer
	if err := header.Serialize(&wire); err != nil {
		t.Fatal(err)
	}

	var decoded MsgGCHdrProtoBuf
	if err := decoded.Deserialize(&wire); err != nil {
		t.Fatal(err)
	}
	if decoded.Msg != header.Msg || decoded.Proto.GetJobidSource() != header.Proto.GetJobidSource() {
		t.Fatal("protobuf framing lost the message or job identity")
	}

	header.Proto.Reset()
	if err := header.Serialize(&wire); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Deserialize(&wire); err != nil {
		t.Fatal(err)
	}
	if decoded.Proto.JobidSource != nil {
		t.Fatal("reused header retained the previous job")
	}
}
