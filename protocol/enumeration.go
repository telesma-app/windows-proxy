package protocol

import (
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/telesma-app/hid"
)

// Enumeration is the payload of a device enumeration response.
// CBOR preserves error messages, but not native error types or identity.
type Enumeration []hid.DeviceSnapshot

type enumeratedDevice struct {
	DeviceInfo  *hid.DeviceInfo
	Errors      map[string]string
	MetadataErr *string
}

func (devices Enumeration) MarshalCBOR() ([]byte, error) {
	wire := make([]enumeratedDevice, len(devices))
	for i, device := range devices {
		info := device.DeviceInfo
		messages := make(map[string]string)

		for field, err := range map[string]error{
			"VendorID":   info.VendorID.Err,
			"ProductID":  info.ProductID.Err,
			"MfrStr":     info.MfrStr.Err,
			"ProductStr": info.ProductStr.Err,
			"SerialNbr":  info.SerialNbr.Err,
			"HIDName":    info.HIDName.Err,
			"HIDUnique":  info.HIDUnique.Err,
		} {
			if err != nil {
				messages[field] = err.Error()
			}
		}

		wire[i] = enumeratedDevice{DeviceInfo: info, Errors: messages}
		if device.MetadataErr != nil {
			wire[i].MetadataErr = new(device.MetadataErr.Error())
		}
	}

	return encMode.Marshal(wire)
}

func (devices *Enumeration) UnmarshalCBOR(data []byte) error {
	var wire []enumeratedDevice
	if err := cbor.Unmarshal(data, &wire); err != nil {
		return err
	}

	decoded := make(Enumeration, len(wire))
	for i, device := range wire {
		info := device.DeviceInfo
		if info == nil {
			return fmt.Errorf("enumerated device %d: missing device info", i)
		}

		for field, target := range map[string]*error{
			"VendorID":   &info.VendorID.Err,
			"ProductID":  &info.ProductID.Err,
			"MfrStr":     &info.MfrStr.Err,
			"ProductStr": &info.ProductStr.Err,
			"SerialNbr":  &info.SerialNbr.Err,
			"HIDName":    &info.HIDName.Err,
			"HIDUnique":  &info.HIDUnique.Err,
		} {
			if message, present := device.Errors[field]; present {
				*target = errors.New(message)
			}
		}

		decoded[i] = hid.DeviceSnapshot{DeviceInfo: info}
		if device.MetadataErr != nil {
			decoded[i].MetadataErr = errors.New(*device.MetadataErr)
		}
	}

	*devices = decoded

	return nil
}
