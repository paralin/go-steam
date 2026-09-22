package steam

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"io"
	"net"
	"sync"

	"github.com/paralin/go-steam/cryptoutil"
	. "github.com/paralin/go-steam/protocol"
	"github.com/pkg/errors"
)

// connection provides framed Steam messages over one socket.
type connection interface {
	Read() (*Packet, error)
	Write([]byte) error
	Close() error
	SetEncryptionKey([]byte)
	IsEncrypted() bool
}

// tcpConnectionMagic marks a Steam TCP frame.
const tcpConnectionMagic uint32 = 0x31305456 // "VT01"

// tcpConnection owns frame encoding and negotiated encryption for one socket.
type tcpConnection struct {
	// conn is the connected TCP socket.
	conn *net.TCPConn
	// ciph is the negotiated message cipher.
	ciph cipher.Block
	// cipherMutex guards cipher replacement during the encryption handshake.
	cipherMutex sync.RWMutex
}

// dialTCPContext opens a TCP transport within the caller's dial deadline.
func dialTCPContext(ctx context.Context, laddr, raddr *net.TCPAddr) (*tcpConnection, error) {
	dialer := &net.Dialer{}
	if laddr != nil {
		dialer.LocalAddr = laddr
	}
	conn, err := dialer.DialContext(ctx, "tcp", raddr.String())
	if err != nil {
		return nil, err
	}

	return &tcpConnection{
		conn: conn.(*net.TCPConn),
	}, nil
}

// Read reads and decrypts one complete Steam frame.
func (c *tcpConnection) Read() (*Packet, error) {
	// All packets begin with a packet length
	var packetLen uint32
	err := binary.Read(c.conn, binary.LittleEndian, &packetLen)
	if err != nil {
		return nil, err
	}

	// A magic value follows for validation
	var packetMagic uint32
	err = binary.Read(c.conn, binary.LittleEndian, &packetMagic)
	if err != nil {
		return nil, err
	}
	if packetMagic != tcpConnectionMagic {
		return nil, errors.Errorf("Invalid connection magic! Expected %d, got %d!", tcpConnectionMagic, packetMagic)
	}

	buf := make([]byte, packetLen)
	_, err = io.ReadFull(c.conn, buf)
	if err == io.ErrUnexpectedEOF {
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}

	// Packets after ChannelEncryptResult are encrypted
	c.cipherMutex.RLock()
	if c.ciph != nil {
		buf = cryptoutil.SymmetricDecrypt(c.ciph, buf)
	}
	c.cipherMutex.RUnlock()

	return NewPacket(buf)
}

// Write encrypts and writes one message. This may only be used by one goroutine at a time.
func (c *tcpConnection) Write(message []byte) error {
	c.cipherMutex.RLock()
	if c.ciph != nil {
		message = cryptoutil.SymmetricEncrypt(c.ciph, message)
	}
	c.cipherMutex.RUnlock()

	err := binary.Write(c.conn, binary.LittleEndian, uint32(len(message)))
	if err != nil {
		return err
	}
	err = binary.Write(c.conn, binary.LittleEndian, tcpConnectionMagic)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(message)
	return err
}

// Close releases the socket and interrupts blocked readers and writers.
func (c *tcpConnection) Close() error {
	return c.conn.Close()
}

// SetEncryptionKey installs the thirty-two-byte negotiated AES key.
func (c *tcpConnection) SetEncryptionKey(key []byte) {
	c.cipherMutex.Lock()
	defer c.cipherMutex.Unlock()
	if key == nil {
		c.ciph = nil
		return
	}
	if len(key) != 32 {
		panic("Connection AES key is not 32 bytes long!")
	}

	var err error
	c.ciph, err = aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
}

// IsEncrypted reports whether the encryption handshake has installed a key.
func (c *tcpConnection) IsEncrypted() bool {
	c.cipherMutex.RLock()
	defer c.cipherMutex.RUnlock()
	return c.ciph != nil
}
