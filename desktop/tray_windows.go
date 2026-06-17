//go:build windows

package main

import (
	"context"
	goruntime "runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Win32 constants for Shell_NotifyIconW and tray message handling.
const (
	nimAdd    = 0x00000000
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	wmUser     = 0x0400
	wmNull     = 0x0000
	wmTrayIcon = wmUser + 1
	wmTrayStop = wmUser + 2
	wmCommand  = 0x0111
	wmDestroy  = 0x0002
	wmClose    = 0x0010

	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205

	idmOpen = 1001
	idmQuit = 1002

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0020
	tpmReturnCmd   = 0x0100
	mfString       = 0x00000000
	imageIcon      = 1
	lrDefaultSize  = 0x00000040
	lrShared       = 0x00008000
	hwndMessage    = ^uintptr(2) // HWND_MESSAGE = (HWND)-3
	csHRedraw      = 0x0002
	csVRedraw      = 0x0001
	wmQuit         = 0x0012
)

// NOTIFYICONDATAW is the Win32 structure for tray icon management.
type notifyIconDataW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
}

// WNDCLASSEXW for registering the hidden tray message window.
type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  uintptr
	LpszClassName *uint16
	HIconSm       uintptr
}

// MSG structure for GetMessageW.
type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type point struct {
	X, Y int32
}

// DLL and proc references.
var (
	shell32TrayDLL           = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIconW     = shell32TrayDLL.NewProc("Shell_NotifyIconW")
	procCreateWindowExW      = user32DLL.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32DLL.NewProc("DefWindowProcW")
	procRegisterClassExW     = user32DLL.NewProc("RegisterClassExW")
	procGetMessageW          = user32DLL.NewProc("GetMessageW")
	procTranslateMessage     = user32DLL.NewProc("TranslateMessage")
	procDispatchMessageW     = user32DLL.NewProc("DispatchMessageW")
	procPostMessageW         = user32DLL.NewProc("PostMessageW")
	procPostThreadMessageW   = user32DLL.NewProc("PostThreadMessageW")
	procCreatePopupMenu      = user32DLL.NewProc("CreatePopupMenu")
	procAppendMenuW          = user32DLL.NewProc("AppendMenuW")
	procTrackPopupMenu       = user32DLL.NewProc("TrackPopupMenu")
	procDestroyMenu          = user32DLL.NewProc("DestroyMenu")
	procSetForegroundWindowT = user32DLL.NewProc("SetForegroundWindow")
	procGetCursorPos         = user32DLL.NewProc("GetCursorPos")
	procPostQuitMessage      = user32DLL.NewProc("PostQuitMessage")
	procLoadImageW           = user32DLL.NewProc("LoadImageW")
	procGetCurrentThreadId   = kernel32DLL.NewProc("GetCurrentThreadId")
)

// Package-level state for the tray.
var (
	trayCtx               context.Context
	trayHWnd              uintptr
	trayThreadID          uint32
	trayNID               notifyIconDataW
	trayIconAdded         bool
	trayShutdownRequested bool
	trayStateMu           sync.Mutex
)

// initTray creates the system tray icon and starts the message loop.
// Must be called from app.startup after the Wails context is available.
func initTray(ctx context.Context) {
	trayStateMu.Lock()
	trayCtx = ctx
	trayShutdownRequested = false
	trayStateMu.Unlock()
	go runTrayLoop(isBackgroundLaunch())
}

// destroyTray removes the tray icon. Called from app.shutdown.
func destroyTray() {
	trayStateMu.Lock()
	hwnd := trayHWnd
	threadID := trayThreadID
	trayShutdownRequested = true
	trayStateMu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmTrayStop, 0, 0)
		return
	}
	if threadID != 0 {
		procPostThreadMessageW.Call(uintptr(threadID), wmQuit, 0, 0)
	}
}

