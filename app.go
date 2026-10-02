//go:build windows

// app.go — WhalePet application: the transparent pet window, tray icon,
// context menus, input handling and the Win32 message loop.
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"syscall"
	"time"
	"unsafe"
)

// ---------------------------------------------------------------------------
// constants
// ---------------------------------------------------------------------------

// window / timer / tray constants
const (
	winW = 320
	winH = 400

	timerAnim = 1
	timerMem  = 2

	wmTrayIcon = 0x8001 // WM_APP + 1
)

// pet animation states
const (
	stateIdle = iota
	stateCleaning
	stateHappy
)

// menu command ids
const (
	menuClean    = 100
	menuAbout    = 101
	menuExit     = 102
	menuElevate  = 103
	menuAutoOff  = 110
	menuAuto70   = 111
	menuAuto80   = 112
	menuAuto90   = 113
	menuModeStd  = 120
	menuModeDeep = 121
)

// pet geometry
const (
	petMaxW        = 280
	petMaxH        = 250
	petBottomMargin = 28
	bubbleTop      = 14.0
)

// App holds the entire application state.
type App struct {
	hwnd     uintptr
	memDC    uintptr
	dib      uintptr
	scanBmp  uintptr // GpBitmap* wrapping the DIB bits
	graphics uintptr // GpGraphics* of scanBmp

	petImg          uintptr
	petSrcW, petSrcH int32
	petDW, petDH     int32 // pet draw size
	petX, petY       int32 // pet draw origin

	gpToken        uintptr
	font           uintptr
	fontFamily     uintptr
	fontCollection uintptr // private GDI+ font collection (embedded 霞鹜文楷)

	state       int
	stateChanged time.Time
	animStart   time.Time
	lastFrame   time.Time

	particles []Particle

	bubbleMsg    string
	bubbleAccent uint32
	bubbleUntil  time.Time

	ramPct   int
	ramAvail uint64
	ramTotal uint64

	elevated      bool
	autoThreshold int
	deepMode      bool
	lastClean     time.Time
	secCount      int
	nextChat      time.Time
	lastTipPct    int

	cleanDone chan cleanResult

	dragging  bool
	dragPt    POINT
	dragMoved int
	winX, winY int

	trayIcon uintptr
	config   Config
}

var gApp *App

