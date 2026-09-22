package steam

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/paralin/go-steam/cryptoutil"
	"github.com/paralin/go-steam/netutil"
	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"
	"github.com/pkg/errors"
)

// Client communicates with the Steam network.
// Always poll events from the channel returned by Events() or receiving messages will stop.
// All access, unless otherwise noted, should be threadsafe.
//
// A FatalErrorEvent closes the connection. Connect waits for the old transport
// to finish before establishing its replacement.
// Other errors don't have any effect.
type Client struct {
	// sessionId is the authenticated Steam session identifier.
	sessionId atomic.Int32
	// steamId is the authenticated account identity.
	steamId atomic.Uint64
	// currentJobId allocates request identities across transport lifetimes.
	currentJobId atomic.Uint64

	// Auth implements account authentication.
	Auth *Auth
	// Social implements friends and persona operations.
	Social *Social
	// Web implements Steam web authentication.
	Web *Web
	// Notifications implements account notifications.
	Notifications *Notifications
	// Trading implements trade operations.
	Trading *Trading
	// GC dispatches application coordinator traffic.
	GC *GameCoordinator

	// events carries protocol observations to the caller.
	events chan any
	// handlers receives incoming packets in registration order.
	handlers []PacketHandler
	// handlersMutex guards packet-handler registration.
	handlersMutex sync.RWMutex

	// ConnectionTimeout bounds discovery and dialing; zero uses thirty seconds.
	ConnectionTimeout time.Duration

	// mutex guards the active connection and its completion channels.
	mutex sync.RWMutex
	// session owns one transport, its queues, encryption handshake and cancellation.
	session *clientSession
}

// PacketHandler consumes Steam packets on the connection reader goroutine.
type PacketHandler interface {
	HandlePacket(*Packet)
}

// NewClient constructs a disconnected Steam client.
func NewClient() *Client {
	client := &Client{
		events: make(chan any, 30),
	}
	client.Auth = &Auth{client: client}
	client.RegisterPacketHandler(client.Auth)
	client.Social = newSocial(client)
	client.RegisterPacketHandler(client.Social)
	client.Web = &Web{client: client}
	client.RegisterPacketHandler(client.Web)
	client.Notifications = newNotifications(client)
	client.RegisterPacketHandler(client.Notifications)
	client.Trading = &Trading{client: client}
	client.RegisterPacketHandler(client.Trading)
	client.GC = newGC(client)
	client.RegisterPacketHandler(client.GC)
	return client
}

// Events returns the event channel. By convention all events are pointers, except for errors.
// It is never closed.
func (c *Client) Events() <-chan any {
	return c.events
}

// Emit publishes an event to the continuously drained event stream.
func (c *Client) Emit(event any) {
	c.events <- event
}

// Fatalf reports a fatal protocol error and disconnects.
func (c *Client) Fatalf(format string, a ...any) {
	c.Emit(FatalErrorEvent(errors.Errorf(format, a...)))
	c.Disconnect()
}

// Errorf reports a recoverable protocol error.
func (c *Client) Errorf(format string, a ...any) {
	c.Emit(errors.Errorf(format, a...))
}

// RegisterPacketHandler attaches an incoming packet consumer.
func (c *Client) RegisterPacketHandler(handler PacketHandler) {
	c.handlersMutex.Lock()
	defer c.handlersMutex.Unlock()
	c.handlers = append(c.handlers, handler)
}

// GetNextJobId returns the next job ID to use.
func (c *Client) GetNextJobId() JobId {
	return JobId(c.currentJobId.Add(1))
}

// SteamId returns the client's steam ID.
func (c *Client) SteamId() steamid.SteamId {
	return steamid.SteamId(c.steamId.Load())
}

// SessionId returns the session id.
func (c *Client) SessionId() int32 {
	return c.sessionId.Load()
}

// Connected reports whether the current transport remains open.
func (c *Client) Connected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.session != nil && c.session.ctx.Err() == nil
}

