package protocol

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/telesma-app/hid"
)

func TestMessageRoundTrip(t *testing.T) {
	wantPath := `\\?\hid#device`
	message, err := NewMessage(CommandStart, wantPath)
	if err != nil {
		t.Fatal(err)
	}

	var wire bytes.Buffer
	if _, err := message.WriteTo(&wire); err != nil {
		t.Fatal(err)
	}

	got, err := ParseMessage(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != CommandStart {
		t.Fatalf("command = %d, want %d", got.Command, CommandStart)
	}

	var gotPath string
	if err := cbor.Unmarshal(got.Data, &gotPath); err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestNewMessageRejectsOversizedPayload(t *testing.T) {
	if _, err := NewMessage(CommandStart, make([]byte, math.MaxUint16)); err == nil {
		t.Fatal("expected oversized payload error")
	}
}

func TestEnumerationMessagePreservesDeviceMetadata(t *testing.T) {
	want := Enumeration{
		{
			DeviceInfo: &hid.DeviceInfo{
				Path:           `\\?\hid#read`,
				VendorID:       hid.Descriptor[uint16]{Value: new(uint16)},
				ProductID:      hid.Descriptor[uint16]{Value: new(uint16(0x0407))},
				MfrStr:         hid.Descriptor[string]{Value: new(string)},
				ProductStr:     hid.Descriptor[string]{Value: new("Security Key")},
				SerialNbr:      hid.Descriptor[string]{Err: errors.New("serial unavailable")},
				ReleaseNbr:     0x0507,
				UsagePage:      0xf1d0,
				Usage:          0x01,
				InterfaceNbr:   2,
				InstanceID:     `HID\VID_1050&PID_0407&MI_02\instance`,
				ParentDeviceID: `USB\VID_1050&PID_0407\parent`,
				HIDUnique:      hid.Descriptor[string]{Value: new("transport-address")},
			},
			MetadataErr: errors.New("topology unavailable"),
		},
		{
			DeviceInfo: &hid.DeviceInfo{
				Path:       `\\?\hid#failed`,
				VendorID:   hid.Descriptor[uint16]{Err: errors.New("invalid vendor ID")},
				ProductID:  hid.Descriptor[uint16]{Err: errors.New("invalid product ID")},
				MfrStr:     hid.Descriptor[string]{Err: errors.New("manufacturer unavailable")},
				ProductStr: hid.Descriptor[string]{Err: errors.New("product unavailable")},
				SerialNbr:  hid.Descriptor[string]{Err: errors.New("serial unavailable")},
				HIDName:    hid.Descriptor[string]{Err: errors.New("HID name unavailable")},
				HIDUnique:  hid.Descriptor[string]{Err: errors.New("HID unique unavailable")},
			},
		},
	}

	message, err := NewMessage(CommandEnumerate, want)
	if err != nil {
		t.Fatal(err)
	}
	var got Enumeration
	if err := cbor.Unmarshal(message.Data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("enumeration = %+v, want %+v", got, want)
	}
}

func TestEnumerationMessageWithNoDevices(t *testing.T) {
	message, err := NewMessage(CommandEnumerate, Enumeration{})
	if err != nil {
		t.Fatal(err)
	}
	var devices Enumeration
	if err := cbor.Unmarshal(message.Data, &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("devices = %v, want no devices", devices)
	}
}

func TestEnumerationRejectsMissingDeviceInfo(t *testing.T) {
	message, err := NewMessage(CommandEnumerate, []*hid.DeviceInfo{{Path: `\\?\hid#old-format`}})
	if err != nil {
		t.Fatal(err)
	}
	var devices Enumeration
	if err := cbor.Unmarshal(message.Data, &devices); err == nil {
		t.Fatal("expected error for a response without DeviceInfo")
	}
}

func TestWriteToRejectsOversizedMutatedPayload(t *testing.T) {
	message, err := NewMessage(CommandStart, nil)
	if err != nil {
		t.Fatal(err)
	}
	message.Data = make([]byte, math.MaxUint16+1)

	var wire bytes.Buffer
	if _, err := message.WriteTo(&wire); err == nil {
		t.Fatal("expected oversized payload error")
	}
	if wire.Len() != 0 {
		t.Fatalf("wrote %d bytes before rejecting payload", wire.Len())
	}
}
