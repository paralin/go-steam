package steam

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/paralin/go-steam/netutil"
	"github.com/paralin/go-steam/protocol"
	"github.com/paralin/go-steam/protocol/protobuf"
	"github.com/paralin/go-steam/protocol/steamlang"
)

// stalledConnection keeps a write blocked until its socket is closed.
type stalledConnection struct {
	// writing announces entry into the transport write.
	writing chan struct{}
	// closed interrupts both transport directions.
	closed chan struct{}
}

// Read blocks until the simulated socket closes.
func (c *stalledConnection) Read() (*protocol.Packet, error) {
	<-c.closed
	return nil, io.EOF
}

// Write announces the blocked transport write.
func (c *stalledConnection) Write([]byte) error {
	close(c.writing)
	<-c.closed
	return io.EOF
}

// Close releases the simulated socket once per Client session.
func (c *stalledConnection) Close() error {
	close(c.closed)
	return nil
}

// SetReadTimeout leaves shutdown control with the blocked-write test.
func (c *stalledConnection) SetReadTimeout(time.Duration) error { return nil }

// SetEncryptionKey accepts the test's unencrypted transport.
func (c *stalledConnection) SetEncryptionKey([]byte) {}

// IsEncrypted reports that this test transport does not encrypt.
func (c *stalledConnection) IsEncrypted() bool { return false }

// TestDisconnectUnblocksFullSendQueue prevents shutdown waiting on its own writer.
func TestDisconnectUnblocksFullSendQueue(t *testing.T) {
	// Fill the application queue behind a blocked socket write.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	conn := &stalledConnection{writing: make(chan struct{}), closed: make(chan struct{})}
	client := NewClient()
	client.session = &clientSession{
		ctx: runCtx, cancel: cancelRun, conn: conn,
		writes: make(chan protocol.IMsg, 1), heartbeat: make(chan time.Duration, 1),
		readDone: make(chan struct{}), writeDone: make(chan struct{}), disconnectDone: make(chan struct{}),
	}
	go client.readLoop(client.session)
	go client.writeLoop(client.session)
	message := protocol.NewClientMsgProtobuf(steamlang.EMsg_ClientHeartBeat, &protobuf.CMsgClientHeartBeat{})
	client.Write(message)
	select {
	case <-conn.writing:
	case <-ctx.Done():
		t.Fatal("writer never reached the socket")
	}
	client.Write(message)
	written := make(chan struct{})
	go func() { client.Write(message); close(written) }()

	// Closing the socket must release both queued writes and owned transport loops.
	client.Disconnect()
	select {
	case <-written:
	case <-ctx.Done():
		t.Fatal("send queue prevented shutdown")
	}
	if err := client.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestConnectionCancellationAndReconnect exercises actual TCP lifetime boundaries.
func TestConnectionCancellationAndReconnect(t *testing.T) {
	// Accept two real connections without completing Steam's encryption handshake.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	address := netutil.ParsePortAddr(listener.Addr().String())
	client := NewClient()
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		if err := client.ConnectToBindContext(ctx, address, nil); err != nil {
			cancel()
			t.Fatal(err)
		}
		server, err := listener.Accept()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		client.setHeartbeat(30)
		cancel()
		waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
		if err := client.Wait(waitCtx); err != nil {
			cancelWait()
			_ = server.Close()
			t.Fatal(err)
		}
		cancelWait()
		_ = server.Close()
		if client.Connected() {
			t.Fatal("cancelled connection remained active")
		}
	}
}