// Connect connects through Steam's directory, reporting failure on Events.
func (c *Client) Connect() *netutil.PortAddr {
	server, err := c.ConnectContext(context.Background())
	if err != nil {
		c.Emit(FatalErrorEvent(err))
	}
	return server
}

// ConnectContext connects through Steam's directory within the supplied lifetime.
// Cancellation closes the socket. The caller must continuously consume Events.
// Connect calls on one Client must be serialized by the caller.
func (c *Client) ConnectContext(ctx context.Context) (*netutil.PortAddr, error) {
	// Bound discovery and dialing without shortening the established connection.
	timeout := c.ConnectionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	setupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var server *netutil.PortAddr
	if !steamDirectoryCache.IsInitialized() {
		_ = steamDirectoryCache.InitializeContext(setupCtx)
	}
	if steamDirectoryCache.IsInitialized() {
		server = steamDirectoryCache.GetRandomCM()
	} else {
		server = GetRandomCM()
	}
	if err := c.connectToBind(ctx, setupCtx, server, nil); err != nil {
		return server, err
	}
	return server, nil
}

// ConnectTo connects to an explicit CM, reporting failure on Events.
func (c *Client) ConnectTo(addr *netutil.PortAddr) { c.ConnectToBind(addr, nil) }

// ConnectToBind connects to an explicit CM and optional local address.
func (c *Client) ConnectToBind(addr *netutil.PortAddr, local *net.TCPAddr) {
	if err := c.ConnectToBindContext(context.Background(), addr, local); err != nil {
		c.Emit(FatalErrorEvent(err))
	}
}

// ConnectToBindContext connects to an explicit CM for the supplied lifetime.
func (c *Client) ConnectToBindContext(ctx context.Context, addr *netutil.PortAddr, local *net.TCPAddr) error {
	timeout := c.ConnectionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	setupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.connectToBind(ctx, setupCtx, addr, local)
}

// connectToBind releases the previous transport before installing a new session.
func (c *Client) connectToBind(ctx, setupCtx context.Context, addr *netutil.PortAddr, local *net.TCPAddr) error {
	// Wait for all old packet handlers before replacing their transport identity.
	c.Disconnect()
	if err := c.Wait(setupCtx); err != nil {
		return err
	}
	conn, err := dialTCPContext(setupCtx, local, addr.ToTCPAddr())
	if err != nil {
		return err
	}

	// Bind reader, writer and heartbeat work to this socket's cancellation.
	runCtx, cancel := context.WithCancel(ctx)
	session := &clientSession{ctx: runCtx, cancel: cancel, conn: conn,
		writes: make(chan IMsg, 5), heartbeat: make(chan time.Duration, 1),
		readDone: make(chan struct{}), writeDone: make(chan struct{}), disconnectDone: make(chan struct{})}
	c.mutex.Lock()
	c.session = session
	c.mutex.Unlock()
	context.AfterFunc(runCtx, func() { c.disconnectSession(session) })
	go c.readLoop(session)
	go c.writeLoop(session)
	return nil
}

// Disconnect cancels the current transport and unblocks queued writers.
func (c *Client) Disconnect() {
	c.mutex.RLock()
	session := c.session
	c.mutex.RUnlock()
	if session != nil {
		c.disconnectSession(session)
	}
}

// disconnectSession closes a socket once, independently of subsequent connections.
func (c *Client) disconnectSession(session *clientSession) {
	c.mutex.Lock()
	if session.closed {
		c.mutex.Unlock()
		return
	}
	session.closed = true
	c.mutex.Unlock()
	session.cancel()
	_ = session.conn.Close()
	c.Emit(&DisconnectedEvent{})
	close(session.disconnectDone)
}

