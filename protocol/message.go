// Package protocol defines the Windows HID proxy wire protocol.
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/fxamacker/cbor/v2"
)

var encMode, _ = cbor.CTAP2EncOptions().EncMode()

const NamedPipePath = "\\\\.\\pipe\\ctaphid"

type Command byte

const (
	CommandEnumerate Command = iota + 1
	CommandStart
	CommandDevicesChanged
)

type Message struct {
	Command Command
	Data    []byte
}

func payloadLength(data []byte) (uint16, error) {
	if len(data) > math.MaxUint16 {
		return 0, fmt.Errorf("protocol payload is %d bytes, maximum is %d", len(data), math.MaxUint16)
	}

	return uint16(len(data)), nil
}

func ParseMessage(reader io.Reader) (Message, error) {
	var cmd [1]byte
	if _, err := io.ReadFull(reader, cmd[:]); err != nil {
		return Message{}, err
	}

	var bLen [2]byte
	if _, err := io.ReadFull(reader, bLen[:]); err != nil {
		return Message{}, err
	}

	length := binary.BigEndian.Uint16(bLen[:])

	bData := make([]byte, length)
	if _, err := io.ReadFull(reader, bData); err != nil {
		return Message{}, err
	}

	return Message{
		Command: Command(cmd[0]),
		Data:    bData,
	}, nil
}

func NewMessage(cmd Command, data any) (Message, error) {
	b := make([]byte, 0)

	var err error
	if data != nil {
		b, err = encMode.Marshal(data)
		if err != nil {
			return Message{}, err
		}
	}

	if _, err := payloadLength(b); err != nil {
		return Message{}, err
	}

	return Message{Command: cmd, Data: b}, nil
}

func (m *Message) WriteTo(w io.Writer) (int64, error) {
	length, err := payloadLength(m.Data)
	if err != nil {
		return 0, err
	}

	var header [3]byte

	header[0] = byte(m.Command)
	binary.BigEndian.PutUint16(header[1:], length)

	var total int64
	// Keep command, length, and payload as separate named-pipe messages.
	for _, part := range [][]byte{header[:1], header[1:], m.Data} {
		n, err := w.Write(part)

		total += int64(n)
		if err != nil {
			return total, err
		}

		if n != len(part) {
			return total, io.ErrShortWrite
		}
	}

	return total, nil
}
