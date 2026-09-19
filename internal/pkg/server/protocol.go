package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	minecraftPort       = 25565
	protocolPingTimeout = 2 * time.Second
	// A status response is a short json document; this only stops a broken peer from making us
	// allocate whatever length it claims.
	maxStatusResponseBytes = 64 * 1024
)

// pingProtocolVersion asks the server at ip what protocol it speaks, with a Minecraft status ping:
// a handshake with the status intent, then a status request. It works against any build, old or
// new, which a dedicated endpoint on the server would not.
func pingProtocolVersion(ctx context.Context, ip string) (int32, error) {
	return pingProtocolVersionAt(ctx, ip, minecraftPort)
}

func pingProtocolVersionAt(ctx context.Context, ip string, port int) (int32, error) {
	ctx, cancel := context.WithTimeout(ctx, protocolPingTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return 0, fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	var handshake bytes.Buffer
	handshake.WriteByte(0x00) // intention
	handshake.Write(binary.AppendUvarint(nil, 0))
	writeString(&handshake, ip)
	_ = binary.Write(&handshake, binary.BigEndian, uint16(port))
	handshake.WriteByte(1) // status
	if err = writePacket(conn, handshake.Bytes()); err != nil {
		return 0, err
	}
	if err = writePacket(conn, []byte{0x00}); err != nil { // status_request
		return 0, err
	}

	reader := bufio.NewReader(conn)
	length, err := binary.ReadUvarint(reader)
	if err != nil {
		return 0, fmt.Errorf("failed to read response length: %w", err)
	} else if length > maxStatusResponseBytes {
		return 0, fmt.Errorf("status response of %d bytes", length)
	}
	packet := make([]byte, length)
	if _, err = io.ReadFull(reader, packet); err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	body := bytes.NewReader(packet)
	if id, err := binary.ReadUvarint(body); err != nil || id != 0x00 {
		return 0, errors.New("not a status_response")
	}
	jsonLength, err := binary.ReadUvarint(body)
	if err != nil || jsonLength > uint64(body.Len()) {
		return 0, errors.New("malformed status_response")
	}
	raw := make([]byte, jsonLength)
	_, _ = body.Read(raw)

	var status struct {
		Version struct {
			Protocol int32 `json:"protocol"`
		} `json:"version"`
	}
	if err = json.Unmarshal(raw, &status); err != nil {
		return 0, fmt.Errorf("failed to decode status: %w", err)
	}
	if status.Version.Protocol <= 0 {
		return 0, fmt.Errorf("status reports protocol %d", status.Version.Protocol)
	}
	return status.Version.Protocol, nil
}

func writeString(buf *bytes.Buffer, s string) {
	buf.Write(binary.AppendUvarint(nil, uint64(len(s))))
	buf.WriteString(s)
}

func writePacket(w io.Writer, packet []byte) error {
	frame := binary.AppendUvarint(nil, uint64(len(packet)))
	if _, err := w.Write(append(frame, packet...)); err != nil {
		return fmt.Errorf("failed to write packet: %w", err)
	}
	return nil
}
