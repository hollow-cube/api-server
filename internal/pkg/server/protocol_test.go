package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func readPacket(t *testing.T, r *bufio.Reader) []byte {
	t.Helper()
	length, err := binary.ReadUvarint(r)
	if err != nil {
		t.Fatalf("read length: %v", err)
	}
	packet := make([]byte, length)
	if _, err = io.ReadFull(r, packet); err != nil {
		t.Fatalf("read packet: %v", err)
	}
	return packet
}

func TestPingProtocolVersion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)

		handshake := readPacket(t, r)
		if handshake[0] != 0x00 || handshake[len(handshake)-1] != 1 {
			t.Errorf("not a status handshake: %x", handshake)
			return
		}
		if request := readPacket(t, r); !bytes.Equal(request, []byte{0x00}) {
			t.Errorf("not a status request: %x", request)
			return
		}

		status := []byte(`{"version":{"name":"26.3","protocol":777},"players":{"max":0,"online":0}}`)
		var body bytes.Buffer
		body.WriteByte(0x00)
		body.Write(binary.AppendUvarint(nil, uint64(len(status))))
		body.Write(status)
		_ = writePacket(conn, body.Bytes())
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	got, err := pingProtocolVersionAt(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	if got != 777 {
		t.Fatalf("got protocol %d, want 777", got)
	}
}
