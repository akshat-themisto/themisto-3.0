//go:build windows

package main

import (
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	installerEnrollmentWindowClass = "ThemistoInstallerEnrollmentPrompt"

	wmCommand     = 0x0111
	wmClose       = 0x0010
	wmDestroy     = 0x0002
	bnClicked     = 0
	wsTabStop     = 0x00010000
	esAutoHScroll = 0x0080

	installerEnrollmentInputID = 1000
	buttonContinueID           = 1001
	buttonSkipID               = 1002

	installerEnrollmentWindowWidth  = 1060
	installerEnrollmentWindowHeight = 620
)

var (
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procSetFocus             = user32.NewProc("SetFocus")

	activeInstallerEnrollmentPrompt *installerEnrollmentUI
)

type installerEnrollmentUI struct {
	windowHWND   uintptr
	inputHWND    uintptr
	continueHWND uintptr
	skipHWND     uintptr
	resultCh     chan installerEnrollmentPromptResult
	done         chan struct{}
	once         sync.Once
	message      string
	helperText   string
	errorState   bool
}

func promptInstallerEnrollmentLink(prefill, message string) installerEnrollmentPromptResult {
	helper := strings.TrimSpace(message)
	if helper == "" {
		helper = "Paste the full enrollment link from the dashboard, or skip and finish inside the desktop app later."
	}

	ui := &installerEnrollmentUI{
		resultCh:   make(chan installerEnrollmentPromptResult, 1),
		done:       make(chan struct{}),
		message:    helper,
		helperText: helper,
	}
	activeInstallerEnrollmentPrompt = ui
	go ui.run(prefill)

	result := <-ui.resultCh
	<-ui.done
	activeInstallerEnrollmentPrompt = nil
	return result
}

