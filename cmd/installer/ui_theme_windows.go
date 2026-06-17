//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmPaint        = 0x000F
	wmEraseBkgnd   = 0x0014
	wmDrawItem     = 0x002B
	wmSetFont      = 0x0030
	wmCtlColorEdit = 0x0133
	enChange       = 0x0300

	bsOwnerDraw  = 0x0000000B
	dtTop        = 0x00000000
	dtLeft       = 0x00000000
	dtCenter     = 0x00000001
	dtVCenter    = 0x00000004
	dtSingleLine = 0x00000020
	dtWordBreak  = 0x00000010

	odsSelected = 0x0001
	odsDisabled = 0x0004

	fwNormal   = 400
	fwMedium   = 500
	fwSemiBold = 600
	fwBold     = 700

	bkModeTransparent = 1
	smCxScreen        = 0
	smCyScreen        = 1
	createNoWindow    = 0x08000000
)

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procBeginPaint      = user32.NewProc("BeginPaint")
	procDrawTextW       = user32.NewProc("DrawTextW")
	procEnableWindow    = user32.NewProc("EnableWindow")
	procEndPaint        = user32.NewProc("EndPaint")
	procFillRect        = user32.NewProc("FillRect")
	procFrameRect       = user32.NewProc("FrameRect")
	procGetClientRect   = user32.NewProc("GetClientRect")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procInvalidateRect  = user32.NewProc("InvalidateRect")

	procCreateFontW     = gdi32.NewProc("CreateFontW")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject    = gdi32.NewProc("DeleteObject")
	procSelectObject    = gdi32.NewProc("SelectObject")
	procSetBkColor      = gdi32.NewProc("SetBkColor")
	procSetBkMode       = gdi32.NewProc("SetBkMode")
	procSetTextColor    = gdi32.NewProc("SetTextColor")
)

type installerRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type installerPaintStruct struct {
	HDC         uintptr
	FErase      int32
	RcPaint     installerRect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type installerDrawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	HDC        uintptr
	RcItem     installerRect
	ItemData   uintptr
}

type installerTheme struct {
	once           sync.Once
	titleFont      uintptr
	headlineFont   uintptr
	bodyFont       uintptr
	labelFont      uintptr
	buttonFont     uintptr
	captionFont    uintptr
	darkBrush      uintptr
	panelBrush     uintptr
	inputBrush     uintptr
}

var sharedInstallerTheme installerTheme

func initInstallerTheme() {
	sharedInstallerTheme.once.Do(func() {
		sharedInstallerTheme.titleFont = createInstallerFont(-44, fwBold)
		sharedInstallerTheme.headlineFont = createInstallerFont(-30, fwBold)
		sharedInstallerTheme.bodyFont = createInstallerFont(-19, fwNormal)
		sharedInstallerTheme.labelFont = createInstallerFont(-17, fwSemiBold)
		sharedInstallerTheme.buttonFont = createInstallerFont(-18, fwSemiBold)
		sharedInstallerTheme.captionFont = createInstallerFont(-16, fwMedium)
		sharedInstallerTheme.darkBrush = createInstallerBrush(installerColor(17, 27, 22))
		sharedInstallerTheme.panelBrush = createInstallerBrush(installerColor(245, 246, 241))
		sharedInstallerTheme.inputBrush = createInstallerBrush(installerColor(255, 255, 255))
	})
}

func installerColor(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}

func createInstallerBrush(color uintptr) uintptr {
	brush, _, _ := procCreateSolidBrush.Call(color)
	return brush
}

func createInstallerFont(height int32, weight int32) uintptr {
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(height),
		0,
		0,
		0,
		uintptr(weight),
		0,
		0,
		0,
		1,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(face)),
	)
	return font
}

func installerClientRect(hwnd uintptr) installerRect {
	var rect installerRect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	return rect
}

func centerInstallerWindow(width, height int32) (int32, int32) {
	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)
	x := int32((int(screenW) - int(width)) / 2)
	y := int32((int(screenH) - int(height)) / 2)
	if x < 40 {
		x = 40
	}
	if y < 40 {
		y = 40
	}
	return x, y
}

func installerBeginPaint(hwnd uintptr) (uintptr, installerPaintStruct) {
	var ps installerPaintStruct
	procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	return ps.HDC, ps
}

func installerEndPaint(hwnd uintptr, ps *installerPaintStruct) {
	procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(ps)))
}

func installerFill(hdc uintptr, rect installerRect, color uintptr) {
	brush := createInstallerBrush(color)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), brush)
	procDeleteObject.Call(brush)
}

func installerFrame(hdc uintptr, rect installerRect, color uintptr) {
	brush := createInstallerBrush(color)
	procFrameRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), brush)
	procDeleteObject.Call(brush)
}

func installerDrawText(hdc, font uintptr, color uintptr, text string, rect installerRect, format uint32) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procSetBkMode.Call(hdc, bkModeTransparent)
	procSetTextColor.Call(hdc, color)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(ptr)), ^uintptr(0), uintptr(unsafe.Pointer(&rect)), uintptr(format))
	if oldFont != 0 {
		procSelectObject.Call(hdc, oldFont)
	}
}

func installerInvalidate(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	procInvalidateRect.Call(hwnd, 0, 1)
}

func installerSetFont(hwnd, font uintptr) {
	if hwnd == 0 || font == 0 {
		return
	}
	procSendMessageW.Call(hwnd, wmSetFont, font, 1)
}

func installerEnable(hwnd uintptr, enabled bool) {
	flag := uintptr(0)
	if enabled {
		flag = 1
	}
	procEnableWindow.Call(hwnd, flag)
	installerInvalidate(hwnd)
}

func installerConfigureHiddenCommand(cmd interface{ SetSysProcAttr(*syscall.SysProcAttr) }) {
	cmd.SetSysProcAttr(&syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	})
}

func drawInstallerActionButton(dis *installerDrawItemStruct, label string, primary bool) uintptr {
	fill := installerColor(240, 242, 237)
	border := installerColor(205, 211, 201)
	text := installerColor(34, 43, 36)

	if primary {
		fill = installerColor(25, 83, 54)
		border = installerColor(25, 83, 54)
		text = installerColor(255, 255, 255)
	}
	if dis.ItemState&odsDisabled != 0 {
		fill = installerColor(226, 231, 223)
		border = installerColor(210, 216, 206)
		text = installerColor(141, 150, 139)
	} else if dis.ItemState&odsSelected != 0 {
		if primary {
			fill = installerColor(18, 68, 43)
			border = installerColor(18, 68, 43)
		} else {
			fill = installerColor(232, 237, 229)
			border = installerColor(191, 198, 187)
		}
	}

	installerFill(dis.HDC, dis.RcItem, fill)
	installerFrame(dis.HDC, dis.RcItem, border)
	installerDrawText(dis.HDC, sharedInstallerTheme.buttonFont, text, label, dis.RcItem, dtCenter|dtVCenter|dtSingleLine)
	return 1
}
