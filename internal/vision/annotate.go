package vision

import (
	"image"
	"image/color"
)

var (
	colTop1  = color.RGBA{R: 0xD3, G: 0x2F, B: 0x2F, A: 0xFF} // 红色：差异最大
	colTopN  = color.RGBA{R: 0xFF, G: 0x98, B: 0x00, A: 0xFF} // 橙色：其余候选
	colBoard = color.RGBA{R: 0x00, G: 0xBC, B: 0xD4, A: 0xFF} // 青色：棋盘范围
)

// AnnotateDiffs 在画面上框出差异最大的若干格点，用于排查识别失败（§20）。
//
// 视觉问题光看数字很难定位，把"程序认为哪几个格子在变"直接画出来，
// 一眼就能看出是校准偏了、还是动画没滤干净。
func AnnotateDiffs(img *image.RGBA, cal *Calibration, diffs []CellDiff, topN int) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	copy(out.Pix, img.Pix)

	drawRectOutline(out, cal.Board, colBoard)

	dx, dy := cal.Pitch()
	boxW, boxH := int(dx*0.9), int(dy*0.9)

	for i, d := range TopChanged(diffs, topN) {
		cell, ok := cal.CellAt(d.Row, d.Col)
		if !ok {
			continue
		}
		c := colTopN
		if i == 0 {
			c = colTop1
		}
		drawRectOutline(out, centeredRectSized(cell.X, cell.Y, boxW, boxH), c)
	}
	return out
}