// ---------------------------------------------------------------------------
// window procedure
// ---------------------------------------------------------------------------

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	a := gApp
	if a != nil {
		switch msg {
		case WM_TIMER:
			switch wParam {
			case timerAnim:
				a.onTick()
			case timerMem:
				a.onSecond()
			}
		case WM_LBUTTONDOWN:
			a.beginDrag()
		case WM_MOUSEMOVE:
			if a.dragging {
				a.updateDrag()
			}
		case WM_LBUTTONUP:
			if a.dragging {
				a.endDrag()
			}
		case WM_RBUTTONUP:
			a.showMenu()
		case wmTrayIcon:
			switch uint32(lParam) {
			case WM_LBUTTONUP:
				a.requestClean(false)
			case WM_RBUTTONUP:
				a.showMenu()
			}
		case WM_DESTROY:
			a.shutdown()
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// ---------------------------------------------------------------------------
// lifecycle
// ---------------------------------------------------------------------------

func (a *App) run() error {
	className := mustW("WhalePetMainWnd")
	if className == nil {
		return errors.New("utf16 conversion failed")
	}

	wc := WNDCLASSEXW{}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(wndProc)
	wc.hInstance = getModuleHandle()
	wc.hCursor, _, _ = procLoadCursorW.Call(0, IDC_ARROW)
	wc.lpszClassName = className
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// default window position: bottom-right of the primary work area
	wa := workArea()
	a.winX = a.config.X
	a.winY = a.config.Y
	if !a.positionValid(wa) {
		a.winX = int(wa.Right) - winW - 60
		a.winY = int(wa.Bottom) - winH
		if a.winX < int(wa.Left) {
			a.winX = int(wa.Left)
		}
	}

	windowName := mustW("WhalePet")
	// WS_EX_NOACTIVATE keeps the pet from stealing focus on click
	exStyle := uintptr(WS_EX_LAYERED | WS_EX_TOPMOST | WS_EX_TOOLWINDOW | 0x08000000)
	style := uintptr(WS_POPUP)
	hwnd, _, _ := procCreateWindowExW.Call(exStyle,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		style,
		uintptr(uint32(a.winX)), uintptr(uint32(a.winY)),
		winW, winH,
		0, 0, getModuleHandle(), 0)
	if hwnd == 0 {
		return errors.New("CreateWindowExW failed")
	}
	a.hwnd = hwnd

	// --- render target: 32bpp top-down DIB + GDI+ graphics -------------
	memDC, _, _ := procCreateCompatibleDC.Call(0)
	if memDC == 0 {
		return errors.New("CreateCompatibleDC failed")
	}
	a.memDC = memDC

	bmi := BITMAPINFO{}
	bmi.bmiHeader.biSize = uint32(unsafe.Sizeof(bmi.bmiHeader))
	bmi.bmiHeader.biWidth = winW
	bmi.bmiHeader.biHeight = -winH // top-down
	bmi.bmiHeader.biPlanes = 1
	bmi.bmiHeader.biBitCount = 32
	bmi.bmiHeader.biCompression = BI_RGB
	var bits uintptr
	dib, _, _ := procCreateDIBSection.Call(memDC, uintptr(unsafe.Pointer(&bmi)),
		DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == 0 {
		return errors.New("CreateDIBSection failed")
	}
	a.dib = dib
	procSelectObject.Call(memDC, dib)

	a.scanBmp = gpCreateScan0Bitmap(winW, winH, bits)
	if a.scanBmp == 0 {
		return errors.New("GdipCreateBitmapFromScan0 failed")
	}
	a.graphics = gpGraphicsFromImage(a.scanBmp)
	if a.graphics == 0 {
		return errors.New("GdipGetImageGraphicsContext failed")
	}
	gpInitQuality(a.graphics)

	// --- pet image -------------------------------------------------------
	a.petImg = gpLoadBitmapFromBytes(petPNG)
	if a.petImg == 0 {
		return errors.New("failed to decode embedded pet image")
	}
	gpImageEnsureTransparency(a.petImg)
	a.petSrcW, a.petSrcH = gpImageSize(a.petImg)
	a.petDW, a.petDH = fitInto(a.petSrcW, a.petSrcH, petMaxW, petMaxH)
	a.petX = (winW - a.petDW) / 2
	a.petY = winH - petBottomMargin - a.petDH

	// --- font (embedded 霞鹜文楷, fallback to system fonts) --------------------
	a.font, a.fontFamily, a.fontCollection = gpCreateFontFromMemory(wenKaiTTF, 15)
	if a.font == 0 {
		a.font, a.fontFamily = gpCreateFont(15)
	}
	if a.font == 0 {
		return errors.New("font creation failed")
	}

	// --- tray icon ---------------------------------------------------------
	a.trayIcon = gpCreateHICON(a.petImg)
	nid := NOTIFYICONDATAW{}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = a.hwnd
	nid.UID = 1
	nid.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.uCallbackMessage = wmTrayIcon
	nid.hIcon = a.trayIcon
	tip, _ := syscall.UTF16FromString("WhalePet 鲸鱼娘桌宠")
	copy(nid.szTip[:], tip)
	procShellNotifyW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))

	// --- timers, first frame, show ----------------------------------------
	a.onSecond()
	a.drawFrame(time.Now())
	procShowWindow.Call(a.hwnd, SW_SHOWNORMAL)

	procSetTimer.Call(a.hwnd, timerAnim, 33, 0)
	procSetTimer.Call(a.hwnd, timerMem, 1000, 0)

	// --- message loop ------------------------------------------------------
	var m MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || r == ^uintptr(0) {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func (a *App) cleanup() {
	if a.graphics != 0 {
		procGdipDeleteGraphics.Call(a.graphics)
		a.graphics = 0
	}
	if a.scanBmp != 0 {
		procGdipDisposeImage.Call(a.scanBmp)
		a.scanBmp = 0
	}
	if a.petImg != 0 {
		procGdipDisposeImage.Call(a.petImg)
		a.petImg = 0
	}
	if a.font != 0 {
		procGdipDeleteFont.Call(a.font)
		a.font = 0
	}
	if a.fontFamily != 0 {
		procGdipDeleteFontFamily.Call(a.fontFamily)
		a.fontFamily = 0
	}
	if a.fontCollection != 0 {
		procGdipDeletePrivateFontCollection.Call(uintptr(unsafe.Pointer(&a.fontCollection)))
		a.fontCollection = 0
	}
	if a.dib != 0 {
		procDeleteObject.Call(a.dib)
		a.dib = 0
	}
	if a.memDC != 0 {
		procDeleteDC.Call(a.memDC)
		a.memDC = 0
	}
	if a.trayIcon != 0 {
		procDestroyIcon.Call(a.trayIcon)
		a.trayIcon = 0
	}
	if a.gpToken != 0 {
		gdiplusShutdown(a.gpToken)
		a.gpToken = 0
	}
}

func (a *App) shutdown() {
	a.saveUIConfig()
	nid := NOTIFYICONDATAW{}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = a.hwnd
	nid.UID = 1
	procShellNotifyW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
}

func (a *App) positionValid(wa RECT) bool {
	return a.config.X != 0 && a.config.Y != 0 &&
		a.config.X < int(wa.Right) && a.config.Y < int(wa.Bottom) &&
		a.config.X+winW > int(wa.Left)+40 && a.config.Y+winH > int(wa.Top)+40
}

func (a *App) saveUIConfig() {
	a.config.X, a.config.Y = a.winX, a.winY
	a.config.Auto = a.autoThreshold
	a.config.Deep = a.deepMode
	saveConfig(a.config)
}

// ---------------------------------------------------------------------------
// input handling
// ---------------------------------------------------------------------------

func (a *App) beginDrag() {
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	a.dragPt = pt
	a.dragging = true
	a.dragMoved = 0
	procSetCapture.Call(a.hwnd)
}

func (a *App) updateDrag() {
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	dx := int(pt.X - a.dragPt.X)
	dy := int(pt.Y - a.dragPt.Y)
	if dx == 0 && dy == 0 {
		return
	}
	a.dragMoved += absInt(dx) + absInt(dy)
	a.winX += dx
	a.winY += dy

	// keep at least 80px of the window visible inside the work area
	wa := workArea()
	minX := int(wa.Left) - winW + 80
	maxX := int(wa.Right) - 80
	minY := int(wa.Top) - winH + 80
	maxY := int(wa.Bottom) - 80
	if a.winX < minX {
		a.winX = minX
	}
	if a.winX > maxX {
		a.winX = maxX
	}
	if a.winY < minY {
		a.winY = minY
	}
	if a.winY > maxY {
		a.winY = maxY
	}

	a.dragPt = pt
	procMoveWindow.Call(a.hwnd, uintptr(uint32(a.winX)), uintptr(uint32(a.winY)),
		winW, winH, 1)
}

func (a *App) endDrag() {
	a.dragging = false
	procReleaseCapture.Call()
	if a.dragMoved < 6 { // a click, not a drag -> clean!
		a.requestClean(false)
	}
}

// ---------------------------------------------------------------------------
// timers
// ---------------------------------------------------------------------------

func (a *App) onTick() {
	now := time.Now()

	// consume finished clean jobs (sent by the cleaner goroutine)
	select {
	case res := <-a.cleanDone:
		if a.state == stateCleaning {
			switch {
			case res.Freed > 0:
				a.sayMsg(fmt.Sprintf("好耶!释放了 %s 内存~", humanBytes(res.Freed)), 6*time.Second)
			case res.Errs > 0:
				a.sayMsg("呜...清理失败了", 5*time.Second)
			default:
				a.sayMsg("内存已经很干净啦~", 5*time.Second)
			}
			a.state = stateHappy
			a.stateChanged = now
		}
	default:
	}

	switch a.state {
	case stateHappy:
		if now.Sub(a.stateChanged) > 1600*time.Millisecond {
			a.state = stateIdle
		}
	case stateCleaning:
		if now.Sub(a.stateChanged) > 10*time.Second {
			a.state = stateIdle
		}
	}

	a.drawFrame(now)
}

func (a *App) onSecond() {
	load, avail, total := memoryInfo()
	a.ramPct = int(load)
	a.ramAvail = avail
	a.ramTotal = total

	if a.ramPct != a.lastTipPct {
		a.lastTipPct = a.ramPct
		a.updateTrayTip()
	}

	// auto-clean check (30s cooldown, like memreduct's AUTOREDUCT_COOLDOWN)
	a.secCount++
	if a.secCount >= 30 {
		a.secCount = 0
		if a.elevated && a.autoThreshold > 0 && a.ramPct >= a.autoThreshold &&
			time.Since(a.lastClean) > 30*time.Second && a.state == stateIdle {
			a.requestClean(true)
		}
	}

	// idle chatter
	if a.state == stateIdle && time.Now().After(a.nextChat) {
		a.sayMsg(a.pickChatLine(), 5*time.Second)
		a.nextChat = time.Now().Add(time.Duration(18+rand.Intn(22)) * time.Second)
	}
}

func (a *App) updateTrayTip() {
	nid := NOTIFYICONDATAW{}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = a.hwnd
	nid.UID = 1
	nid.uFlags = NIF_TIP
	tip, _ := syscall.UTF16FromString(fmt.Sprintf("WhalePet 鲸鱼娘 · 内存 %d%%", a.ramPct))
	copy(nid.szTip[:], tip)
	procShellNotifyW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

// ---------------------------------------------------------------------------
// actions
// ---------------------------------------------------------------------------

// requestClean starts a memory reduction run. Auto runs use the standard
// mask; manual runs honor the deep-mode toggle.
func (a *App) requestClean(auto bool) {
	if a.state != stateIdle {
		return
	}
	if !a.elevated {
		a.sayMsg("权限不足,右键菜单可重启为管理员~", 5*time.Second)
		return
	}
	a.state = stateCleaning
	a.stateChanged = time.Now()
	a.lastClean = time.Now()
	a.sayMsg("清理中...", 8*time.Second)

	mask := uint32(maskStandard)
	if a.deepMode {
		mask = maskDeep
	}
	go func() {
		start := time.Now()
		res := cleanMemory(mask)
		// keep the cute animation visible for at least 1.4s
		if d := 1400*time.Millisecond - time.Since(start); d > 0 {
			time.Sleep(d)
		}
		select {
		case a.cleanDone <- res:
		default:
		}
	}()
}

func (a *App) sayMsg(text string, d time.Duration) {
	a.bubbleMsg = text
	a.bubbleAccent = colTextNormal
	a.bubbleUntil = time.Now().Add(d)
}

func (a *App) pickChatLine() string {
	if a.autoThreshold > 0 && a.ramPct >= a.autoThreshold {
		lines := []string{
			"内存好满呀,点我清理!",
			"快帮帮我,要清理内存啦~",
		}
		return lines[rand.Intn(len(lines))]
	}
	lines := []string{
		"呜噜噜~",
		"内存状态良好哦~",
		"要一起清理内存吗?",
		"鲸鱼娘帮你守护内存!",
		"左键点我可以清理内存哦~",
	}
	return lines[rand.Intn(len(lines))]
}

func (a *App) showAbout() {
	text := mustW("WhalePet v1.0.0\r\n" +
		"鲸鱼娘内存清理桌宠\r\n\r\n" +
		"清理引擎移植自 mem reduct\r\n\r\n" +
		"左键点击宠物: 立即清理内存\r\n" +
		"拖动宠物: 移动位置\r\n" +
		"右键宠物/托盘图标: 更多设置")
	title := mustW("关于 WhalePet")
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0)
}

// ---------------------------------------------------------------------------
// tray menu
// ---------------------------------------------------------------------------

func appendMenuItem(hMenu uintptr, flags uintptr, id uintptr, text string) {
	var p uintptr
	if text != "" {
		p = uintptr(unsafe.Pointer(mustW(text)))
	}
	procAppendMenuW.Call(hMenu, flags, id, p)
}

func checkItem(hMenu, id uintptr, checked bool) {
	flags := uintptr(0) // MF_BYCOMMAND
	if checked {
		flags |= MF_CHECKED
	}
	procCheckMenuItem.Call(hMenu, flags, id)
}

func (a *App) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}

	appendMenuItem(menu, MF_STRING, menuClean, "立即清理 (&C)")
	appendMenuItem(menu, MF_SEPARATOR, 0, "")

	autoMenu, _, _ := procCreatePopupMenu.Call()
	appendMenuItem(autoMenu, MF_STRING, menuAutoOff, "关闭")
	appendMenuItem(autoMenu, MF_STRING, menuAuto70, "使用率 ≥ 70% 时")
	appendMenuItem(autoMenu, MF_STRING, menuAuto80, "使用率 ≥ 80% 时")
	appendMenuItem(autoMenu, MF_STRING, menuAuto90, "使用率 ≥ 90% 时")
	checkItem(autoMenu, menuAutoOff, a.autoThreshold == 0)
	checkItem(autoMenu, menuAuto70, a.autoThreshold == 70)
	checkItem(autoMenu, menuAuto80, a.autoThreshold == 80)
	checkItem(autoMenu, menuAuto90, a.autoThreshold == 90)
	appendMenuItem(menu, MF_POPUP, autoMenu, "自动清理 (&A)")

	modeMenu, _, _ := procCreatePopupMenu.Call()
	appendMenuItem(modeMenu, MF_STRING, menuModeStd, "标准清理 (推荐)")
	appendMenuItem(modeMenu, MF_STRING, menuModeDeep, "深度清理 (清空待机列表)")
	checkItem(modeMenu, menuModeStd, !a.deepMode)
	checkItem(modeMenu, menuModeDeep, a.deepMode)
	appendMenuItem(menu, MF_POPUP, modeMenu, "清理方式 (&M)")

	if !a.elevated {
		appendMenuItem(menu, MF_STRING, menuElevate, "以管理员身份重启 (&R)")
		appendMenuItem(menu, MF_SEPARATOR, 0, "")
	}
	appendMenuItem(menu, MF_STRING, menuAbout, "关于 WhalePet (&B)")
	appendMenuItem(menu, MF_STRING, menuExit, "退出 (&X)")

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(a.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu,
		TPM_RIGHTBUTTON|TPM_RETURNCMD|TPM_NONOTIFY,
		uintptr(uint32(pt.X)), uintptr(uint32(pt.Y)), 0, a.hwnd)
	procDestroyMenu.Call(modeMenu)
	procDestroyMenu.Call(autoMenu)
	procDestroyMenu.Call(menu)

	switch cmd {
	case menuClean:
		a.requestClean(false)
	case menuAutoOff:
		a.autoThreshold = 0
		a.saveUIConfig()
		a.sayMsg("已关闭自动清理", 3*time.Second)
	case menuAuto70:
		a.autoThreshold = 70
		a.saveUIConfig()
		a.sayMsg("自动清理: 使用率≥70% 时触发", 3*time.Second)
	case menuAuto80:
		a.autoThreshold = 80
		a.saveUIConfig()
		a.sayMsg("自动清理: 使用率≥80% 时触发", 3*time.Second)
	case menuAuto90:
		a.autoThreshold = 90
		a.saveUIConfig()
		a.sayMsg("自动清理: 使用率≥90% 时触发", 3*time.Second)
	case menuModeStd:
		a.deepMode = false
		a.saveUIConfig()
		a.sayMsg("已切换标准清理", 3*time.Second)
	case menuModeDeep:
		a.deepMode = true
		a.saveUIConfig()
		a.sayMsg("已切换深度清理", 3*time.Second)
	case menuElevate:
		if tryElevate() {
			procDestroyWindow.Call(a.hwnd)
		} else {
			a.sayMsg("重启失败啦...", 3*time.Second)
		}
	case menuAbout:
		a.showAbout()
	case menuExit:
		procDestroyWindow.Call(a.hwnd)
	}
}

// ---------------------------------------------------------------------------
// misc helpers
// ---------------------------------------------------------------------------

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// fitInto scales (w,h) to fit inside (maxW,maxH) preserving the aspect ratio.
func fitInto(w, h, maxW, maxH int32) (int32, int32) {
	if w <= 0 || h <= 0 {
		return maxW, maxH
	}
	s := float64(maxW) / float64(w)
	if sH := float64(maxH) / float64(h); sH < s {
		s = sH
	}
	dw := int32(float64(w) * s)
	dh := int32(float64(h) * s)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	return dw, dh
}
