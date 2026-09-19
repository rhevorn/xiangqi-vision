package vision

import (
	"fmt"
	"image"
	"sort"
	"strings"

	"xiangqi-vision/internal/game"
)

// CellDiff 是一个交叉点在两帧之间的差异（《技术方案》§7）。
type CellDiff struct {
	Row   int
	Col   int
	Score float64 // 取样区域的平均绝对差，范围 0~255
}

// Square 返回该差异对应的棋盘坐标。
func (d CellDiff) Square() game.Square { return game.Square{Row: d.Row, Col: d.Col} }

// String 返回形如 "H2=81.2" 的表示，对应 §20 的日志格式。
func (d CellDiff) String() string {
	return fmt.Sprintf("%s=%.1f", d.Square(), d.Score)
}

// DiffCells 计算两幅灰度图在全部 90 个交叉点上的平均绝对差。
//
// 两幅图应来自同一尺寸的画面与同一套校准参数。取样区域越出画面时会钳制
// 到画面边缘，因此棋盘贴着屏幕边也不会越界。
func DiffCells(a, b *image.Gray, cal *Calibration) []CellDiff {
	out := make([]CellDiff, 0, game.NumSquares)
	for r := 0; r < game.Rows; r++ {
		for c := 0; c < game.Cols; c++ {
			out = append(out, CellDiff{
				Row:   r,
				Col:   c,
				Score: meanAbsDiffROI(a, b, cal.cells[r][c].ROI),
			})
		}
	}
	return out
}

// meanAbsDiffROI 返回两块区域的平均绝对灰度差（0~255）。
//
// 这是第一版最简单的差异度量（§7：Gray → absdiff → mean pixel difference）。
func meanAbsDiffROI(a, b *image.Gray, roi Rect) float64 {
	if roi.Width <= 0 || roi.Height <= 0 {
		return 0
	}
	ab, bb := a.Bounds(), b.Bounds()

	var sum int64
	for y := 0; y < roi.Height; y++ {
		sy := roi.Y + y
		ay := clampi(sy, ab.Min.Y, ab.Max.Y-1)
		by := clampi(sy, bb.Min.Y, bb.Max.Y-1)
		aRow := a.PixOffset(ab.Min.X, ay)
		bRow := b.PixOffset(bb.Min.X, by)

		for x := 0; x < roi.Width; x++ {
			sx := roi.X + x
			ax := clampi(sx, ab.Min.X, ab.Max.X-1)
			bx := clampi(sx, bb.Min.X, bb.Max.X-1)

			d := int(a.Pix[aRow+(ax-ab.Min.X)]) - int(b.Pix[bRow+(bx-bb.Min.X)])
			if d < 0 {
				d = -d
			}
			sum += int64(d)
		}
	}
	return float64(sum) / float64(roi.Width*roi.Height)
}

// MaxCellDiff 返回所有格点差异中的最大值。
// 用它判断"画面是否还在变"比用平均值可靠：走子只会影响一两个格子，
// 平均下来很容易被稀释掉。
func MaxCellDiff(diffs []CellDiff) float64 {
	var maxScore float64
	for _, d := range diffs {
		if d.Score > maxScore {
			maxScore = d.Score
		}
	}
	return maxScore
}

// TopChanged 返回差异最大的前 n 个格点，按差异从大到小排列。
func TopChanged(diffs []CellDiff, n int) []CellDiff {
	sorted := make([]CellDiff, len(diffs))
	copy(sorted, diffs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })
	if n > 0 && len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

// FormatTop 把前 n 个差异格式化成日志片段，如 "H2=81.2 E2=76.8"（§20）。
func FormatTop(diffs []CellDiff, n int) string {
	top := TopChanged(diffs, n)
	parts := make([]string, 0, len(top))
	for _, d := range top {
		parts = append(parts, d.String())
	}
	return strings.Join(parts, " ")
}
