//go:build windows

// gdiplus.go — flat GDI+ API bindings (PNG decode, ARGB surface drawing,
// anti-aliased shapes, world transforms and text).
package main

import (
	"math"
	"syscall"
	"unsafe"
)

var modGdiplus = syscall.NewLazyDLL("gdiplus.dll")

var (
	procGdiplusStartup              = modGdiplus.NewProc("GdiplusStartup")
	procGdiplusShutdown             = modGdiplus.NewProc("GdiplusShutdown")
	procGdipCreateBitmapFromStream  = modGdiplus.NewProc("GdipCreateBitmapFromStream")
	procGdipCreateBitmapFromScan0   = modGdiplus.NewProc("GdipCreateBitmapFromScan0")
	procGdipCreateHICONFromBitmap   = modGdiplus.NewProc("GdipCreateHICONFromBitmap")
	procGdipGetImageGraphicsContext = modGdiplus.NewProc("GdipGetImageGraphicsContext")
	procGdipGetImageWidth           = modGdiplus.NewProc("GdipGetImageWidth")
	procGdipGetImageHeight          = modGdiplus.NewProc("GdipGetImageHeight")
	procGdipDisposeImage            = modGdiplus.NewProc("GdipDisposeImage")
	procGdipDeleteGraphics          = modGdiplus.NewProc("GdipDeleteGraphics")
	procGdipGraphicsClear           = modGdiplus.NewProc("GdipGraphicsClear")
	procGdipSetInterpolationMode    = modGdiplus.NewProc("GdipSetInterpolationMode")
	procGdipSetSmoothingMode        = modGdiplus.NewProc("GdipSetSmoothingMode")
	procGdipSetPixelOffsetMode      = modGdiplus.NewProc("GdipSetPixelOffsetMode")
	procGdipDrawImageRectRectI      = modGdiplus.NewProc("GdipDrawImageRectRectI")
	procGdipResetWorldTransform     = modGdiplus.NewProc("GdipResetWorldTransform")
	procGdipTranslateWorldTransform = modGdiplus.NewProc("GdipTranslateWorldTransform")
	procGdipRotateWorldTransform    = modGdiplus.NewProc("GdipRotateWorldTransform")
	procGdipScaleWorldTransform     = modGdiplus.NewProc("GdipScaleWorldTransform")
	procGdipCreateSolidFill         = modGdiplus.NewProc("GdipCreateSolidFill")
	procGdipDeleteBrush             = modGdiplus.NewProc("GdipDeleteBrush")
	procGdipFillEllipseI            = modGdiplus.NewProc("GdipFillEllipseI")
	procGdipDrawEllipseI            = modGdiplus.NewProc("GdipDrawEllipseI")
	procGdipFillRectangleI          = modGdiplus.NewProc("GdipFillRectangleI")
	procGdipCreatePen1              = modGdiplus.NewProc("GdipCreatePen1")
	procGdipDeletePen               = modGdiplus.NewProc("GdipDeletePen")
	procGdipDrawLineI               = modGdiplus.NewProc("GdipDrawLineI")
	procGdipCreatePath              = modGdiplus.NewProc("GdipCreatePath")
	procGdipDeletePath              = modGdiplus.NewProc("GdipDeletePath")
	procGdipAddPathArcI             = modGdiplus.NewProc("GdipAddPathArcI")
	procGdipCloseFigure             = modGdiplus.NewProc("GdipClosePathFigure")
	procGdipFillPath                = modGdiplus.NewProc("GdipFillPath")
	procGdipDrawPath                = modGdiplus.NewProc("GdipDrawPath")
	procGdipCreateFontFamilyFromName     = modGdiplus.NewProc("GdipCreateFontFamilyFromName")
	procGdipDeleteFontFamily             = modGdiplus.NewProc("GdipDeleteFontFamily")
	procGdipNewPrivateFontCollection     = modGdiplus.NewProc("GdipNewPrivateFontCollection")
	procGdipDeletePrivateFontCollection  = modGdiplus.NewProc("GdipDeletePrivateFontCollection")
	procGdipPrivateAddMemoryFont         = modGdiplus.NewProc("GdipPrivateAddMemoryFont")
	procGdipCreateFont                   = modGdiplus.NewProc("GdipCreateFont")
	procGdipDeleteFont              = modGdiplus.NewProc("GdipDeleteFont")
	procGdipDrawString              = modGdiplus.NewProc("GdipDrawString")
	procGdipMeasureString           = modGdiplus.NewProc("GdipMeasureString")
	procGdipBitmapLockBits          = modGdiplus.NewProc("GdipBitmapLockBits")
	procGdipBitmapUnlockBits        = modGdiplus.NewProc("GdipBitmapUnlockBits")
)

