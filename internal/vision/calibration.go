package vision

import (
	"fmt"
	"image"
	"math"

	"xiangqi-vision/internal/game"
)

// Rect 是画面中的一块矩形区域（像素坐标）。
type Rect struct {
	X      int `yaml:"x"`
	Y      int `yaml:"y"`
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
}

// Valid 报告矩形尺寸是否为正。
func (r Rect) Valid() bool { return r.Width > 0 && r.Height > 0 }

// Contains 报告点 (x,y) 是否落在矩形内。
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

// Cell 是一个棋盘交叉点及其在画面中的取样位置。
type Cell struct {
	Row int  // 棋盘行 0..9
	Col int  // 棋盘列 0..8
	X   int  // 格点中心的画面横坐标
	Y   int  // 格点中心的画面纵坐标
	ROI Rect // 以格点为中心的取样区域
}

// Square 返回该格点对应的棋盘坐标。
func (c Cell) Square() game.Square { return game.Square{Row: c.Row, Col: c.Col} }

// Calibration 把画面像素坐标与棋盘行列对应起来。
//
// Board 记录用户点击的棋盘左上角与右下角**交叉点**所围成的矩形。
// 中国象棋是 9 列 10 行，因此相邻格点的间距为：
//
//	dx = Width  / 8
//	dy = Height / 9
type Calibration struct {
	Board   Rect `yaml:"board"`
	ROISize int  `yaml:"roi_size"`

	dx, dy float64
	cells  [game.Rows][game.Cols]Cell
}

// DefaultROIRatio 是取样区域边长占格点间距的比例。
//
// 取 0.45 而非满格：象棋 App 普遍会在棋子上叠加「最后一步」的高亮方框、
// 选中框或可走点提示，这些装饰都在格子的边缘。把取样区限制在格子中央，
// 可以尽量只看到棋子本身，减少无关变化。
const DefaultROIRatio = 0.45

// NewCalibration 根据棋盘矩形建立坐标映射。roiSize<=0 时按格距自动推算。
func NewCalibration(board Rect, roiSize int) (*Calibration, error) {
	if !board.Valid() {
		return nil, fmt.Errorf("棋盘区域无效: %+v", board)
	}
	if board.Width < game.Cols-1 || board.Height < game.Rows-1 {
		return nil, fmt.Errorf("棋盘区域过小: %dx%d", board.Width, board.Height)
	}

	c := &Calibration{
		Board: board,
		dx:    float64(board.Width) / float64(game.Cols-1),
		dy:    float64(board.Height) / float64(game.Rows-1),
	}
	if roiSize <= 0 {
		roiSize = defaultROISize(c.dx, c.dy)
	}
	c.ROISize = roiSize

	for r := 0; r < game.Rows; r++ {
		for col := 0; col < game.Cols; col++ {
			x := board.X + int(math.Round(float64(col)*c.dx))
			y := board.Y + int(math.Round(float64(r)*c.dy))
			c.cells[r][col] = Cell{
				Row: r,
				Col: col,
				X:   x,
				Y:   y,
				ROI: centeredRect(x, y, roiSize),
			}
		}
	}
	return c, nil
}

// defaultROISize 按格距推算取样边长，并限制在合理范围内。
func defaultROISize(dx, dy float64) int {
	pitch := math.Min(dx, dy)
	n := int(math.Round(pitch * DefaultROIRatio))
	return clampi(n, 8, 128)
}

// centeredRect 返回以 (x,y) 为中心、边长 size 的矩形。
func centeredRect(x, y, size int) Rect {
	h := size / 2
	return Rect{X: x - h, Y: y - h, Width: size, Height: size}
}

// Pitch 返回相邻格点的横纵间距。
func (c *Calibration) Pitch() (dx, dy float64) { return c.dx, c.dy }

