//go:generate powershell -Command "go tool cgo -godefs types_windows.go | gofmt | Set-Content -Path ztypes_windows.go -Encoding Ascii"
package devnotify

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modHidsdi                        = windows.NewLazySystemDLL("hid.dll")
	procHidD_GetHidGuid              = modHidsdi.NewProc("HidD_GetHidGuid")
	user32                           = syscall.NewLazyDLL("user32.dll")
	procRegisterDeviceNotificationW  = user32.NewProc("RegisterDeviceNotificationW")
	procUnregisterDeviceNotification = user32.NewProc("UnregisterDeviceNotification")
)

type DeviceNotification uintptr

const (
	_DBT_DEVTYP_DEVICEINTERFACE   = 0x00000005
	_DEVICE_NOTIFY_SERVICE_HANDLE = 0x00000001
)

func RegisterDeviceNotification(hReceiver windows.Handle) (DeviceNotification, error) {
	var hidGUID windows.GUID
	// HidD_GetHidGuid returns void; GetLastError does not describe its result.
	_, _, _ = procHidD_GetHidGuid.Call(uintptr(unsafe.Pointer(&hidGUID)))

	filter := _DEV_BROADCAST_DEVICEINTERFACE_W{
		Size:       uint32(unsafe.Sizeof(_DEV_BROADCAST_DEVICEINTERFACE_W{})),
		Devicetype: _DBT_DEVTYP_DEVICEINTERFACE,
		Classguid:  _GUID(hidGUID),
	}

	r1, _, err := procRegisterDeviceNotificationW.Call(
		uintptr(hReceiver),
		uintptr(unsafe.Pointer(&filter)),
		_DEVICE_NOTIFY_SERVICE_HANDLE,
	)
	if r1 == 0 {
		return 0, err
	}

	return DeviceNotification(r1), nil
}

func UnregisterDeviceNotification(notification DeviceNotification) error {
	r1, _, err := procUnregisterDeviceNotification.Call(uintptr(notification))
	if r1 == 0 {
		return err
	}

	return nil
}
