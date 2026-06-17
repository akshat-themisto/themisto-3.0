//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

const (
	appIconResourceID = 3
	wmSetIcon         = 0x0080
	iconSmall         = 0
	iconBig           = 1
)

var (
	user32DLL           = syscall.NewLazyDLL("user32.dll")
	kernel32DLL         = syscall.NewLazyDLL("kernel32.dll")
	procFindWindowW     = user32DLL.NewProc("FindWindowW")
	procSendMessageW    = user32DLL.NewProc("SendMessageW")
	procLoadIconW       = user32DLL.NewProc("LoadIconW")
	procGetModuleHandle = kernel32DLL.NewProc("GetModuleHandleW")
)

func ensureWindowIcons() {
	go func() {
		for range 20 {
			window := findWindowByClass(themistoWindowClassName)
			icon := loadAppIcon()
			if window != 0 && icon != 0 {
				setWindowIcon(window, iconSmall, icon)
				setWindowIcon(window, iconBig, icon)
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
}

func findWindowByClass(className string) uintptr {
	classPtr, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(classPtr)), 0)
	return hwnd
}

func loadAppIcon() uintptr {
	instance, _, _ := procGetModuleHandle.Call(0)
	icon, _, _ := procLoadIconW.Call(instance, uintptr(appIconResourceID))
	return icon
}

func setWindowIcon(window uintptr, iconType uintptr, icon uintptr) {
	procSendMessageW.Call(window, uintptr(wmSetIcon), iconType, icon)
}
