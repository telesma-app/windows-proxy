package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

type sharedHIDTestDevice struct {
	reads     chan []byte
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newSharedHIDTestDevice() *sharedHIDTestDevice {
	return &sharedHIDTestDevice{
		reads:  make(chan []byte, 1),
		writes: make(chan []byte, 8),
		closed: make(chan struct{}),
	}
}

func (d *sharedHIDTestDevice) Read(ctx context.Context, output []byte) (int, error) {
	select {
	case data := <-d.reads:
		return copy(output, data), nil
	case <-d.closed:
		return 0, io.ErrClosedPipe
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (d *sharedHIDTestDevice) Write(_ context.Context, data []byte) (int, error) {
	copied := append([]byte(nil), data...)
	d.writes <- copied

	return len(data), nil
}

func (d *sharedHIDTestDevice) Close() error {
	d.closeOnce.Do(func() {
		close(d.closed)
	})

	return nil
}

type cancelableWriteHIDTestDevice struct {
	*sharedHIDTestDevice
	writeStarted chan struct{}
}

func (d *cancelableWriteHIDTestDevice) Write(ctx context.Context, _ []byte) (int, error) {
	close(d.writeStarted)
	<-ctx.Done()

	return 0, ctx.Err()
}

func TestSharedDeviceFailureCancelsPendingWrite(t *testing.T) {
	device := &cancelableWriteHIDTestDevice{
		sharedHIDTestDevice: newSharedHIDTestDevice(),
		writeStarted:        make(chan struct{}),
	}
	p := New(WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	p.openDevice = func(string) (hidDevice, error) { return device, nil }
	server, client := net.Pipe()

	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	shared, err := p.acquireDevice("hid-1", 1, server)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		shared.stop()

		select {
		case <-shared.done:
		case <-time.After(time.Second):
			t.Error("HID reader did not stop")
		}
	})

	writeDone := make(chan error, 1)
	go func() { writeDone <- shared.writeReports([][]byte{make([]byte, hidReportSize)}) }()

	select {
	case <-device.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("HID write did not start")
	}

	// An endpoint read failure must cancel writes before waiting for them.
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-writeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("HID write error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HID write was not canceled after endpoint failure")
	}
}

func TestProxySharesDeviceAcrossConcurrentSessions(t *testing.T) {
	const path = "hid-1"

	device := newSharedHIDTestDevice()
	opened := make(chan string, 2)
	p := New()
	p.validate = func(requested string) (string, error) {
		return requested, nil
	}
	p.openDevice = func(openedPath string) (hidDevice, error) {
		opened <- openedPath

		return device, nil
	}

	clients := make([]net.Conn, 0, 2)
	done := make(chan struct{}, 2)

	for range 2 {
		server, client := net.Pipe()
		clients = append(clients, client)

		go func() {
			p.Proxy(server, path)

			done <- struct{}{}
		}()
	}

	waitForSessionCount(t, p, path, 2)

	select {
	case openedPath := <-opened:
		if openedPath != path {
			t.Fatalf("opened path = %q, want %q", openedPath, path)
		}
	case <-time.After(time.Second):
		t.Fatal("shared HID endpoint was not opened")
	}

	select {
	case duplicate := <-opened:
		t.Fatalf("second physical HID endpoint opened for %q", duplicate)
	default:
	}

	response := make([]byte, hidPacketSize)
	copy(response[:4], []byte{1, 2, 3, 4})
	response[4] = 0x90

	type receivedReport struct {
		data []byte
		err  error
	}

	receivedReports := make(chan receivedReport, len(clients))
	for _, client := range clients {
		go func() {
			if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				receivedReports <- receivedReport{err: err}

				return
			}

			received := make([]byte, hidPacketSize)
			_, err := io.ReadFull(client, received)
			_ = client.SetReadDeadline(time.Time{})

			receivedReports <- receivedReport{data: received, err: err}
		}()
	}

	device.reads <- response

	for range clients {
		received := <-receivedReports
		if received.err != nil {
			t.Fatalf("read broadcast HID report: %v", received.err)
		}

		if string(received.data) != string(response) {
			t.Fatalf("broadcast report = %x, want %x", received.data, response)
		}
	}

	writeDone := make(chan error, 2)

	for index, client := range clients {
		request := testHIDRequest(byte(index + 1))
		go func() {
			writeDone <- writeFull(client, request)
		}()
	}

	for range clients {
		if err := <-writeDone; err != nil {
			t.Fatalf("write client request: %v", err)
		}
	}

	written := make([][]byte, 0, 4)

	for range 4 {
		select {
		case report := <-device.writes:
			written = append(written, report)
		case <-time.After(time.Second):
			t.Fatal("shared HID endpoint did not receive all request reports")
		}
	}

	firstCID := written[0][1]
	if written[1][1] != firstCID {
		t.Fatalf("first request reports interleaved: CIDs %d and %d", firstCID, written[1][1])
	}

	secondCID := written[2][1]
	if written[3][1] != secondCID || secondCID == firstCID {
		t.Fatalf(
			"second request reports = CIDs %d and %d after first CID %d",
			secondCID,
			written[3][1],
			firstCID,
		)
	}

	if err := clients[0].Close(); err != nil {
		t.Errorf("close first client: %v", err)
	}

	waitForSessionCount(t, p, path, 1)

	select {
	case <-device.closed:
		t.Fatal("physical HID endpoint closed while one session remained")
	default:
	}

	if err := clients[1].Close(); err != nil {
		t.Errorf("close second client: %v", err)
	}

	for range clients {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("proxy session did not stop after client closed")
		}
	}

	select {
	case <-device.closed:
	case <-time.After(time.Second):
		t.Fatal("physical HID endpoint remained open after last session")
	}
}

func testHIDRequest(cid byte) []byte {
	first := make([]byte, hidReportSize)
	for index := 1; index < 5; index++ {
		first[index] = cid
	}

	first[5] = 0x90
	binary.BigEndian.PutUint16(first[6:8], hidInitialDataSize+1)

	continuation := make([]byte, hidReportSize)
	copy(continuation[1:5], first[1:5])

	return append(first, continuation...)
}

func waitForSessionCount(t *testing.T, p *Proxy, path string, want int) {
	t.Helper()

	deadline := time.Now().Add(time.Second)

	for {
		p.mu.Lock()

		count := 0
		if shared := p.devices[path]; shared != nil {
			count = len(shared.sessions)
		}
		p.mu.Unlock()

		if count == want {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("active sessions = %d, want %d", count, want)
		}

		time.Sleep(time.Millisecond)
	}
}
