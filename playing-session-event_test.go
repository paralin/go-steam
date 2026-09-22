package steam

import (
	"bytes"
	"testing"

	"github.com/paralin/go-steam/protocol"
	"github.com/paralin/go-steam/protocol/protobuf"
	"github.com/paralin/go-steam/protocol/steamlang"
)

// TestPlayingSessionStateEvents preserves ordered playback revocation and release.
func TestPlayingSessionStateEvents(t *testing.T) {
	client := NewClient()
	appID := uint32(1422450)

	// Decode the wire observations through the registered authentication handler.
	for _, blocked := range []bool{true, false} {
		body := &protobuf.CMsgClientPlayingSessionState{
			PlayingBlocked: &blocked,
			PlayingApp:     &appID,
		}
		var wire bytes.Buffer
		if err := protocol.NewClientMsgProtobuf(steamlang.EMsg_ClientPlayingSessionState, body).Serialize(&wire); err != nil {
			t.Fatal(err)
		}
		packet, err := protocol.NewPacket(wire.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		client.Auth.HandlePacket(packet)

		// The caller receives the server's state without initiating another login.
		event, ok := (<-client.Events()).(*PlayingSessionStateEvent)
		if !ok || event.PlayingBlocked != blocked || event.PlayingApp != appID {
			t.Fatalf("unexpected playback observation: %#v", event)
		}
	}
}
