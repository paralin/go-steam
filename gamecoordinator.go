package steam

import (
	"bytes"

	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/gamecoordinator"
	. "github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
)

// GameCoordinator routes GC envelopes through its Steam client's connection.
// Packet handlers are registered before the client begins reading packets.
type GameCoordinator struct {
	// client owns the Steam transport and event dispatch.
	client *Client
	// handlers receive packets synchronously in registration order.
	handlers []GCPacketHandler
}

// newGC binds coordinator traffic to one Steam client.
func newGC(client *Client) *GameCoordinator {
	return &GameCoordinator{
		client:   client,
		handlers: make([]GCPacketHandler, 0),
	}
}

// GCPacketHandler consumes coordinator packets on the Steam reader goroutine.
type GCPacketHandler interface {
	// HandleGCPacket consumes or ignores one parsed coordinator envelope.
	HandleGCPacket(*GCPacket)
}

// RegisterPacketHandler adds a receiver before the Steam client starts.
func (g *GameCoordinator) RegisterPacketHandler(handler GCPacketHandler) {
	g.handlers = append(g.handlers, handler)
}

// HandlePacket unwraps coordinator traffic before dispatching it to receivers.
func (g *GameCoordinator) HandlePacket(packet *Packet) {
	// Ignore Steam messages belonging to another subsystem.
	if packet.EMsg != EMsg_ClientFromGC {
		return
	}

	// Parse the GC envelope once for all registered receivers.
	msg := new(CMsgGCClient)
	packet.ReadProtoMsg(msg)

	p, err := NewGCPacket(msg)
	if err != nil {
		g.client.Errorf("Error reading GC message: %v", err)
		return
	}

	// Deliver the same immutable envelope in registration order.
	for _, handler := range g.handlers {
		handler.HandleGCPacket(p)
	}
}

// Write sends a serialized GC message without publishing malformed envelopes.
func (g *GameCoordinator) Write(msg IGCMsg) {
	// Complete message encoding before handing bytes to the Steam transport.
	buf := new(bytes.Buffer)
	if err := msg.Serialize(buf); err != nil {
		g.client.Errorf("encode GC message: %v", err)
		return
	}

	// Mark protobuf framing in the outer Steam envelope.
	msgType := msg.GetMsgType()
	if msg.IsProto() {
		msgType = msgType | 0x80000000 // mask with protoMask
	}

	g.client.Write(NewClientMsgProtobuf(EMsg_ClientToGC, &CMsgGCClient{
		Msgtype: new(msgType),
		Appid:   new(msg.GetAppId()),
		Payload: buf.Bytes(),
	}))
}

// SetGamesPlayed announces the active applications; an empty list clears playing status.
func (g *GameCoordinator) SetGamesPlayed(appIds ...uint64) {
	games := make([]*CMsgClientGamesPlayed_GamePlayed, 0)
	for _, appId := range appIds {
		games = append(games, &CMsgClientGamesPlayed_GamePlayed{
			GameId: new(appId),
		})
	}

	g.client.Write(NewClientMsgProtobuf(EMsg_ClientGamesPlayed, &CMsgClientGamesPlayed{
		GamesPlayed: games,
	}))
}
