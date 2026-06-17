//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	installerWindowClass = "ThemistoInstallerProgressWindow"

	cwUseDefault = ^uintptr(0x7fffffff)

	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsVisible     = 0x10000000
	wsChild       = 0x40000000

	swHide = 0
	swShow = 5

	wmUser = 0x0400

	idcArrow = 32512

	mbOK            = 0x00000000
	mbYesNo         = 0x00000004
	mbIconInfo      = 0x00000040
	mbIconError     = 0x00000010
	mbIconQuestion  = 0x00000020
	mbSetForeground = 0x00010000

	idYes = 6

	installerProgressWindowWidth  = 900
	installerProgressWindowHeight = 470
)

var (
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procUpdateWindow     = user32.NewProc("UpdateWindow")

	activeInstallerProgressWindow *installerProgressUI
)

type installerProgressUI struct {
	enabled      bool
	total        int
	windowHWND   uintptr
	ready        chan error
	done         chan struct{}
	mu           sync.RWMutex
	stepIndex    int
	statusLabel  string
	canClose     bool
}

type installerWndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type installerMsg struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       installerPoint
	LPrivate uint32
}

type installerPoint struct {
	X int32
	Y int32
}

func startInstallerProgressWindow(total int) *installerProgressUI {
	ui := &installerProgressUI{
		total:       total,
		ready:       make(chan error, 1),
		done:        make(chan struct{}),
		stepIndex:   0,
		statusLabel: "Preparing setup...",
	}

	activeInstallerProgressWindow = ui
	go ui.run()
	if err := <-ui.ready; err != nil {
		activeInstallerProgressWindow = nil
		return ui
	}
	ui.enabled = true
	return ui
}

func (ui *installerProgressUI) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(ui.done)

	initInstallerTheme()

	className, _ := syscall.UTF16PtrFromString(installerWindowClass)
	titleText, _ := syscall.UTF16PtrFromString("Themisto Setup")

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wc := installerWndClassEx{
		CbSize:        uint32(unsafe.Sizeof(installerWndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(installerProgressWndProc),
		HInstance:     hInstance,
		HCursor:       cursor,
		HbrBackground: sharedInstallerTheme.panelBrush,
		LpszClassName: className,
	}

	atom, _, regErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 && regErr != syscall.Errno(1410) {
		ui.ready <- fmt.Errorf("register installer window: %v", regErr)
		return
	}

	x, y := centerInstallerWindow(installerProgressWindowWidth, installerProgressWindowHeight)
	hwnd, _, createErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titleText)),
		wsOverlapped|wsCaption|wsSysMenu|wsMinimizeBox|wsVisible,
		uintptr(x),
		uintptr(y),
		installerProgressWindowWidth,
		installerProgressWindowHeight,
		0,
		0,
		hInstance,
		0,
	)
	if hwnd == 0 {
		ui.ready <- fmt.Errorf("create installer window: %v", createErr)
		return
	}

	ui.windowHWND = hwnd
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	ui.ready <- nil

	var msg installerMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (ui *installerProgressUI) SetStep(index int, label string) {
	if ui == nil || !ui.enabled {
		return
	}
	if index < 0 {
		index = 0
	}
	if index > ui.total {
		index = ui.total
	}

	ui.mu.Lock()
	ui.stepIndex = index + 1
	if ui.stepIndex > ui.total {
		ui.stepIndex = ui.total
	}
	ui.statusLabel = label
	ui.mu.Unlock()
	installerInvalidate(ui.windowHWND)
}

func (ui *installerProgressUI) Complete(message string) {
	if ui == nil || !ui.enabled {
		return
	}
	ui.mu.Lock()
	ui.stepIndex = ui.total
	ui.statusLabel = "Setup complete."
	ui.canClose = true
	ui.mu.Unlock()
	installerInvalidate(ui.windowHWND)
	showInstallerMessage(ui.windowHWND, "Themisto Setup", message, mbIconInfo)
	ui.Close()
}

func (ui *installerProgressUI) Fail(message string) {
	if ui == nil || !ui.enabled {
		return
	}
	ui.mu.Lock()
	ui.canClose = true
	ui.statusLabel = "Setup could not finish."
	ui.mu.Unlock()
	installerInvalidate(ui.windowHWND)
	showInstallerMessage(ui.windowHWND, "Themisto Setup", message, mbIconError)
	ui.Close()
}

func (ui *installerProgressUI) Close() {
	if ui == nil || !ui.enabled || ui.windowHWND == 0 {
		return
	}
	procPostMessageW.Call(ui.windowHWND, wmClose, 0, 0)
	<-ui.done
	ui.enabled = false
	activeInstallerProgressWindow = nil
}

func (ui *installerProgressUI) snapshot() (int, string, bool) {
	ui.mu.RLock()
	defer ui.mu.RUnlock()
	return ui.stepIndex, ui.statusLabel, ui.canClose
}