// enum values
const (
	pixFmt32bppARGB  = 0x26200A
	pixFmt32bppPARGB = 0xE200B

	unitPixel                    = 2 // Unit::Pixel
	matrixOrderAppend            = 1
	interpolationHighQualityBicubic = 8
	smoothingModeAntiAlias       = 5
	pixelOffsetModeHalf          = 5
	imageLockModeReadWrite       = 3
)

type gdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

type gpBitmapData struct {
	Width      uint32
	Height     uint32
	Stride     int32
	PixelFormat int32
	Scan0      uintptr
	Reserved   uint32
}

type gpRectF struct{ X, Y, Width, Height float32 }
type gpRectI struct{ X, Y, Width, Height int32 }

// f32 converts a float32 argument into a uintptr bit pattern for Call().
func f32(v float32) uintptr { return uintptr(math.Float32bits(v)) }

// comRelease calls p->Release() on a COM object.
func comRelease(p uintptr) {
	if p == 0 {
		return
	}
	vt := *(*uintptr)(unsafe.Pointer(p))
	if vt == 0 {
		return
	}
	rel := *(*uintptr)(unsafe.Pointer(vt + 2*unsafe.Sizeof(uintptr(0))))
	if rel != 0 {
		syscall.SyscallN(rel, p)
	}
}

func gdiplusStartup() uintptr {
	in := gdiplusStartupInput{GdiplusVersion: 1}
	var token uintptr
	procGdiplusStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&in)), 0)
	return token
}

func gdiplusShutdown(token uintptr) {
	if token != 0 {
		procGdiplusShutdown.Call(token)
	}
}

// gpLoadBitmapFromBytes decodes a PNG (or other GDI+ supported image) from memory.
func gpLoadBitmapFromBytes(data []byte) uintptr {
	if len(data) == 0 {
		return 0
	}
	hg, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(len(data)))
	if hg == 0 {
		return 0
	}
	locked, _, _ := procGlobalLock.Call(hg)
	if locked == 0 {
		return 0
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(locked)), len(data))
	copy(dst, data)
	procGlobalUnlock.Call(hg)

	var stream uintptr
	r, _, _ := procCreateStreamOnHGlobal.Call(hg, 1, uintptr(unsafe.Pointer(&stream)))
	if r != 0 {
		return 0
	}
	defer comRelease(stream)

	var bmp uintptr
	st, _, _ := procGdipCreateBitmapFromStream.Call(stream, uintptr(unsafe.Pointer(&bmp)))
	if st != 0 || bmp == 0 {
		return 0
	}
	return bmp
}

// gpCreateScan0Bitmap wraps an existing 32bpp premultiplied-ARGB pixel buffer.
func gpCreateScan0Bitmap(w, h int32, scan0 uintptr) uintptr {
	var bmp uintptr
	procGdipCreateBitmapFromScan0.Call(uintptr(w), uintptr(h), uintptr(w*4),
		pixFmt32bppPARGB, scan0, uintptr(unsafe.Pointer(&bmp)))
	return bmp
}

func gpGraphicsFromImage(img uintptr) uintptr {
	var g uintptr
	procGdipGetImageGraphicsContext.Call(img, uintptr(unsafe.Pointer(&g)))
	return g
}

func gpImageSize(img uintptr) (int32, int32) {
	var w, h uint32
	procGdipGetImageWidth.Call(img, uintptr(unsafe.Pointer(&w)))
	procGdipGetImageHeight.Call(img, uintptr(unsafe.Pointer(&h)))
	return int32(w), int32(h)
}

func gpCreateHICON(bmp uintptr) uintptr {
	var icon uintptr
	procGdipCreateHICONFromBitmap.Call(bmp, uintptr(unsafe.Pointer(&icon)))
	return icon
}

// ---------------------------------------------------------------------------
// Graphics state & drawing
// ---------------------------------------------------------------------------

func gpClear(g uintptr, argb uint32) {
	procGdipGraphicsClear.Call(g, uintptr(argb))
}

func gpInitQuality(g uintptr) {
	procGdipSetInterpolationMode.Call(g, interpolationHighQualityBicubic)
	procGdipSetSmoothingMode.Call(g, smoothingModeAntiAlias)
	procGdipSetPixelOffsetMode.Call(g, pixelOffsetModeHalf)
}

func gpResetTransform(g uintptr) {
	procGdipResetWorldTransform.Call(g)
}

func gpTranslateTransform(g uintptr, dx, dy float32) {
	procGdipTranslateWorldTransform.Call(g, f32(dx), f32(dy), matrixOrderAppend)
}

func gpRotateTransform(g uintptr, deg float32) {
	procGdipRotateWorldTransform.Call(g, f32(deg), matrixOrderAppend)
}

