package gamecoordinator

import (
	"bytes"
	"testing"

	"github.com/paralin/go-steam/protocol/protobuf"
)

// TestProtobufPacket preserves GC job correlation and replaces reused message data.
func TestProtobufPacket(t *testing.T) {
	// Serialize a generated lite message through the public GC envelope.
	message := NewGCMsgProtobuf(1422450, 7001, &protobuf.CMsgClientGamesPlayed_GamePlayed{
		GameId: new(uint64(9007199254740993)),
	})
	message.SetTargetJobId(42)
	var wire bytes.Buffer
	if err := message.Serialize(&wire); err != nil {
		t.Fatal(err)
	}

	// Decode the actual envelope before replacing a previously populated response.
	packet, err := NewGCPacket(&protobuf.CMsgGCClient{
		Appid:   new(uint32(1422450)),
		Msgtype: new(uint32(7001 | 0x80000000)),
		Payload: wire.Bytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.TargetJobId != 42 || packet.MsgType != 7001 || !packet.IsProto {
		t.Fatalf("incorrect GC envelope: %+v", packet)
	}
	response := &protobuf.CMsgClientGamesPlayed_GamePlayed{GameExtraInfo: new("previous")}
	packet.ReadProtoMsg(response)
	if response.GetGameId() != 9007199254740993 || response.GameExtraInfo != nil {
		t.Fatalf("response was not replaced: %v", response)
	}
}
