package vision

import (
	"image"
	"image/color"

	"xiangqi-vision/internal/game"
)

// RenderSyntheticBoard 按给定棋盘状态合成一张画面。
//
// 它有两个用途：一是让视觉与全链路可以在没有手机的情况下被确定性地测试；
// 二是提供一个"干跑"手段——不用连真机就能验证校准、差分与走法推断是否正常。
//
// highlights 用来模拟象棋 App 的"最后一步"高亮框：这些装饰画在格子边缘，
// 正好可以用来检验取样区域是否真的避开了它们。
func RenderSyntheticBoard(b *game.Board, cal *Calibration, size image.Point, highlights ...game.Square) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))

	// 棋盘底色（木色）：灰度约 225，明显亮于所有棋子
	fillBackground(img, synthBackground)

	drawGrid(img, cal)

	// 高亮框画在棋子之前，模拟真实 App 里高亮在棋子之下
	for _, sq := range highlights {
		if cell, ok := cal.CellFor(sq); ok {
			drawHighlight(img, cell, cal)
		}
	}

	// 棋子
	dx, dy := cal.Pitch()
	radius := int(minf(dx, dy) * 0.40)
	for r := 0; r < game.Rows; r++ {
		for c := 0; c < game.Cols; c++ {
			p := b.At(game.Square{Row: r, Col: c})
			if p.IsEmpty() {
				continue
			}
			cell, ok := cal.CellAt(r, c)
			if !ok {
				continue
			}
			fillDisc(img, cell.X, cell.Y, radius, pieceShade(p))
		}
	}
	return img
}

// synthBackgroundGray 是合成棋盘的底色灰度，用来说明棋子灰度的选取范围。
const synthBackgroundGray = 225

// pieceShade 给每枚棋子一个可区分的灰度值。
//
// 真实棋子上写的是汉字，但那是 OCR 要解决的问题——第一版根本不识别棋子
// 类型，只需要"这里有个子 / 这里没子"以及"换了个子"能被差分看出来。
//
// 取值范围刻意避开底色 225：红子 38~86，黑子 118~166，于是
// 「黑卒 vs 底色」的最小差值也有 59，远高于差分阈值。
func pieceShade(p game.Piece) color.RGBA {
	base := 30
	if p.Color == game.Black {
		base = 110
	}
	v := uint8(base + int(p.Type)*8)
	return color.RGBA{R: v, G: v, B: v, A: 0xFF}
}

// fillBackground 填充整个画面。
func fillBackground(img *image.RGBA, c color.RGBA) {
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			setPixel(img, x, y, c)
		}
	}
}

// drawGrid 依照校准参数画出棋盘线：横竖各 9/10 条，外加九宫斜线。
func drawGrid(img *image.RGBA, cal *Calibration) {
	for r := 0; r < game.Rows; r++ {
		left, _ := cal.CellAt(r, 0)
		right, _ := cal.CellAt(r, game.Cols-1)
		drawHLine(img, left.X, right.X, left.Y, synthGrid)
	}
	for c := 0; c < game.Cols; c++ {
		top, _ := cal.CellAt(0, c)
		bottom, _ := cal.CellAt(game.Rows-1, c)
		// 河界处竖线断开，与实际棋盘一致
		if c == 0 || c == game.Cols-1 {
			drawVLine(img, top.X, top.Y, bottom.Y, synthGrid)
			continue
		}
		midTop, _ := cal.CellAt(4, c)
		midBottom, _ := cal.CellAt(5, c)
		drawVLine(img, top.X, top.Y, midTop.Y, synthGrid)
		drawVLine(img, top.X, midBottom.Y, bottom.Y, synthGrid)
	}

	// 九宫斜线
	for _, palace := range [][2][2]int{
		{{0, 3}, {2, 5}}, {{0, 5}, {2, 3}}, // 黑方九宫
		{{7, 3}, {9, 5}}, {{7, 5}, {9, 3}}, // 红方九宫
	} {
		from, _ := cal.CellAt(palace[0][0], palace[0][1])
		to, _ := cal.CellAt(palace[1][0], palace[1][1])
		drawLine(img, from.X, from.Y, to.X, to.Y, synthGrid)
	}
}

// drawHighlight 画一个"最后一步"高亮方框，位置在格子边缘。
func drawHighlight(img *image.RGBA, cell Cell, cal *Calibration) {
	dx, dy := cal.Pitch()
	w := int(dx * 0.84)
	h := int(dy * 0.84)
	rect := centeredRectSized(cell.X, cell.Y, w, h)
	for i := 0; i < 3; i++ { // 加粗，模拟高亮的显眼程度
		drawRectOutline(img, Rect{X: rect.X + i, Y: rect.Y + i, Width: rect.Width - 2*i, Height: rect.Height - 2*i}, colHighlight)
	}
}

// centeredRectSized 返回以 (x,y) 为中心、指定宽高的矩形。
func centeredRectSized(x, y, w, h int) Rect {
	return Rect{X: x - w/2, Y: y - h/2, Width: w, Height: h}
}

// fillDisc 填充一个实心圆。
func fillDisc(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				setPixel(img, x, y, c)
			}
		}
	}
}

// drawLine 用 Bresenham 画任意方向的直线（九宫斜线需要）。
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := absi(x1 - x0)
	dy := -absi(y1 - y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		setPixel(img, x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

var (
	colHighlight    = color.RGBA{R: 0xFF, G: 0xC1, B: 0x07, A: 0xFF} // 琥珀色：最后一步高亮
	synthGrid       = color.RGBA{R: 0x6B, G: 0x4A, B: 0x33, A: 0xFF} // 深棕：棋盘线
	synthBackground = color.RGBA{
		R: synthBackgroundGray, G: uint8(synthBackgroundGray * 0xC8 / 0xE3), B: uint8(synthBackgroundGray * 0x96 / 0xE3), A: 0xFF,
	}
)

func absi(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
