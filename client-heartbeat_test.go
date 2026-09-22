package steam

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/paralin/go-steam/netutil"
	"github.com/paralin/go-steam/protocol"
	"github.com/paralin/go-steam/protocol/protobuf"
	"github.com/paralin/go-steam/protocol/steamlang"
)

// TestHeartbeatLiveness keeps responsive idle sessions and closes silent TCP peers.
func TestHeartbeatLiveness(t *testing.T) {
	for _, responsive := range []bool{false, true} {
		name := "silent"
		if responsive {
			name = "responsive"
		}
		t.Run(name, func(t *testing.T) {
			// Exercise the real framing, socket deadlines and client transport loops.
			ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancel()
			listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			client := NewClient()
			if err := client.ConnectToBindContext(ctx, netutil.ParsePortAddr(listener.Addr().String()), nil); err != nil {
				t.Fatal(err)
			}
			defer client.Disconnect()
			peer, err := listener.AcceptTCP()
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			server := &tcpConnection{conn: peer}
			client.setHeartbeat(1)

			// A responsive peer outlives the initial three-interval silence deadline.
			for range 4 {
				packet, err := server.Read()
				if err != nil {
					t.Fatal(err)
				}
				heartbeat := &protobuf.CMsgClientHeartBeat{}
				packet.ReadProtoMsg(heartbeat)
				if packet.EMsg != steamlang.EMsg_ClientHeartBeat || !heartbeat.GetSendReply() {
					t.Fatal("heartbeat did not request a response")
				}
				if !responsive {
					break
				}

				// A complete inbound frame renews the reader's liveness deadline.
				message := protocol.NewClientMsgProtobuf(steamlang.EMsg_ClientHeartBeat, &protobuf.CMsgClientHeartBeat{})
				var encoded bytes.Buffer
				if err := message.Serialize(&encoded); err != nil {
					t.Fatal(err)
				}
				if err := server.Write(encoded.Bytes()); err != nil {
					t.Fatal(err)
				}
			}

			// Silence must close all transport workers without application cancellation.
			if responsive {
				if !client.Connected() {
					t.Fatal("responsive session was disconnected")
				}
				client.Disconnect()
			}
			if err := client.Wait(ctx); err != nil {
				t.Fatal("transport did not finish", err)
			}
			if client.Connected() {
				t.Fatal("silent session remained connected")
			}
		})
	}
}