func gpScaleTransform(g uintptr, sx, sy float32) {
	procGdipScaleWorldTransform.Call(g, f32(sx), f32(sy), matrixOrderAppend)
}

func gpDrawImageScaled(g, img uintptr, dx, dy, dw, dh, sw, sh int32) {
	procGdipDrawImageRectRectI.Call(g, img,
		uintptr(dx), uintptr(dy), uintptr(dw), uintptr(dh),
		0, 0, uintptr(sw), uintptr(sh),
		unitPixel, 0, 0, 0)
}

func gpCreateBrush(argb uint32) uintptr {
	var b uintptr
	procGdipCreateSolidFill.Call(uintptr(argb), uintptr(unsafe.Pointer(&b)))
	return b
}

func gpDeleteBrush(b uintptr) { procGdipDeleteBrush.Call(b) }

func gpFillEllipse(g, brush uintptr, x, y, w, h float64) {
	procGdipFillEllipseI.Call(g, brush, i32(x), i32(y), i32(w), i32(h))
}

func gpDrawEllipse(g, pen uintptr, x, y, w, h float64) {
	procGdipDrawEllipseI.Call(g, pen, i32(x), i32(y), i32(w), i32(h))
}

func gpFillRect(g, brush uintptr, x, y, w, h float64) {
	procGdipFillRectangleI.Call(g, brush, i32(x), i32(y), i32(w), i32(h))
}

func gpCreatePen(argb uint32, width float32) uintptr {
	var p uintptr
	procGdipCreatePen1.Call(uintptr(argb), f32(width), unitPixel, uintptr(unsafe.Pointer(&p)))
	return p
}

func gpDeletePen(p uintptr) { procGdipDeletePen.Call(p) }

func gpDrawLine(g, pen uintptr, x1, y1, x2, y2 float64) {
	procGdipDrawLineI.Call(g, pen, i32(x1), i32(y1), i32(x2), i32(y2))
}

func i32(v float64) uintptr { return uintptr(int32(v)) }

func roundRectPath(x, y, w, h, r float64) (uintptr, bool) {
	if r*2 > w {
		r = w / 2
	}
	if r*2 > h {
		r = h / 2
	}
	var path uintptr
	st, _, _ := procGdipCreatePath.Call(0, uintptr(unsafe.Pointer(&path)))
	if st != 0 || path == 0 {
		return 0, false
	}
	r2 := r * 2
	procGdipAddPathArcI.Call(path, i32(x+w-r2), i32(y), i32(r2), i32(r2), f32(180), f32(90))      // top-right
	procGdipAddPathArcI.Call(path, i32(x+w-r2), i32(y+h-r2), i32(r2), i32(r2), f32(270), f32(90)) // bottom-right
	procGdipAddPathArcI.Call(path, i32(x), i32(y+h-r2), i32(r2), i32(r2), f32(0), f32(90))        // bottom-left
	procGdipAddPathArcI.Call(path, i32(x), i32(y), i32(r2), i32(r2), f32(90), f32(90))            // top-left
	procGdipCloseFigure.Call(path)
	return path, true
}

func gpFillRoundedRect(g, brush uintptr, x, y, w, h, r float64) {
	path, ok := roundRectPath(x, y, w, h, r)
	if !ok {
		return
	}
	procGdipFillPath.Call(g, brush, path)
	procGdipDeletePath.Call(path)
}

func gpDrawRoundedRect(g, pen uintptr, x, y, w, h, r float64) {
	path, ok := roundRectPath(x, y, w, h, r)
	if !ok {
		return
	}
	procGdipDrawPath.Call(g, pen, path)
	procGdipDeletePath.Call(path)
}

// ---------------------------------------------------------------------------
// Text
// ---------------------------------------------------------------------------

// gpCreateFont probes the preferred font families (first match wins) and
// returns (font, fontFamily). Both must be destroyed on exit.
func gpCreateFont(sizePx float32) (uintptr, uintptr) {
	names := []string{"Microsoft YaHei UI", "Microsoft YaHei", "Segoe UI", "Arial"}
	for _, name := range names {
		np := mustW(name)
		if np == nil {
			continue
		}
		var fam uintptr
		st, _, _ := procGdipCreateFontFamilyFromName.Call(uintptr(unsafe.Pointer(np)), 0,
			uintptr(unsafe.Pointer(&fam)))
		if st != 0 || fam == 0 {
			continue
		}
		var font uintptr
		st, _, _ = procGdipCreateFont.Call(fam, f32(sizePx), 0, unitPixel, uintptr(unsafe.Pointer(&font)))
		if st == 0 && font != 0 {
			return font, fam
		}
		procGdipDeleteFontFamily.Call(fam)
	}
	return 0, 0
}

