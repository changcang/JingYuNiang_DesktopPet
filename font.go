//go:build windows

// font.go — embedded LXGW WenKai (霞鹜文楷) font for the speech bubble.
// https://github.com/lxgw/LxgwWenKai  (SIL Open Font License 1.1)
package main

import (
	_ "embed"
	"unsafe"
)

//go:embed LXGWWenKai-Regular.ttf
var wenKaiTTF []byte

// gpCreateFontFromMemory builds a GDI+ font from embedded TTF data via a
// private font collection. Returns (font, fontFamily, collection); the
// caller owns all three handles and must dispose them on exit.
func gpCreateFontFromMemory(ttf []byte, sizePx float32) (uintptr, uintptr, uintptr) {
	if len(ttf) < 4 {
		return 0, 0, 0
	}
	var coll uintptr
	if st, _, _ := procGdipNewPrivateFontCollection.Call(uintptr(unsafe.Pointer(&coll))); st != 0 || coll == 0 {
		return 0, 0, 0
	}
	if st, _, _ := procGdipPrivateAddMemoryFont.Call(coll,
		uintptr(unsafe.Pointer(&ttf[0])), uintptr(len(ttf))); st != 0 {
		procGdipDeletePrivateFontCollection.Call(uintptr(unsafe.Pointer(&coll)))
		return 0, 0, 0
	}
	// GDI+ reports the family under its localized name first.
	for _, name := range []string{"霞鹜文楷", "LXGW WenKai"} {
		np := mustW(name)
		if np == nil {
			continue
		}
		var fam uintptr
		st, _, _ := procGdipCreateFontFamilyFromName.Call(uintptr(unsafe.Pointer(np)), coll,
			uintptr(unsafe.Pointer(&fam)))
		if st != 0 || fam == 0 {
			continue
		}
		var font uintptr
		st, _, _ = procGdipCreateFont.Call(fam, f32(sizePx), 0, unitPixel, uintptr(unsafe.Pointer(&font)))
		if st == 0 && font != 0 {
			return font, fam, coll
		}
		procGdipDeleteFontFamily.Call(fam)
	}
	procGdipDeletePrivateFontCollection.Call(uintptr(unsafe.Pointer(&coll)))
	return 0, 0, 0
}
