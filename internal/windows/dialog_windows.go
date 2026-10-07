//go:build windows

package windows

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mbOK              = 0x0
	mbYesNo           = 0x4
	mbIconQuestion    = 0x20
	mbIconInformation = 0x40
	mbSetForeground   = 0x10000
	mbTopMost         = 0x40000
	idYes             = 6
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

// InfoBox shows a modal information dialog. It is used for the pairing fingerprint — a value the
// user must read and compare with the phone's screen, which the tray alone cannot present.
func InfoBox(title, text string) {
	messageBox(title, text, mbOK|mbIconInformation|mbSetForeground|mbTopMost)
}

// ConfirmBox shows a modal Yes/No dialog and reports whether the user chose Yes.
func ConfirmBox(title, text string) bool {
	return messageBox(title, text, mbYesNo|mbIconQuestion|mbSetForeground|mbTopMost) == idYes
}

func messageBox(title, text string, flags uintptr) int {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	m, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return 0
	}
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
	return int(r)
}
