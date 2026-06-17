//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	shell32AppModelDLL                  = syscall.NewLazyDLL("shell32.dll")
	procSetCurrentProcessExplicitAppIDW = shell32AppModelDLL.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

func setCurrentProcessAppUserModelID(appID string) {
	if stringsPtr, err := syscall.UTF16PtrFromString(appID); err == nil {
		_, _, _ = procSetCurrentProcessExplicitAppIDW.Call(uintptr(unsafe.Pointer(stringsPtr)))
	}
}
