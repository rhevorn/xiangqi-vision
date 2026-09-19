// Package vision 负责从画面中提取棋盘信息（《技术方案》§6-§8、§10）。
//
// 本包刻意只依赖 Go 标准库：MVP 需要的图像操作只有灰度化、区域取样与帧间
// 差分，纯 Go 实现足够快（90 个取样点约 0.5 ms），而且省掉了 OpenCV 这一个
// 重量级原生依赖，二进制可以直接分发。若将来需要模板匹配等高级算子，
// 只需替换本包的实现，接口不变。
package vision

import (
	"image"
	"image/color"
)

// ToGray 把 RGBA 图像转成灰度图。
//
// 直接按字节遍历而不是调用 image.Image 接口，避免每像素一次接口调用的开销。
func ToGray(img *image.RGBA) *image.Gray {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	g := image.NewGray(image.Rect(0, 0, w, h))

	for y := 0; y < h; y++ {
		srcOff := img.PixOffset(b.Min.X, b.Min.Y+y)
		dstOff := g.PixOffset(0, y)
		for x := 0; x < w; x++ {
			r := int(img.Pix[srcOff+0])
			gg := int(img.Pix[srcOff+1])
			bl := int(img.Pix[srcOff+2])
			// Rec. 601 亮度权重
			g.Pix[dstOff] = uint8((299*r + 587*gg + 114*bl) / 1000)
			srcOff += 4
			dstOff++
		}
	}
	return g
}

// clampi 把 v 限制在 [lo, hi] 区间内。
func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------------------------------------------------------------------------
// 绘制辅助：用于输出校准预览图，便于人工核对 90 个取样点是否落在交叉点上
// ---------------------------------------------------------------------------

var (
	colGrid  = color.RGBA{R: 0x21, G: 0x96, B: 0xF3, A: 0xFF} // 蓝色：网格线
	colPoint = color.RGBA{R: 0xF4, G: 0x43, B: 0x36, A: 0xFF} // 红色：取样点
	colROI   = color.RGBA{R: 0x4C, G: 0xAF, B: 0x50, A: 0xFF} // 绿色：取样区域
)

// setPixel 安全地写入一个像素。
func setPixel(img *image.RGBA, x, y int, c color.RGBA) {
	if !(image.Point{X: x, Y: y}).In(img.Bounds()) {
		return
	}
	off := img.PixOffset(x, y)
	img.Pix[off+0] = c.R
	img.Pix[off+1] = c.G
	img.Pix[off+2] = c.B
	img.Pix[off+3] = c.A
}

// drawHLine 画一条水平线。
func drawHLine(img *image.RGBA, x0, x1, y int, c color.RGBA) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		setPixel(img, x, y, c)
	}
}

// drawVLine 画一条垂直线。
func drawVLine(img *image.RGBA, x, y0, y1 int, c color.RGBA) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		setPixel(img, x, y, c)
	}
}

// drawRectOutline 画矩形边框。
func drawRectOutline(img *image.RGBA, r Rect, c color.RGBA) {
	drawHLine(img, r.X, r.X+r.Width-1, r.Y, c)
	drawHLine(img, r.X, r.X+r.Width-1, r.Y+r.Height-1, c)
	drawVLine(img, r.X, r.Y, r.Y+r.Height-1, c)
	drawVLine(img, r.X+r.Width-1, r.Y, r.Y+r.Height-1, c)
}

// drawCross 在 (x,y) 处画一个十字标记。
func drawCross(img *image.RGBA, x, y, arm int, c color.RGBA) {
	drawHLine(img, x-arm, x+arm, y, c)
	drawVLine(img, x, y-arm, y+arm, c)
}
