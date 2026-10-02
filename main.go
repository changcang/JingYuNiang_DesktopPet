//go:build windows

// WhalePet — 鲸鱼娘桌宠 × 内存清理 (mem reduct)
//
// A desktop pet with mem-reduct-style memory cleaning, written in pure Go
// (standard library only, no CGO). The pet window is a transparent
// always-on-top layered window; all drawing is done through GDI+.
package main

import (
	_ "embed"
	"os"
	"runtime"
	"time"
	"unsafe"
)

//go:embed DSniang1.png
var petPNG []byte

func main() {
	runtime.LockOSThread()

	skipMutex := false
	for _, arg := range os.Args[1:] {
		if arg == "--elevated" {
			skipMutex = true
		}
	}

	// --- single instance guard ---------------------------------------------
	if !skipMutex {
		classNameP := mustW("WhalePetMainWnd")
		if classNameP != nil {
			hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(classNameP)), 0)
			if hwnd != 0 {
				text := mustW("WhalePet 已经在运行啦~")
				title := mustW("WhalePet")
				procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)),
					uintptr(unsafe.Pointer(title)), 0)
				return
			}
		}
	}

	// best-effort DPI awareness (crisper window placement on HiDPI)
	procSetProcessDPIAware.Call()

	// --- elevation ----------------------------------------------------------
	// mem-reduct style cleaning requires admin privileges
	// (SeProfileSingleProcessPrivilege / SeIncreaseQuotaPrivilege), so on
	// first launch we restart elevated. If the user declines UAC we keep
	// running in "limited" mode where cleaning is disabled.
	elevated := isElevated()
	if !elevated && tryElevate() {
		return // elevated copy is starting; exit this instance
	}

	// --- GDI+ ---------------------------------------------------------------
	token := gdiplusStartup()
	if token == 0 {
		fatal("GDI+ 初始化失败")
	}

	// --- app ----------------------------------------------------------------
	a := &App{
		elevated:      elevated,
		autoThreshold: 90,
		deepMode:      false,
		config:        loadConfig(),
		animStart:     time.Now(),
		lastClean:     time.Now(),
		nextChat:      time.Now().Add(12 * time.Second),
		cleanDone:     make(chan cleanResult, 1),
		gpToken:       token,
	}
	if a.config.Auto >= 0 {
		a.autoThreshold = a.config.Auto
	}
	a.deepMode = a.config.Deep
	gApp = a
	defer a.cleanup()

	// enable the privileges memreduct relies on (no-op when not elevated)
	enableCleanerPrivileges()

	a.sayMsg("点我清理内存哦~ (拖动可移动)", 5*time.Second)

	if err := a.run(); err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	text := mustW("WhalePet: " + msg)
	title := mustW("WhalePet")
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)), 0)
	os.Exit(1)
}
