//go:build windows

// render.go — WhalePet animation: idle floating/breathing, cleaning shake
// with sparkles, happy bounce with stars, particles and the speech bubble.
package main

import (
	"fmt"
	"math"
	"math/rand"
	"syscall"
	"time"
	"unsafe"
)

// particle kinds
const (
	pkSpark = iota
	pkStar
)

// bubble colors
const (
	colBubbleFill   = 0xF2FFFFFF
	colBubbleBorder = 0xFF8FB6E8
	colBubbleTail   = 0xF2FFFFFF
	colTextNormal   = 0xFF33404E
)

type Particle struct {
	x, y    float64
	vx, vy  float64
	life    float64
	maxLife float64
	kind    int
	size    float64
	rot     float64
	vrot    float64
	argb    uint32
}

func scaleAlpha(argb uint32, f float64) uint32 {
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	a := float64((argb>>24)&0xFF) * f
	if a > 255 {
		a = 255
	}
	return (argb & 0x00FFFFFF) | (uint32(a) << 24)
}

// drawFrame renders one frame of the pet and presents it through
// UpdateLayeredWindow (per-pixel alpha).
func (a *App) drawFrame(now time.Time) {
	g := a.graphics
	if g == 0 {
		return
	}
	dt := 1.0 / 30.0
	if !a.lastFrame.IsZero() {
		if d := now.Sub(a.lastFrame).Seconds(); d > 0 && d < 1.0 {
			dt = d
		}
	}
	a.lastFrame = now
	t := now.Sub(a.animStart).Seconds()

	// start from a fully transparent surface
	gpClear(g, 0x00000000)

	// ---- animation ---------------------------------------------------------
	var bob float64
	rot := 0.0
	sx, sy := 1.0, 1.0

	switch a.state {
	case stateIdle:
		bob = math.Sin(t*1.8) * 6
		br := math.Sin(t*1.8+math.Pi/2) * 0.015 // breathing
		sx, sy = 1+br, 1-br
		if math.Mod(t, 13) < 1.4 { // occasional wiggle
			rot = math.Sin(t*14) * 3
		}
	case stateCleaning:
		bob = math.Abs(math.Sin(t*9)) * 8
		rot = math.Sin(t*30) * 7
		if rand.Float64() < 0.85 {
			a.spawnSpark()
		}
	case stateHappy:
		bob = math.Abs(math.Sin(t*10)) * 14
		br := math.Sin(t*10) * 0.02
		sx, sy = 1+br, 1-br
		if rand.Float64() < 0.7 {
			a.spawnStar()
		}
	}

	cx := float64(a.petX) + float64(a.petDW)/2
	cy := float64(a.petY) + float64(a.petDH)/2 + bob

	// ---- ground shadow -----------------------------------------------------
	flat := 1.0 - bob*0.012
	if flat < 0.4 {
		flat = 0.4
	}
	shadow := gpCreateBrush(0x2E000000)
	sw := float64(a.petDW) * 0.22 * flat
	sh := 7.0 * flat
	gpFillEllipse(g, shadow, cx-sw, float64(a.petY+a.petDH)-6-sh, sw*2, sh*2)
	gpDeleteBrush(shadow)

	// ---- the pet -------------------------------------------------------------
	gpResetTransform(g)
	gpTranslateTransform(g, float32(cx), float32(cy))
	gpRotateTransform(g, float32(rot))
	gpScaleTransform(g, float32(sx), float32(sy))
	gpDrawImageScaled(g, a.petImg, -a.petDW/2, -a.petDH/2, a.petDW, a.petDH,
		a.petSrcW, a.petSrcH)
	gpResetTransform(g)

	// ---- particles ---------------------------------------------------------
	a.updateDrawParticles(g, dt)

	// orbiting dots while cleaning
	if a.state == stateCleaning {
		for i := 0; i < 3; i++ {
			ang := t*5 + float64(i)*2.0944
			px := cx + math.Cos(ang)*62
			py := float64(a.petY) - 22 + math.Sin(ang)*9
			v := 0.45 + 0.55*(0.5+0.5*math.Sin(ang+math.Pi/2))
			b := gpCreateBrush(scaleAlpha(0xFFDDEEFF, v))
			gpFillEllipse(g, b, px-4, py-4, 8, 8)
			gpDeleteBrush(b)
		}
	}

	// ---- speech bubble -------------------------------------------------------
	a.drawBubble(g)

	// ---- present --------------------------------------------------------------
	size := POINT{winW, winH}
	ptSrc := POINT{0, 0}
	blend := BLENDFUNCTION{BlendOp: 0, BlendFlags: 0, SourceConstantAlpha: 255, AlphaFormat: 1}
	procUpdateLayeredWindow.Call(a.hwnd, 0, 0,
		uintptr(unsafe.Pointer(&size)),
		a.memDC,
		uintptr(unsafe.Pointer(&ptSrc)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		ULW_ALPHA)
}

// bubbleContent returns the text (and its color) for the speech bubble.
func (a *App) bubbleContent() (string, uint32) {
	if a.state == stateCleaning {
		return "清理中...", colTextNormal
	}
	if time.Now().Before(a.bubbleUntil) {
		return a.bubbleMsg, a.bubbleAccent
	}
	var accent uint32 = colTextNormal
	switch {
	case a.ramPct >= 80:
		accent = 0xFFE05252
	case a.ramPct >= 60:
		accent = 0xFFE8890C
	default:
		accent = 0xFF2E9E5B
	}
	return fmt.Sprintf("内存 %d%%", a.ramPct), accent
}

func (a *App) drawBubble(g uintptr) {
	if a.font == 0 {
		return
	}
	text, accent := a.bubbleContent()
	if text == "" {
		return
	}
	ws, err := syscall.UTF16FromString(text)
	if err != nil || len(ws) == 0 {
		return
	}

	tw, th := gpMeasureText(g, a.font, &ws[0], float64(winW-24), 48)
	if tw <= 0 || th <= 0 {
		return
	}
	const padX = 17.0
	const padY = 10.0
	bw := tw + padX*2
	bh := th + padY*2
	if bh < 30 {
		bh = 30
	}
	x := (float64(winW) - bw) / 2
	y := bubbleTop

	// thought-bubble trail leading towards the pet
	tail := gpCreateBrush(colBubbleTail)
	gpFillEllipse(g, tail, x+bw*0.5-32, y+bh-4, 11, 9)
	gpFillEllipse(g, tail, x+bw*0.5-44, y+bh+8, 8, 7)
	gpDeleteBrush(tail)

	fill := gpCreateBrush(colBubbleFill)
	gpFillRoundedRect(g, fill, x, y, bw, bh, 14)
	gpDeleteBrush(fill)

	pen := gpCreatePen(colBubbleBorder, 1.2)
	gpDrawRoundedRect(g, pen, x, y, bw, bh, 14)
	gpDeletePen(pen)

	brush := gpCreateBrush(accent)
	gpDrawText(g, a.font, &ws[0], x+(bw-tw)/2-1, y+(bh-th)/2, tw+2, th+4, brush)
	gpDeleteBrush(brush)
}

// ---------------------------------------------------------------------------
// particles
// ---------------------------------------------------------------------------

func (a *App) spawnSpark() {
	if len(a.particles) >= 120 {
		return
	}
	x := float64(a.petX) + rand.Float64()*float64(a.petDW)
	y := float64(a.petY) + rand.Float64()*float64(a.petDH)
	life := 0.5 + rand.Float64()*0.7
	colors := []uint32{0xFF9FD8FF, 0xFFC9E9FF, 0xFFFFFFFF, 0xFF7FC4F4}
	a.particles = append(a.particles, Particle{
		x: x, y: y,
		vx: (rand.Float64() - 0.5) * 30,
		vy: -(20 + rand.Float64()*60),
		life: life, maxLife: life,
		kind: pkSpark,
		size: 1.5 + rand.Float64()*2.5,
		argb: colors[rand.Intn(len(colors))],
	})
}

func (a *App) spawnStar() {
	if len(a.particles) >= 120 {
		return
	}
	x := float64(a.petX) + 10 + rand.Float64()*float64(a.petDW-20)
	y := float64(a.petY) + rand.Float64()*float64(a.petDH)*0.8
	life := 0.7 + rand.Float64()*0.8
	a.particles = append(a.particles, Particle{
		x: x, y: y,
		vx: (rand.Float64() - 0.5) * 90,
		vy: -(60 + rand.Float64()*90),
		life: life, maxLife: life,
		kind: pkStar,
		size: 3 + rand.Float64()*3,
		rot: rand.Float64() * 360,
		vrot: (rand.Float64() - 0.5) * 240,
		argb: 0xFFFFD166,
	})
}

func (a *App) updateDrawParticles(g uintptr, dt float64) {
	out := a.particles[:0]
	for _, p := range a.particles {
		p.life -= dt
		if p.life <= 0 {
			continue
		}
		p.x += p.vx * dt
		p.y += p.vy * dt
		if p.kind == pkStar {
			p.vy += 120 * dt // gravity
			p.rot += p.vrot * dt
		}
		fade := p.life / p.maxLife
		if fade > 1 {
			fade = 1
		}
		switch p.kind {
		case pkSpark:
			b := gpCreateBrush(scaleAlpha(p.argb, fade))
			gpFillEllipse(g, b, p.x-p.size, p.y-p.size, p.size*2, p.size*2)
			gpDeleteBrush(b)
		case pkStar:
			pen := gpCreatePen(scaleAlpha(p.argb, fade), 1.4)
			rad := p.rot * math.Pi / 180
			s := math.Cos(rad) * p.size
			c := math.Sin(rad) * p.size
			gpDrawLine(g, pen, p.x-s, p.y-c, p.x+s, p.y+c)
			gpDrawLine(g, pen, p.x+c, p.y-s, p.x-c, p.y+s)
			gpDeletePen(pen)
		}
		out = append(out, p)
	}
	a.particles = out
}
