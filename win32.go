//go:build windows

// win32.go — all raw Win32 API declarations (no external dependencies).
//
// WhalePet: a desktop pet with mem-reduct functionality.
// Window creation, layered-window compositing, tray icon, menus,
// privileges, token helpers and the native memory-cleaner calls.
package main

import (
	"os"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// DLLs
// ---------------------------------------------------------------------------

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modAdvapi32 = syscall.NewLazyDLL("advapi32.dll")
	modNtdll    = syscall.NewLazyDLL("ntdll.dll")
	modOle32    = syscall.NewLazyDLL("ole32.dll")
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// window styles / extended styles
const (
	WS_POPUP         = 0x80000000
	WS_VISIBLE       = 0x10000000
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_LAYERED    = 0x00080000
)

// messages
const (
	WM_DESTROY     = 0x0002
	WM_TIMER       = 0x0113
	WM_MOUSEMOVE   = 0x0200
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	WM_RBUTTONUP   = 0x0205
)

// misc
const (
	SM_CXSCREEN     = 0
	SM_CYSCREEN     = 1
	SPI_GETWORKAREA = 0x30

	ULW_ALPHA      = 2
	BI_RGB         = 0
	DIB_RGB_COLORS = 0
	GMEM_MOVEABLE  = 0x0002
	IDC_ARROW      = 32512
	SW_SHOWNORMAL  = 1
)

// menu
const (
	MF_STRING    = 0x00000000
	MF_POPUP     = 0x00000010
	MF_SEPARATOR = 0x00000800
	MF_CHECKED   = 0x00000008

	TPM_RIGHTBUTTON = 0x00000002
	TPM_NONOTIFY    = 0x00000080
	TPM_RETURNCMD   = 0x00000100
)

// tray icon
const (
	NIM_ADD     = 0
	NIM_MODIFY  = 1
	NIM_DELETE  = 2
	NIF_MESSAGE = 0x01
	NIF_ICON    = 0x02
	NIF_TIP     = 0x04
)

// token / privileges
const (
	TOKEN_QUERY               = 0x0008
	TOKEN_ADJUST_PRIVILEGES   = 0x0020
	TokenElevation            = 20
	SE_PRIVILEGE_ENABLED      = 0x00000002
	ERROR_ALREADY_EXISTS      = 183
)

const (
	invalidHandleValue = ^uintptr(0) // INVALID_HANDLE_VALUE
)

// ---------------------------------------------------------------------------
// Structures (x64 layouts; WhalePet is built for amd64)
// ---------------------------------------------------------------------------

type POINT struct{ X, Y int32 }

type RECT struct{ Left, Top, Right, Bottom int32 }

type MSG struct {
	Hwnd     uintptr
	Message  uint32
	_        uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}

type WNDCLASSEXW struct {
	cbSize, style uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type BLENDFUNCTION struct {
	BlendOp            byte
	BlendFlags         byte
	SourceConstantAlpha byte
	AlphaFormat        byte
}

type BITMAPINFOHEADER struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type RGBQUAD struct{ Blue, Green, Red, Reserved uint8 }

type BITMAPINFO struct {
	bmiHeader BITMAPINFOHEADER
	bmiColors RGBQUAD
}

type MEMORYSTATUSEX struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

type LUID struct {
	LowPart  uint32
	HighPart int32
}

// C layout on x64: LUID is {DWORD, LONG} (align 4), so LUID_AND_ATTRIBUTES is
// 12 bytes and TOKEN_PRIVILEGES places Privileges at offset 4 — NOT 8.
type LUID_AND_ATTRIBUTES struct {
	Luid       LUID
	Attributes uint32
}

type TOKEN_PRIVILEGES struct {
	PrivilegeCount uint32
	Privileges     [1]LUID_AND_ATTRIBUTES
}

// NOTIFYICONDATAW (x64 layout, total 984 bytes)
type NOTIFYICONDATAW struct {
	cbSize           uint32
	_                uint32 // padding
	HWnd             uintptr
	UID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	_                uint32 // padding
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

// ---------------------------------------------------------------------------
// Procedure table
// ---------------------------------------------------------------------------

var (
	// user32
	procRegisterClassExW    = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW     = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW      = modUser32.NewProc("DefWindowProcW")
	procGetMessageW         = modUser32.NewProc("GetMessageW")
	procTranslateMessage    = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW    = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage     = modUser32.NewProc("PostQuitMessage")
	procDestroyWindow       = modUser32.NewProc("DestroyWindow")
	procUpdateLayeredWindow = modUser32.NewProc("UpdateLayeredWindow")
	procLoadCursorW         = modUser32.NewProc("LoadCursorW")
	procSetTimer            = modUser32.NewProc("SetTimer")
	procGetCursorPos        = modUser32.NewProc("GetCursorPos")
	procSetCapture         = modUser32.NewProc("SetCapture")
	procReleaseCapture      = modUser32.NewProc("ReleaseCapture")
	procGetWindowRect       = modUser32.NewProc("GetWindowRect")
	procMoveWindow          = modUser32.NewProc("MoveWindow")
	procShowWindow          = modUser32.NewProc("ShowWindow")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procGetSystemMetrics    = modUser32.NewProc("GetSystemMetrics")
	procSysParamsInfoW      = modUser32.NewProc("SystemParametersInfoW")
	procFindWindowW         = modUser32.NewProc("FindWindowW")
	procMessageBoxW         = modUser32.NewProc("MessageBoxW")
	procCreatePopupMenu     = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW         = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu      = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu         = modUser32.NewProc("DestroyMenu")
	procCheckMenuItem       = modUser32.NewProc("CheckMenuItem")
	procSetProcessDPIAware  = modUser32.NewProc("SetProcessDPIAware")
	procDestroyIcon         = modUser32.NewProc("DestroyIcon")

	// kernel32
	procGetModuleHandleW     = modKernel32.NewProc("GetModuleHandleW")
	procGetCurrentProcess    = modKernel32.NewProc("GetCurrentProcess")
	procCloseHandle          = modKernel32.NewProc("CloseHandle")
	procGlobalMemoryStatusEx = modKernel32.NewProc("GlobalMemoryStatusEx")
	procGlobalAlloc          = modKernel32.NewProc("GlobalAlloc")
	procGlobalLock           = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock         = modKernel32.NewProc("GlobalUnlock")
	procFindFirstVolumeW     = modKernel32.NewProc("FindFirstVolumeW")
	procFindNextVolumeW      = modKernel32.NewProc("FindNextVolumeW")
	procFindVolumeClose      = modKernel32.NewProc("FindVolumeClose")
	procCreateFileW          = modKernel32.NewProc("CreateFileW")
	procFlushFileBuffers     = modKernel32.NewProc("FlushFileBuffers")

	// gdi32
	procCreateCompatibleDC = modGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC          = modGdi32.NewProc("DeleteDC")
	procDeleteObject      = modGdi32.NewProc("DeleteObject")
	procSelectObject      = modGdi32.NewProc("SelectObject")
	procCreateDIBSection  = modGdi32.NewProc("CreateDIBSection")

	// shell32
	procShellNotifyW  = modShell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW = modShell32.NewProc("ShellExecuteW")

	// advapi32
	procOpenProcessToken       = modAdvapi32.NewProc("OpenProcessToken")
	procLookupPrivilegeValueW  = modAdvapi32.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivileges  = modAdvapi32.NewProc("AdjustTokenPrivileges")
	procGetTokenInformation    = modAdvapi32.NewProc("GetTokenInformation")

	// ntdll
	procNtSetSystemInformation = modNtdll.NewProc("NtSetSystemInformation")

	// ole32
	procCreateStreamOnHGlobal = modOle32.NewProc("CreateStreamOnHGlobal")
)

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func mustW(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func getModuleHandle() uintptr {
	r, _, _ := procGetModuleHandleW.Call(0)
	return r
}

// ntdll NtSetSystemInformation — returns NTSTATUS (0 == success).
func ntSetSystemInformation(cls uintptr, info unsafe.Pointer, size uintptr) int32 {
	r, _, _ := procNtSetSystemInformation.Call(cls, uintptr(info), size)
	return int32(r)
}

// isElevated reports whether the current process token is elevated (admin).
func isElevated() bool {
	var token uintptr
	r, _, _ := procOpenProcessToken.Call(getCurrentProcess(), TOKEN_QUERY, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return false
	}
	defer procCloseHandle.Call(token)

	var elev uint32
	var ret uint32
	r, _, _ = procGetTokenInformation.Call(token, TokenElevation,
		uintptr(unsafe.Pointer(&elev)), 4, uintptr(unsafe.Pointer(&ret)))
	return r != 0 && elev != 0
}

func getCurrentProcess() uintptr {
	r, _, _ := procGetCurrentProcess.Call()
	return r
}

// enablePrivilege enables a named privilege on the current process token.
func enablePrivilege(name string) bool {
	var token uintptr
	r, _, _ := procOpenProcessToken.Call(getCurrentProcess(),
		TOKEN_ADJUST_PRIVILEGES|TOKEN_QUERY, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return false
	}
	defer procCloseHandle.Call(token)

	var tp TOKEN_PRIVILEGES
	tp.PrivilegeCount = 1
	tp.Privileges[0].Attributes = SE_PRIVILEGE_ENABLED

	nameP, _ := syscall.UTF16PtrFromString(name)
	r, _, _ = procLookupPrivilegeValueW.Call(0, uintptr(unsafe.Pointer(nameP)),
		uintptr(unsafe.Pointer(&tp.Privileges[0].Luid)))
	if r == 0 {
		return false
	}
	r, _, callErr := procAdjustTokenPrivileges.Call(token, 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
	if r == 0 {
		return false // AdjustTokenPrivileges itself failed
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
		return false // e.g. ERROR_NOT_ALL_ASSIGNED (1300): privilege not held by token
	}
	return true
}

// memoryInfo returns (loadPercent, availPhysBytes, totalPhysBytes).
func memoryInfo() (uint32, uint64, uint64) {
	var ms MEMORYSTATUSEX
	ms.dwLength = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 {
		return 0, 0, 0
	}
	return ms.dwMemoryLoad, ms.ullAvailPhys, ms.ullTotalPhys
}

// workArea returns the primary screen work area (taskbar excluded).
func workArea() RECT {
	var rc RECT
	procSysParamsInfoW.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&rc)), 0)
	return rc
}

// tryElevate relaunches the current executable via the "runas" verb.
// Returns true when the elevated copy was (probably) started.
func tryElevate() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	verb := mustW("runas")
	exeP := mustW(exe)
	args := mustW("--elevated")
	if exeP == nil {
		return false
	}
	r, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(exeP)),
		uintptr(unsafe.Pointer(args)),
		0, SW_SHOWNORMAL)
	return r > 32
}