func (ui *installerEnrollmentUI) run(prefill string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(ui.done)

	initInstallerTheme()

	className, _ := syscall.UTF16PtrFromString(installerEnrollmentWindowClass)
	titleText, _ := syscall.UTF16PtrFromString("Themisto Setup")

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wc := installerWndClassEx{
		CbSize:        uint32(unsafe.Sizeof(installerWndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(installerEnrollmentWndProc),
		HInstance:     hInstance,
		HCursor:       cursor,
		HbrBackground: sharedInstallerTheme.panelBrush,
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	x, y := centerInstallerWindow(installerEnrollmentWindowWidth, installerEnrollmentWindowHeight)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titleText)),
		wsOverlapped|wsCaption|wsSysMenu|wsVisible,
		uintptr(x),
		uintptr(y),
		installerEnrollmentWindowWidth,
		installerEnrollmentWindowHeight,
		0,
		0,
		hInstance,
		0,
	)
	if hwnd == 0 {
		ui.emit(installerEnrollmentPromptResult{Skipped: true})
		return
	}

	ui.windowHWND = hwnd
	ui.inputHWND = createInstallerEdit(prefill, installerEnrollmentInputID, hwnd, 374, 339, 564, 32)
	ui.continueHWND = createInstallerButton("Continue", buttonContinueID, hwnd, 680, 470, 128, 42, true)
	ui.skipHWND = createInstallerButton("Set up later", buttonSkipID, hwnd, 820, 470, 134, 42, false)

	installerSetFont(ui.inputHWND, sharedInstallerTheme.bodyFont)
	installerSetFont(ui.continueHWND, sharedInstallerTheme.buttonFont)
	installerSetFont(ui.skipHWND, sharedInstallerTheme.buttonFont)
	installerEnable(ui.continueHWND, strings.TrimSpace(prefill) != "")

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetFocus.Call(ui.inputHWND)

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

func (ui *installerEnrollmentUI) emit(result installerEnrollmentPromptResult) {
	ui.once.Do(func() {
		ui.resultCh <- result
	})
}

func (ui *installerEnrollmentUI) setMessage(message string, isError bool) {
	ui.message = message
	ui.errorState = isError
	installerInvalidate(ui.windowHWND)
}

func (ui *installerEnrollmentUI) handleInputChanged() {
	hasValue := strings.TrimSpace(getInstallerEditText(ui.inputHWND)) != ""
	installerEnable(ui.continueHWND, hasValue)
	if ui.errorState {
		ui.setMessage(ui.helperText, false)
	}
}

func (ui *installerEnrollmentUI) handleContinue() {
	link := strings.TrimSpace(getInstallerEditText(ui.inputHWND))
	if link == "" {
		ui.setMessage("Paste the enrollment link, or choose Set up later to finish inside the desktop app.", true)
		return
	}
	ui.emit(installerEnrollmentPromptResult{Link: link})
	procDestroyWindow.Call(ui.windowHWND)
}

func (ui *installerEnrollmentUI) handleSkip() {
	ui.emit(installerEnrollmentPromptResult{Skipped: true})
	procDestroyWindow.Call(ui.windowHWND)
}

func (ui *installerEnrollmentUI) handleCancel() {
	ui.emit(installerEnrollmentPromptResult{Cancelled: true})
	procDestroyWindow.Call(ui.windowHWND)
}

func (ui *installerEnrollmentUI) paint() {
	hdc, ps := installerBeginPaint(ui.windowHWND)
	defer installerEndPaint(ui.windowHWND, &ps)

	client := installerClientRect(ui.windowHWND)
	leftPane := installerRect{Left: 0, Top: 0, Right: 306, Bottom: client.Bottom}
	rightPane := installerRect{Left: 306, Top: 0, Right: client.Right, Bottom: client.Bottom}

	installerFill(hdc, leftPane, installerColor(17, 27, 22))
	installerFill(hdc, rightPane, installerColor(245, 246, 241))

	installerDrawText(hdc, sharedInstallerTheme.captionFont, installerColor(169, 202, 182), "THEMISTO SETUP", installerRect{Left: 40, Top: 64, Right: 260, Bottom: 90}, dtLeft|dtSingleLine)
	installerDrawText(hdc, sharedInstallerTheme.headlineFont, installerColor(255, 255, 255), "Install and connect in one pass.", installerRect{Left: 40, Top: 112, Right: 258, Bottom: 248}, dtWordBreak)
	installerDrawText(hdc, sharedInstallerTheme.bodyFont, installerColor(191, 209, 198), "01  Paste your enrollment link\n02  Let Themisto install everything\n03  Launch straight into the desktop app", installerRect{Left: 40, Top: 312, Right: 262, Bottom: 470}, dtWordBreak)

	installerDrawText(hdc, sharedInstallerTheme.captionFont, installerColor(110, 129, 116), "STEP 1 OF 2", installerRect{Left: 372, Top: 84, Right: 930, Bottom: 110}, dtLeft|dtSingleLine)
	installerDrawText(hdc, sharedInstallerTheme.headlineFont, installerColor(26, 37, 31), "Connect this device", installerRect{Left: 372, Top: 126, Right: 930, Bottom: 172}, dtLeft|dtSingleLine)
	installerDrawText(hdc, sharedInstallerTheme.bodyFont, installerColor(90, 105, 94), "Paste the full enrollment link from the dashboard. We will download the config and finish device enrollment while setup runs.", installerRect{Left: 372, Top: 190, Right: 930, Bottom: 286}, dtWordBreak)
	installerDrawText(hdc, sharedInstallerTheme.labelFont, installerColor(71, 86, 76), "ENROLLMENT LINK", installerRect{Left: 372, Top: 306, Right: 930, Bottom: 328}, dtLeft|dtSingleLine)

	inputFrame := installerRect{Left: 360, Top: 324, Right: 952, Bottom: 386}
	installerFill(hdc, inputFrame, installerColor(255, 255, 255))
	borderColor := installerColor(203, 210, 200)
	if ui.errorState {
		borderColor = installerColor(201, 94, 92)
	}
	installerFrame(hdc, inputFrame, borderColor)

	messageColor := installerColor(94, 106, 95)
	if ui.errorState {
		messageColor = installerColor(171, 68, 66)
	}
	installerDrawText(hdc, sharedInstallerTheme.bodyFont, messageColor, ui.message, installerRect{Left: 372, Top: 414, Right: 930, Bottom: 482}, dtWordBreak)
	installerDrawText(hdc, sharedInstallerTheme.captionFont, installerColor(118, 129, 121), "You can skip this step and finish enrollment later from the desktop app if you need to.", installerRect{Left: 372, Top: 540, Right: 940, Bottom: 572}, dtLeft|dtWordBreak)
}

func installerEnrollmentWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	ui := activeInstallerEnrollmentPrompt
	switch msg {
	case wmCommand:
		if ui == nil {
			break
		}
		id := int(wParam & 0xffff)
		code := int((wParam >> 16) & 0xffff)
		switch {
		case code == bnClicked && id == buttonContinueID:
			ui.handleContinue()
			return 0
		case code == bnClicked && id == buttonSkipID:
			ui.handleSkip()
			return 0
		case code == enChange && id == installerEnrollmentInputID:
			ui.handleInputChanged()
			return 0
		}
	case wmCtlColorEdit:
		if ui != nil && lParam == ui.inputHWND {
			procSetBkColor.Call(wParam, installerColor(255, 255, 255))
			procSetTextColor.Call(wParam, installerColor(31, 41, 34))
			return sharedInstallerTheme.inputBrush
		}
	case wmDrawItem:
		if ui == nil {
			break
		}
		dis := (*installerDrawItemStruct)(unsafe.Pointer(lParam))
		switch dis.CtlID {
		case buttonContinueID:
			return drawInstallerActionButton(dis, "Continue", true)
		case buttonSkipID:
			return drawInstallerActionButton(dis, "Set up later", false)
		}
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		if ui != nil {
			ui.paint()
			return 0
		}
	case wmClose:
		if ui != nil {
			ui.handleCancel()
			return 0
		}
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func createInstallerEdit(value string, id int, parent uintptr, x, y, width, height int32) uintptr {
	classPtr, _ := syscall.UTF16PtrFromString("EDIT")
	valuePtr, _ := syscall.UTF16PtrFromString(value)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(valuePtr)),
		wsChild|wsVisible|wsTabStop|esAutoHScroll,
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		parent,
		uintptr(id),
		0,
		0,
	)
	return hwnd
}

func createInstallerButton(label string, id int, parent uintptr, x, y, width, height int32, _ bool) uintptr {
	classPtr, _ := syscall.UTF16PtrFromString("BUTTON")
	labelPtr, _ := syscall.UTF16PtrFromString(label)

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(labelPtr)),
		wsChild|wsVisible|wsTabStop|bsOwnerDraw,
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		parent,
		uintptr(id),
		0,
		0,
	)
	return hwnd
}

func getInstallerEditText(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