// gpMeasureText measures a NUL-terminated UTF-16 string.
func gpMeasureText(g, font uintptr, s *uint16, maxW, maxH float64) (float64, float64) {
	layout := gpRectF{0, 0, float32(maxW), float32(maxH)}
	box := gpRectF{0, 0, 0, 0}
	procGdipMeasureString.Call(g, uintptr(unsafe.Pointer(s)), ^uintptr(0), font,
		uintptr(unsafe.Pointer(&layout)), 0,
		uintptr(unsafe.Pointer(&box)), 0, 0)
	if box.Width < 0 {
		box.Width = 0
	}
	if box.Height < 0 {
		box.Height = 0
	}
	return float64(box.Width), float64(box.Height)
}

// gpDrawText draws a NUL-terminated UTF-16 string with near/near alignment.
func gpDrawText(g, font uintptr, s *uint16, x, y, maxW, maxH float64, brush uintptr) {
	layout := gpRectF{float32(x), float32(y), float32(maxW), float32(maxH)}
	procGdipDrawString.Call(g, uintptr(unsafe.Pointer(s)), ^uintptr(0), font,
		uintptr(unsafe.Pointer(&layout)), 0, brush)
}

// ---------------------------------------------------------------------------
// Image helpers
// ---------------------------------------------------------------------------

// gpImageEnsureTransparency detects a fully-opaque source image and applies a
// soft rounded-rect feather mask so the pet does not look like a photo tile.
// Images that already contain transparent pixels are left untouched.
func gpImageEnsureTransparency(bmp uintptr) {
	if bmp == 0 {
		return
	}
	srcW, srcH := gpImageSize(bmp)
	rect := gpRectI{0, 0, srcW, srcH}
	var bd gpBitmapData
	st, _, _ := procGdipBitmapLockBits.Call(bmp, uintptr(unsafe.Pointer(&rect)),
		imageLockModeReadWrite, pixFmt32bppARGB, uintptr(unsafe.Pointer(&bd)))
	if st != 0 || bd.Scan0 == 0 {
		return
	}
	defer procGdipBitmapUnlockBits.Call(bmp, uintptr(unsafe.Pointer(&bd)))

	w := int(bd.Width)
	h := int(bd.Height)
	if w < 16 || h < 16 || bd.Stride == 0 {
		return
	}
	stride := int(bd.Stride) / 4
	pix := unsafe.Slice((*uint32)(unsafe.Pointer(bd.Scan0)), h*stride)

	// Sample points near the border and the mid-edges.
	clampX := func(v int) int {
		if v < 0 {
			return 0
		}
		if v >= w {
			return w - 1
		}
		return v
	}
	clampY := func(v int) int {
		if v < 0 {
			return 0
		}
		if v >= h {
			return h - 1
		}
		return v
	}
	samples := [][2]int{
		{2, 2}, {w - 3, 2}, {2, h / 2}, {w - 3, h / 2},
		{2, h - 3}, {w - 3, h - 3}, {w / 2, 2}, {w / 2, h - 3},
	}
	opaque := true
	for _, p := range samples {
		if pix[clampY(p[1])*stride+clampX(p[0])]>>24 < 250 {
			opaque = false
			break
		}
	}
	if !opaque {
		return
	}

	// Rounded-rect soft mask (signed-distance feather).
	const inset = 4.0
	const feather = 12.0
	radius := float64(w) * 0.07
	if radius < 10 {
		radius = 10
	}
	if radius > 60 {
		radius = 60
	}
	cx := float64(w) / 2
	cy := float64(h) / 2
	hw := float64(w)/2 - inset
	hh := float64(h)/2 - inset
	if hw < radius {
		radius = hw
	}
	if hh < radius {
		radius = hh
	}
	for y := 0; y < h; y++ {
		qy := math.Abs(float64(y)+0.5-cy) - (hh - radius)
		if qy < 0 {
			qy = 0
		}
		for x := 0; x < w; x++ {
			qx := math.Abs(float64(x)+0.5-cx) - (hw - radius)
			if qx < 0 {
				qx = 0
			}
			d := math.Sqrt(qx*qx+qy*qy) - radius
			if d <= 0 {
				continue
			}
			factor := 1.0 - d/feather
			if factor <= 0 {
				idx := y*stride + x
				pix[idx] = pix[idx] & 0x00FFFFFF
			} else if factor < 1 {
				idx := y*stride + x
				v := pix[idx]
				na := float64((v>>24)&0xFF) * factor
				if na < 0 {
					na = 0
				}
				if na > 255 {
					na = 255
				}
				pix[idx] = (v & 0x00FFFFFF) | (uint32(na) << 24)
			}
		}
	}
}
