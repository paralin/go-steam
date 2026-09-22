package steam

import (
	"context"
	"time"

	"github.com/paralin/go-steam/protocol"
)

// clientSession is guarded transport state for one Client connection.
type clientSession struct {
	// ctx ends socket, queue and heartbeat work together.
	ctx context.Context
	// cancel ends this connection without touching a later connection.
	cancel context.CancelFunc
	// conn is the socket bound to this session.
	conn connection
	// writes carries immutable outgoing messages.
	writes chan protocol.IMsg
	// heartbeat supplies the server's authenticated heartbeat interval.
	heartbeat chan time.Duration
	// readDone closes after the final packet handler returns.
	readDone chan struct{}
	// writeDone closes after serialization and heartbeat work stop.
	writeDone chan struct{}
	// disconnectDone closes after the disconnect event has been delivered.
	disconnectDone chan struct{}
	// closed is guarded by Client.mutex and makes disconnect notification unique.
	closed bool
	// tempSessionKey belongs to the reader's encryption handshake.
	tempSessionKey []byte
}
