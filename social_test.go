package steam

import (
	"bytes"
	"testing"

	protobuf "github.com/aperturerobotics/protobuf-go-lite"
	"github.com/paralin/go-steam/protocol"
	steampb "github.com/paralin/go-steam/protocol/protobuf"
	"github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/socialcache"
	"github.com/paralin/go-steam/steamid"
)

func TestHandleClanStatePreservesCachedFields(t *testing.T) {
	client := &Client{events: make(chan interface{}, 2)}
	social := newSocial(client)
	clanID := steamid.SteamId(76561198000000000)
	social.Groups.Add(socialcache.Group{
		SteamId:             clanID,
		Name:                "old-name",
		Avatar:              "abcd",
		MemberTotalCount:    10,
		MemberOnlineCount:   3,
		MemberChattingCount: 2,
		MemberInGameCount:   1,
	})
	social.handleClanState(protoPacket(t, steamlang.EMsg_ClientClanState, &steampb.CMsgClientClanState{
		SteamidClan: uint64Ptr(uint64(clanID)),
		UserCounts: &steampb.CMsgClientClanState_UserCounts{
			Members:  uint32Ptr(20),
			Online:   uint32Ptr(8),
			Chatting: uint32Ptr(5),
			InGame:   uint32Ptr(4),
		},
	}))

	group, err := social.Groups.ById(clanID)
	if err != nil {
		t.Fatal(err)
	}
	if group.Name != "old-name" {
		t.Fatalf("name after counts-only update = %q", group.Name)
	}
	if group.Avatar != "abcd" {
		t.Fatalf("avatar after counts-only update = %q", group.Avatar)
	}
	if group.MemberTotalCount != 20 {
		t.Fatalf("member total = %d", group.MemberTotalCount)
	}
	if group.MemberOnlineCount != 8 {
		t.Fatalf("member online = %d", group.MemberOnlineCount)
	}
	if group.MemberChattingCount != 5 {
		t.Fatalf("member chatting = %d", group.MemberChattingCount)
	}
	if group.MemberInGameCount != 4 {
		t.Fatalf("member in game = %d", group.MemberInGameCount)
	}
}

func protoPacket(t *testing.T, msgType steamlang.EMsg, body protobuf.Message) *protocol.Packet {
	t.Helper()
	msg := protocol.NewClientMsgProtobuf(msgType, body)
	var data bytes.Buffer
	if err := msg.Serialize(&data); err != nil {
		t.Fatal(err)
	}
	packet, err := protocol.NewPacket(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return packet
}