func runTrayLoop(backgroundLaunch bool) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	threadID, _, _ := procGetCurrentThreadId.Call()
	trayStateMu.Lock()
	trayThreadID = uint32(threadID)
	trayStateMu.Unlock()
	defer func() {
		trayStateMu.Lock()
		trayThreadID = 0
		trayHWnd = 0
		trayIconAdded = false
		trayStateMu.Unlock()
	}()

	hInstance, _, _ := procGetModuleHandle.Call(0)

	className, _ := syscall.UTF16PtrFromString("ThemistoTrayMsgWnd")
	wc := wndClassExW{
		Style:         csHRedraw | csVRedraw,
		LpfnWndProc:   syscall.NewCallback(trayWndProc),
		HInstance:     hInstance,
		LpszClassName: className,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0, // no title
		0, // style
		0, 0, 0, 0,
		hwndMessage, // message-only window
		0,
		hInstance,
		0,
	)
	if hwnd == 0 {
		if backgroundLaunch && !isTrayShutdownRequested() {
			fallbackShowWindow()
		}
		return
	}
	trayStateMu.Lock()
	trayHWnd = hwnd
	trayStateMu.Unlock()

	icon := loadTrayIcon(hInstance)

	nid := notifyIconDataW{
		HWnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayIcon,
		HIcon:            icon,
	}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	copy(nid.SzTip[:], syscall.StringToUTF16("Themisto"))

	trayStateMu.Lock()
	trayNID = nid
	trayStateMu.Unlock()

	if addTrayIconWithRetry(&nid) {
		trayStateMu.Lock()
		trayIconAdded = true
		trayStateMu.Unlock()
	} else if backgroundLaunch && !isTrayShutdownRequested() {
		fallbackShowWindow()
	}

	// Message loop.
	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&m)),
			0, 0, 0,
		)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func isTrayShutdownRequested() bool {
	trayStateMu.Lock()
	defer trayStateMu.Unlock()
	return trayShutdownRequested
}

func addTrayIconWithRetry(nid *notifyIconDataW) bool {
	const (
		maxAttempts   = 8
		retryInterval = 500 * time.Millisecond
	)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if isTrayShutdownRequested() {
			return false
		}
		ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(nid)))
		if ret != 0 {
			if isTrayShutdownRequested() {
				procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(nid)))
				return false
			}
			return true
		}
		time.Sleep(retryInterval)
	}
	return false
}

func trayWndProc(hwnd, umsg, wparam, lparam uintptr) uintptr {
	switch umsg {
	case wmTrayIcon:
		switch lparam {
		case wmLButtonUp:
			showWindow()
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		id := wparam & 0xFFFF
		handleTrayCommand(id)
		return 0
	case wmTrayStop:
		removeTrayIcon()
		procPostQuitMessage.Call(0)
		return 0
	case wmDestroy, wmClose:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, umsg, wparam, lparam)
	return ret
}

func handleTrayCommand(id uintptr) {
	switch id {
	case idmOpen:
		showWindow()
	case idmQuit:
		removeTrayIcon()
		if ctx := getTrayContext(); ctx != nil {
			wailsRuntime.Quit(ctx)
		}
	}
}

func getTrayContext() context.Context {
	trayStateMu.Lock()
	ctx := trayCtx
	trayStateMu.Unlock()
	return ctx
}

func showWindow() {
	ctx := getTrayContext()
	if ctx == nil {
		return
	}
	wailsRuntime.WindowUnminimise(ctx)
	wailsRuntime.WindowShow(ctx)
}

func fallbackShowWindow() {
	go func() {
		time.Sleep(750 * time.Millisecond)
		showWindow()
	}()
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}

	openStr, _ := syscall.UTF16PtrFromString("Open Themisto")
	quitStr, _ := syscall.UTF16PtrFromString("Quit Themisto")

	procAppendMenuW.Call(menu, mfString, idmOpen, uintptr(unsafe.Pointer(openStr)))
	procAppendMenuW.Call(menu, mfString, idmQuit, uintptr(unsafe.Pointer(quitStr)))

	// SetForegroundWindow is required before TrackPopupMenu per MSDN.
	procSetForegroundWindowT.Call(hwnd)

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	cmd, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmLeftAlign|tpmBottomAlign|tpmReturnCmd,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		hwnd,
		0,
	)
	if cmd != 0 {
		handleTrayCommand(cmd)
	}
	procPostMessageW.Call(hwnd, wmNull, 0, 0)

	procDestroyMenu.Call(menu)
}

func removeTrayIcon() {
	trayStateMu.Lock()
	nid := trayNID
	iconAdded := trayIconAdded
	trayIconAdded = false
	trayStateMu.Unlock()
	if iconAdded {
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	}
}

func loadTrayIcon(hInstance uintptr) uintptr {
	// Try loading icon from the exe resource (same resource ID 3 used by window_icon_windows.go).
	icon, _, _ := procLoadImageW.Call(
		hInstance,
		uintptr(appIconResourceID),
		imageIcon,
		0, 0,
		lrDefaultSize|lrShared,
	)
	if icon != 0 {
		return icon
	}
	// Fallback: use LoadIconW with the same approach as window_icon_windows.go.
	icon, _, _ = procLoadIconW.Call(hInstance, uintptr(appIconResourceID))
	return icon
}