func (ui *installerProgressUI) paint() {
	hdc, ps := installerBeginPaint(ui.windowHWND)
	defer installerEndPaint(ui.windowHWND, &ps)

	stepIndex, statusLabel, _ := ui.snapshot()
	client := installerClientRect(ui.windowHWND)
	leftPane := installerRect{Left: 0, Top: 0, Right: 270, Bottom: client.Bottom}
	rightPane := installerRect{Left: 270, Top: 0, Right: client.Right, Bottom: client.Bottom}

	installerFill(hdc, leftPane, installerColor(17, 27, 22))
	installerFill(hdc, rightPane, installerColor(245, 246, 241))

	installerDrawText(hdc, sharedInstallerTheme.captionFont, installerColor(169, 202, 182), "THEMISTO SETUP", installerRect{Left: 36, Top: 56, Right: 226, Bottom: 88}, dtLeft|dtSingleLine)
	installerDrawText(hdc, sharedInstallerTheme.headlineFont, installerColor(255, 255, 255), "Installing your secure workspace.", installerRect{Left: 36, Top: 96, Right: 226, Bottom: 172}, dtWordBreak)
	installerDrawText(hdc, sharedInstallerTheme.bodyFont, installerColor(191, 209, 198), "We are installing the agent, connecting this device, and preparing the desktop app so the user lands in a working environment.", installerRect{Left: 36, Top: 214, Right: 226, Bottom: 316}, dtWordBreak)

	installerDrawText(hdc, sharedInstallerTheme.captionFont, installerColor(110, 129, 116), "STEP 2 OF 2", installerRect{Left: 330, Top: 72, Right: 830, Bottom: 92}, dtLeft|dtSingleLine)
	installerDrawText(hdc, sharedInstallerTheme.headlineFont, installerColor(26, 37, 31), "Installing Themisto", installerRect{Left: 330, Top: 102, Right: 830, Bottom: 148}, dtLeft|dtSingleLine)
	installerDrawText(hdc, sharedInstallerTheme.bodyFont, installerColor(90, 105, 94), "Please keep this window open while setup finishes. Themisto will launch automatically once installation is complete.", installerRect{Left: 330, Top: 154, Right: 830, Bottom: 214}, dtWordBreak)

	stageCaption := fmt.Sprintf("Current step  %d of %d", max(stepIndex, 1), max(ui.total, 1))
	installerDrawText(hdc, sharedInstallerTheme.labelFont, installerColor(71, 86, 76), stageCaption, installerRect{Left: 330, Top: 246, Right: 830, Bottom: 270}, dtLeft|dtSingleLine)
	installerDrawText(hdc, sharedInstallerTheme.bodyFont, installerColor(44, 56, 47), statusLabel, installerRect{Left: 330, Top: 278, Right: 830, Bottom: 318}, dtWordBreak)

	progressFrame := installerRect{Left: 330, Top: 340, Right: 820, Bottom: 372}
	installerFill(hdc, progressFrame, installerColor(255, 255, 255))
	installerFrame(hdc, progressFrame, installerColor(204, 211, 201))

	usableWidth := progressFrame.Right - progressFrame.Left - 2
	fillWidth := int32(0)
	if ui.total > 0 && stepIndex > 0 {
		fillWidth = int32(float64(stepIndex) / float64(ui.total) * float64(usableWidth))
	}
	if fillWidth > 0 {
		fillRect := installerRect{
			Left:   progressFrame.Left + 1,
			Top:    progressFrame.Top + 1,
			Right:  progressFrame.Left + 1 + fillWidth,
			Bottom: progressFrame.Bottom - 1,
		}
		installerFill(hdc, fillRect, installerColor(25, 83, 54))
	}

	installerDrawText(hdc, sharedInstallerTheme.captionFont, installerColor(112, 124, 116), "When the bar completes, the desktop app will launch automatically.", installerRect{Left: 330, Top: 392, Right: 830, Bottom: 420}, dtLeft|dtWordBreak)
}

func installerProgressWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	ui := activeInstallerProgressWindow
	switch msg {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		if ui != nil {
			ui.paint()
			return 0
		}
	case wmClose:
		if ui != nil {
			_, _, canClose := ui.snapshot()
			if !canClose {
				showInstallerMessage(hwnd, "Themisto Setup", "Setup is still running. Please wait for installation to finish.", mbIconInfo)
				return 0
			}
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func showInstallerMessage(parent uintptr, title, message string, icon uintptr) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBoxW.Call(
		parent,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbOK|mbSetForeground|icon,
	)
}

// showInstallerConfirm shows a Yes/No dialog and returns true if the user clicks Yes.
func showInstallerConfirm(parent uintptr, title, message string) bool {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	ret, _, _ := procMessageBoxW.Call(
		parent,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbYesNo|mbSetForeground|mbIconQuestion,
	)
	return ret == idYes
}

func hideConsoleWindow() {
	console, _, _ := procGetConsoleWindow.Call()
	if console != 0 {
		procShowWindow.Call(console, swHide)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