// CellAt 返回指定行列的格点。
func (c *Calibration) CellAt(row, col int) (Cell, bool) {
	if row < 0 || row >= game.Rows || col < 0 || col >= game.Cols {
		return Cell{}, false
	}
	return c.cells[row][col], true
}

// CellFor 返回棋盘坐标对应的格点。
func (c *Calibration) CellFor(sq game.Square) (Cell, bool) {
	return c.CellAt(sq.Row, sq.Col)
}

// Cells 按行优先顺序返回全部 90 个格点。
func (c *Calibration) Cells() []Cell {
	out := make([]Cell, 0, game.NumSquares)
	for r := 0; r < game.Rows; r++ {
		for col := 0; col < game.Cols; col++ {
			out = append(out, c.cells[r][col])
		}
	}
	return out
}

// String 返回便于日志的校准摘要。
func (c *Calibration) String() string {
	return fmt.Sprintf("棋盘(%d,%d %dx%d) 格距 %.1fx%.1f 取样 %dpx",
		c.Board.X, c.Board.Y, c.Board.Width, c.Board.Height, c.dx, c.dy, c.ROISize)
}

// Validate 检查取样区域是否都落在画面内。
//
// 即使越界也不会崩溃（取样时会做钳制），但通常意味着用户点错了
// 棋盘角点，提前提示比事后排查差分异常要省事得多。
func (c *Calibration) Validate(bounds image.Rectangle) error {
	var outside int
	for _, cell := range c.Cells() {
		if !cell.ROIInside(bounds) {
			outside++
		}
	}
	if outside == 0 {
		return nil
	}
	return fmt.Errorf("有 %d/90 个取样点超出画面范围（画面 %dx%d），请重新校准",
		outside, bounds.Dx(), bounds.Dy())
}

// ROIInside 报告取样区域是否完全落在给定画面内。
func (c Cell) ROIInside(bounds image.Rectangle) bool {
	return c.ROI.X >= bounds.Min.X && c.ROI.Y >= bounds.Min.Y &&
		c.ROI.X+c.ROI.Width <= bounds.Max.X &&
		c.ROI.Y+c.ROI.Height <= bounds.Max.Y
}

// DrawCalibration 在原图上画出棋盘网格、90 个取样点与取样区域。
//
// 校准是整条链路的地基，让人能一眼看出 90 个点有没有对准交叉点，
// 比反复猜测 diff 数值要高效得多。
func DrawCalibration(img *image.RGBA, cal *Calibration) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	copy(out.Pix, img.Pix)

	// 棋盘外框与内部网格线
	firstRow, _ := cal.CellAt(0, 0)
	lastRow, _ := cal.CellAt(game.Rows-1, 0)
	lastCol, _ := cal.CellAt(0, game.Cols-1)

	for r := 0; r < game.Rows; r++ {
		left, _ := cal.CellAt(r, 0)
		right, _ := cal.CellAt(r, game.Cols-1)
		drawHLine(out, left.X, right.X, left.Y, colGrid)
	}
	for c := 0; c < game.Cols; c++ {
		top, _ := cal.CellAt(0, c)
		bottom, _ := cal.CellAt(game.Rows-1, c)
		drawVLine(out, top.X, top.Y, bottom.Y, colGrid)
	}
	_ = firstRow
	_ = lastRow
	_ = lastCol

	// 90 个取样点与取样区域
	arm := clampi(cal.ROISize/6, 2, 10)
	for _, cell := range cal.Cells() {
		drawRectOutline(out, cell.ROI, colROI)
		drawCross(out, cell.X, cell.Y, arm, colPoint)
	}

	// 标出棋盘四角，便于确认点击结果
	tl, _ := cal.CellAt(0, 0)
	br, _ := cal.CellAt(game.Rows-1, game.Cols-1)
	drawRectOutline(out, centeredRect(tl.X, tl.Y, arm*4), colPoint)
	drawRectOutline(out, centeredRect(br.X, br.Y, arm*4), colPoint)

	return out
}
