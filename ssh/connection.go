// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"fmt"
	"net"
)

// OpenChannelError is returned if the other side rejects an
// OpenChannel request.
type OpenChannelError struct {
	Reason  RejectionReason
	Message string
}

func (e *OpenChannelError) Error() string {
	return fmt.Sprintf("ssh: rejected: %s (%s)", e.Reason, e.Message)
}

// DisconnectReason is an enumeration used when closing connections to describe
// why a disconnect was sent.  See RFC 4253, section 11.1.
type DisconnectReason uint32

const (
	HostNotAllowedToConnect DisconnectReason = 1
	ProtocolError                            = 2
	KeyExchangeFailed                        = 3
	// 4 is reserved for future use.
	MacError                    = 5
	CompressionError            = 6
	ServiceNotAvailable         = 7
	ProtocolVersionNotSupported = 8
	HostKeyNotVerifiable        = 9
	ConnectionLost              = 10
	ByApplication               = 11
	TooManyConnections          = 12
	AuthCancelledByUser         = 13
	NoMoreAuthMethodsAvailable  = 14
	IllegalUserName             = 15
)

// DisconnectError is returned by Conn.Wait if the other end of the connection
// explicitly closes the connection by sending a disconnect message.
type DisconnectError struct {
	Reason  DisconnectReason
	Message string
}

func (d *DisconnectError) Error() string {
	return fmt.Sprintf("ssh: disconnect, reason %d: %s", d.Reason, d.Message)
}

// ConnMetadata holds metadata for the connection.
type ConnMetadata interface {
	// User returns the user ID for this connection.
	User() string

	// SessionID returns the session hash, also denoted by H.
	SessionID() []byte

	// ClientVersion returns the client's version string as hashed
	// into the session ID.
	ClientVersion() []byte

	// ServerVersion returns the server's version string as hashed
	// into the session ID.
	ServerVersion() []byte

	// RemoteAddr returns the remote address for this connection.
	RemoteAddr() net.Addr

	// LocalAddr returns the local address for this connection.
	LocalAddr() net.Addr
}

// RawConn is a low level interface for sending and receiving packets over an
// authenticated SSH connection.  It represents a connection as described in RFC
// 4254.  Most users are likely to want to use the higher level Conn interface,
// which provides help with managing channels and responding to requests.
type RawConn interface {
	ConnMetadata

	// SendMessage sends a single RFC 4254 connection level message.
	SendMessage(msg Message) error

	// ReceiveMessage blocks until it can return a single connection level
	// message.
	ReceiveMessage() (Message, error)

	// Disconnect sends a message to explicitly close the connection.  After
	// this has been called, no further messages can be sent.
	Disconnect(reason DisconnectReason, message string) error

	// Close closes the underlying network connection.
	Close() error
}

type GlobalRequestMsg = globalRequestMsg
type GlobalRequestSuccessMsg = globalRequestSuccessMsg
type GlobalRequestFailureMsg = globalRequestFailureMsg
type ChannelOpenMsg = channelOpenMsg
type ChannelOpenConfirmMsg = channelOpenConfirmMsg
type ChannelOpenFailureMsg = channelOpenFailureMsg
type ChannelDataMsg = channelDataMsg
type ChannelExtendedDataMsg = channelExtendedDataMsg
type ChannelWindowAdjustMsg = windowAdjustMsg
type ChannelEOFMsg = channelEOFMsg
type ChannelRequestMsg = channelRequestMsg
type ChannelRequestSuccessMsg = channelRequestSuccessMsg
type ChannelRequestFailureMsg = channelRequestFailureMsg
type ChannelCloseMsg = channelCloseMsg

type Message interface {
	isMessageMarker()
}

func (_ *GlobalRequestMsg) isMessageMarker()         {}
func (_ *GlobalRequestSuccessMsg) isMessageMarker()  {}
func (_ *GlobalRequestFailureMsg) isMessageMarker()  {}
func (_ *ChannelOpenMsg) isMessageMarker()           {}
func (_ *ChannelOpenConfirmMsg) isMessageMarker()    {}
func (_ *ChannelOpenFailureMsg) isMessageMarker()    {}
func (_ *ChannelDataMsg) isMessageMarker()           {}
func (_ *ChannelExtendedDataMsg) isMessageMarker()   {}
func (_ *ChannelWindowAdjustMsg) isMessageMarker()   {}
func (_ *ChannelEOFMsg) isMessageMarker()            {}
func (_ *ChannelRequestMsg) isMessageMarker()        {}
func (_ *ChannelRequestSuccessMsg) isMessageMarker() {}
func (_ *ChannelRequestFailureMsg) isMessageMarker() {}
func (_ *ChannelCloseMsg) isMessageMarker()          {}

// Conn represents an SSH connection for both server and client roles.
// Conn is the basis for implementing an application layer, such
// as ClientConn, which implements the traditional shell access for
// clients.
type Conn interface {
	ConnMetadata

	// SendRequest sends a global request, and returns the
	// reply. If wantReply is true, it returns the response status
	// and payload. See also RFC 4254, section 4.
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)

	// OpenChannel tries to open an channel. If the request is
	// rejected, it returns *OpenChannelError. On success it returns
	// the SSH Channel and a Go channel for incoming, out-of-band
	// requests. The Go channel must be serviced, or the
	// connection will hang.
	OpenChannel(name string, data []byte) (Channel, <-chan *Request, error)

	// Disconnect sends a message to explicitly close the connection.  After
	// this has been called, no further messages can be sent.
	Disconnect(reason DisconnectReason, message string) error

	// Close closes the underlying network connection
	Close() error

	// Wait blocks until the connection has shut down, and returns the
	// error causing the shutdown.  If the connection has been closed by an
	// explicit disconnect message from the other end, then Wait will return a
	// DisconnectError.
	Wait() error
}

// DiscardRequests consumes and rejects all requests from the
// passed-in channel.
func DiscardRequests(in <-chan *Request) {
	for req := range in {
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}

// A connection represents an incoming connection.
type connection struct {
	transport *handshakeTransport
	sshConn

	// The connection protocol.
	*mux
}

func (c *connection) SendMessage(msg Message) error {
	packet := Marshal(msg)
	return c.transport.writePacket(packet)
}

func (c *connection) ReceiveMessage() (Message, error) {
	packet, err := c.transport.readPacket()
	if err != nil {
		return nil, err
	}
	msg, err := decode(packet)
	if err != nil {
		return nil, err
	}

	if msg, ok := msg.(Message); !ok {
		return nil, fmt.Errorf("Not a valid connection message: %T", msg)
	} else {
		return msg, nil
	}
}

func (c *connection) Disconnect(reason DisconnectReason, message string) error {
	packet := Marshal(&disconnectMsg{Reason: uint32(reason), Message: message})
	return c.transport.writePacket(packet)
}

func (c *connection) Close() error {
	return c.sshConn.conn.Close()
}

// sshconn provides net.Conn metadata, but disallows direct reads and
// writes.
type sshConn struct {
	conn net.Conn

	user          string
	sessionID     []byte
	clientVersion []byte
	serverVersion []byte
}

func dup(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func (c *sshConn) User() string {
	return c.user
}

func (c *sshConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *sshConn) Close() error {
	return c.conn.Close()
}

func (c *sshConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *sshConn) SessionID() []byte {
	return dup(c.sessionID)
}

func (c *sshConn) ClientVersion() []byte {
	return dup(c.clientVersion)
}

func (c *sshConn) ServerVersion() []byte {
	return dup(c.serverVersion)
}