// Wait joins the current transport's reader, writer and heartbeat processing.
// The event stream must remain drained until Wait returns.
func (c *Client) Wait(ctx context.Context) error {
	c.mutex.RLock()
	session := c.session
	c.mutex.RUnlock()
	if session == nil {
		return nil
	}
	for _, done := range []<-chan struct{}{session.readDone, session.writeDone, session.disconnectDone} {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Write queues a message until the current transport closes. Disconnected writes
// are ignored. Callers must not modify a message after submitting it.
func (c *Client) Write(msg IMsg) {
	// Attach Steam identity before handing the immutable message to the writer.
	if cm, ok := msg.(IClientMsg); ok {
		cm.SetSessionId(c.SessionId())
		cm.SetSteamId(SteamId(c.SteamId()))
	}
	c.mutex.RLock()
	session := c.session
	c.mutex.RUnlock()
	if session == nil {
		return
	}

	// Never hold the transport lock while waiting for send capacity.
	select {
	case <-session.ctx.Done():
	case session.writes <- msg:
	}
}

// readLoop dispatches packets only within the socket that received them.
func (c *Client) readLoop(session *clientSession) {
	defer close(session.readDone)
	for {
		packet, err := session.conn.Read()
		if err != nil {
			c.failSession(session, errors.Wrap(err, "read Steam connection"))
			return
		}
		if session.ctx.Err() != nil {
			return
		}
		c.handlePacket(packet)
	}
}

// writeLoop owns serialization and heartbeat timing for one socket.
func (c *Client) writeLoop(session *clientSession) {
	defer close(session.writeDone)
	var buffer bytes.Buffer
	var ticker *time.Ticker
	var heartbeat <-chan time.Time
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	for {
		// Heartbeats share the writer lifetime; no timer goroutine survives shutdown.
		var message IMsg
		select {
		case <-session.ctx.Done():
			return
		case interval := <-session.heartbeat:
			if ticker != nil {
				ticker.Stop()
			}
			ticker = time.NewTicker(interval)
			heartbeat = ticker.C
			continue
		case <-heartbeat:
			message = NewClientMsgProtobuf(EMsg_ClientHeartBeat, new(CMsgClientHeartBeat))
			message.(IClientMsg).SetSessionId(c.SessionId())
			message.(IClientMsg).SetSteamId(SteamId(c.SteamId()))
		case message = <-session.writes:
		}

		// Serialize once and discard the buffer before the next message.
		if session.ctx.Err() != nil {
			return
		}
		err := message.Serialize(&buffer)
		if err == nil {
			err = session.conn.Write(buffer.Bytes())
		}
		buffer.Reset()
		if err != nil {
			c.failSession(session, errors.Wrap(err, "write Steam connection"))
			return
		}
	}
}

// setHeartbeat updates the writer's heartbeat interval after authentication.
func (c *Client) setHeartbeat(seconds time.Duration) {
	if seconds <= 0 {
		c.Fatalf("Steam supplied an invalid heartbeat interval")
		return
	}
	c.mutex.RLock()
	session := c.session
	c.mutex.RUnlock()
	if session == nil {
		return
	}
	select {
	case <-session.ctx.Done():
	case session.heartbeat <- seconds * time.Second:
	}
}

// failSession reports transport failure without closing a replacement socket.
func (c *Client) failSession(session *clientSession, err error) {
	if session.ctx.Err() == nil {
		c.Emit(FatalErrorEvent(err))
	}
	c.disconnectSession(session)
}

// handlePacket dispatches transport control and registered protocol handlers.
func (c *Client) handlePacket(packet *Packet) {
	switch packet.EMsg {
	case EMsg_ChannelEncryptRequest:
		c.handleChannelEncryptRequest(packet)
	case EMsg_ChannelEncryptResult:
		c.handleChannelEncryptResult(packet)
	case EMsg_Multi:
		c.handleMulti(packet)
	case EMsg_ClientCMList:
		c.handleClientCMList(packet)
	}

	c.handlersMutex.RLock()
	defer c.handlersMutex.RUnlock()
	for _, handler := range c.handlers {
		handler.HandlePacket(packet)
	}
}

// handleChannelEncryptRequest answers the Steam encryption challenge.
func (c *Client) handleChannelEncryptRequest(packet *Packet) {
	c.mutex.RLock()
	session := c.session
	c.mutex.RUnlock()
	body := NewMsgChannelEncryptRequest()
	packet.ReadMsg(body)

	if body.Universe != EUniverse_Public {
		c.Fatalf("invalid Steam universe %v", body.Universe)
		return
	}

	session.tempSessionKey = make([]byte, 32)
	rand.Read(session.tempSessionKey)
	encryptedKey := cryptoutil.RSAEncrypt(GetPublicKey(EUniverse_Public), session.tempSessionKey)

	payload := new(bytes.Buffer)
	payload.Write(encryptedKey)
	binary.Write(payload, binary.LittleEndian, crc32.ChecksumIEEE(encryptedKey))
	payload.WriteByte(0)
	payload.WriteByte(0)
	payload.WriteByte(0)
	payload.WriteByte(0)

	c.Write(NewMsg(NewMsgChannelEncryptResponse(), payload.Bytes()))
}

// handleChannelEncryptResult installs the negotiated transport key.
func (c *Client) handleChannelEncryptResult(packet *Packet) {
	c.mutex.RLock()
	session := c.session
	c.mutex.RUnlock()
	body := NewMsgChannelEncryptResult()
	packet.ReadMsg(body)

	if body.Result != EResult_OK {
		c.Fatalf("Encryption failed: %v", body.Result)
		return
	}
	session.conn.SetEncryptionKey(session.tempSessionKey)
	session.tempSessionKey = nil

	c.Emit(&ConnectedEvent{})
}

// handleMulti decodes and dispatches a batch of Steam messages.
func (c *Client) handleMulti(packet *Packet) {
	body := new(CMsgMulti)
	packet.ReadProtoMsg(body)

	payload := body.GetMessageBody()

	if body.GetSizeUnzipped() > 0 {
		r, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			c.Errorf("handleMulti: Error while decompressing: %v", err)
			return
		}

		defer r.Close()
		payload, err = io.ReadAll(r)
		if err != nil {
			c.Errorf("handleMulti: Error while decompressing: %v", err)
			return
		}
	}

	pr := bytes.NewReader(payload)
	for pr.Len() > 0 {
		var length uint32
		if err := binary.Read(pr, binary.LittleEndian, &length); err != nil {
			c.Errorf("read multi-message length: %v", err)
			return
		}
		if uint64(length) > uint64(pr.Len()) {
			c.Errorf("multi-message length exceeds the remaining payload")
			return
		}
		packetData := make([]byte, length)
		pr.Read(packetData)
		p, err := NewPacket(packetData)
		if err != nil {
			c.Errorf("Error reading packet in Multi msg %v: %v", packet, err)
			continue
		}
		c.handlePacket(p)
	}
}

// handleClientCMList publishes replacement connection-manager addresses.
func (c *Client) handleClientCMList(packet *Packet) {
	body := new(CMsgClientCMList)
	packet.ReadProtoMsg(body)

	l := make([]*netutil.PortAddr, 0)
	if len(body.GetCmAddresses()) != len(body.GetCmPorts()) {
		c.Errorf("Steam CM address and port counts differ")
		return
	}
	for i, ip := range body.GetCmAddresses() {
		l = append(l, &netutil.PortAddr{
			IP:   readIp(ip),
			Port: uint16(body.GetCmPorts()[i]),
		})
	}

	c.Emit(&ClientCMListEvent{l})
}

// readIp converts a wire IPv4 address to network byte order.
func readIp(ip uint32) net.IP {
	r := make(net.IP, 4)
	r[3] = byte(ip)
	r[2] = byte(ip >> 8)
	r[1] = byte(ip >> 16)
	r[0] = byte(ip >> 24)
	return r
}
