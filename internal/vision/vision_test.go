package vision_test

import (
	"image"
	"testing"

	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/vision"
)

// 一套贴近真机的校准参数：1080x2400 的手机屏幕，棋盘占 (120,310) 起的 870x970。
var (
	screenSize   = image.Pt(1080, 2400)
	screenBounds = image.Rect(0, 0, screenSize.X, screenSize.Y)
	boardRect    = vision.Rect{X: 120, Y: 310, Width: 870, Height: 970}
)

func newCal(t *testing.T) *vision.Calibration {
	t.Helper()
	cal, err := vision.NewCalibration(boardRect, 0)
	if err != nil {
		t.Fatalf("NewCalibration: %v", err)
	}
	return cal
}

// render 把棋盘画成灰度图，可选择性地加上"最后一步"高亮。
func render(t *testing.T, b *game.Board, cal *vision.Calibration, highlights ...game.Square) *image.Gray {
	t.Helper()
	return vision.ToGray(vision.RenderSyntheticBoard(b, cal, screenSize, highlights...))
}

func TestToGray(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	// 纯白 → 255，纯黑 → 0，纯红 → 76（Rec.601: 0.299*255）
	for x := 0; x < 4; x++ {
		off := img.PixOffset(x, 0)
		switch x {
		case 0:
			img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3] = 255, 255, 255, 255
		case 1:
			img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3] = 0, 0, 0, 255
		case 2:
			img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3] = 255, 0, 0, 255
		case 3:
			img.Pix[off+0], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3] = 0, 255, 0, 255
		}
	}
	g := vision.ToGray(img)
	if g.Bounds().Dx() != 4 || g.Bounds().Dy() != 2 {
		t.Fatalf("灰度图尺寸错误: %v", g.Bounds())
	}
	want := []uint8{255, 0, 76, 149}
	for x, w := range want {
		if got := g.GrayAt(x, 0).Y; got != w {
			t.Errorf("灰度(%d,0) = %d，期望 %d", x, got, w)
		}
	}
}

func TestCalibrationGeometry(t *testing.T) {
	cal := newCal(t)

	dx, dy := cal.Pitch()
	if want := 870.0 / 8; dx != want {
		t.Errorf("dx = %v，期望 %v（宽度 / 8）", dx, want)
	}
	if want := 970.0 / 9; dy != want {
		t.Errorf("dy = %v，期望 %v（高度 / 9）", dy, want)
	}

	tl, _ := cal.CellAt(0, 0)
	if tl.X != boardRect.X || tl.Y != boardRect.Y {
		t.Errorf("左上角格点 = (%d,%d)，期望 (%d,%d)", tl.X, tl.Y, boardRect.X, boardRect.Y)
	}
	br, _ := cal.CellAt(game.Rows-1, game.Cols-1)
	if br.X != boardRect.X+boardRect.Width || br.Y != boardRect.Y+boardRect.Height {
		t.Errorf("右下角格点 = (%d,%d)，期望 (%d,%d)",
			br.X, br.Y, boardRect.X+boardRect.Width, boardRect.Y+boardRect.Height)
	}

	if got := len(cal.Cells()); got != 90 {
		t.Errorf("应有 90 个格点，实际 %d", got)
	}

	// 取样区域应明显小于格距，才能避开格子边缘的装饰
	if cal.ROISize >= int(dx) || cal.ROISize >= int(dy) {
		t.Errorf("取样尺寸 %d 不小于格距 %.1fx%.1f", cal.ROISize, dx, dy)
	}

	if err := cal.Validate(screenBounds); err != nil {
		t.Errorf("该组参数应完全落在画面内: %v", err)
	}
}

func TestCalibrationOutOfBounds(t *testing.T) {
	// 棋盘右下角超出屏幕
	cal, err := vision.NewCalibration(vision.Rect{X: 120, Y: 310, Width: 1200, Height: 2200}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := cal.Validate(screenBounds); err == nil {
		t.Error("取样点越界时 Validate 应当报错")
	}

	if _, err := vision.NewCalibration(vision.Rect{Width: 0, Height: 0}, 0); err == nil {
		t.Error("空矩形应被拒绝")
	}
}

// 同一局面渲染两次，差分应全为 0。
func TestDiffIdenticalFrames(t *testing.T) {
	cal := newCal(t)
	b := game.NewBoard()

	a := render(t, b, cal)
	c := render(t, b, cal)

	diffs := vision.DiffCells(a, c, cal)
	if len(diffs) != 90 {
		t.Fatalf("应返回 90 个差异，实际 %d", len(diffs))
	}
	if max := vision.MaxCellDiff(diffs); max > 0.001 {
		t.Errorf("同一局面最大差异应为 0，实际 %.3f", max)
	}
}

// 走一步棋之后，应当只有起止两格发生明显变化。
func TestDiffAfterMove(t *testing.T) {
	cal := newCal(t)
	b0 := game.NewBoard()
	m, err := game.ParseChinese(b0, "炮二平五") // h2e2
	if err != nil {
		t.Fatal(err)
	}

	b1 := b0.Clone()
	if err := b1.Apply(m); err != nil {
		t.Fatal(err)
	}

	diffs := vision.DiffCells(render(t, b0, cal), render(t, b1, cal), cal)

	top := vision.TopChanged(diffs, 3)
	t.Logf("变化最大的格点: %s", vision.FormatTop(diffs, 5))

	if top[0].Score < 50 {
		t.Errorf("起点差异应很明显，实际只有 %.1f", top[0].Score)
	}
	// 前两名应当正好是 h2 与 e2（顺序取决于棋子与底色的对比度）
	got := map[game.Square]bool{top[0].Square(): true, top[1].Square(): true}
	for _, want := range []game.Square{m.From, m.To} {
		if !got[want] {
			t.Errorf("变化最大的两格应包含 %s，实际 %s", want, vision.FormatTop(diffs, 3))
		}
	}
	// 第三名应当远小于前两名，说明没有多余格子被误判
	if top[2].Score > top[0].Score*0.3 {
		t.Errorf("第三名差异 %.1f 偏大，前两名 %.1f / %.1f",
			top[2].Score, top[0].Score, top[1].Score)
	}
}

// "最后一步"高亮画在格子边缘，取样区域必须避开它，否则每走一步都会有额外的格子变化。
func TestHighlightDoesNotAffectROI(t *testing.T) {
	cal := newCal(t)
	b := game.NewBoard()

	plain := render(t, b, cal)
	// 给几个空格子加上高亮框（真实 App 里上一手的落点常常是空格）
	highlighted := render(t, b, cal,
		game.Square{Row: 4, Col: 4},
		game.Square{Row: 3, Col: 4},
		game.Square{Row: 5, Col: 0},
	)

	diffs := vision.DiffCells(plain, highlighted, cal)
	if max := vision.MaxCellDiff(diffs); max > 1 {
		t.Errorf("高亮框不应影响取样区域，最大差异却达到 %.2f（%s）",
			max, vision.FormatTop(diffs, 3))
	}
}

func TestDetectMoveCandidates(t *testing.T) {
	cal := newCal(t)
	b0 := game.NewBoard()
	m, _ := game.ParseChinese(b0, "炮二平五")

	b1 := b0.Clone()
	if err := b1.Apply(m); err != nil {
		t.Fatal(err)
	}

	cands := vision.DetectMoveCandidates(vision.DiffCells(render(t, b0, cal), render(t, b1, cal), cal), 20, 4)
	if len(cands) == 0 {
		t.Fatal("未能推断出任何候选走法")
	}
	t.Logf("候选: %v", cands)

	// 正确的走法（或其反向）应当是首选候选
	first := cands[0].Move
	if first != m && first.From != m.To {
		t.Errorf("首选候选 = %s，期望 %s 或其反向", first, m)
	}

	// 关键：反向走法在规则上不合法，因此规则层能唯一定出真实走法
	reverse := game.Move{From: m.To, To: m.From}
	if b0.IsLegal(reverse) {
		t.Error("该测试局面的反向走法本应非法，否则无法体现规则层的筛选作用")
	}
	if !b0.IsLegal(m) {
		t.Errorf("真实走法 %s 在规则上应合法", m)
	}
}

// 候选走法里的非真实选项必须被规则层挡掉。
func TestCandidatesFilteredByRules(t *testing.T) {
	cal := newCal(t)
	b0 := game.NewBoard()
	m, _ := game.ParseChinese(b0, "马二进三") // h0g2

	b1 := b0.Clone()
	if err := b1.Apply(m); err != nil {
		t.Fatal(err)
	}

	cands := vision.DetectMoveCandidates(vision.DiffCells(render(t, b0, cal), render(t, b1, cal), cal), 20, 4)

	var legal []game.Move
	for _, c := range cands {
		if b0.IsLegal(c.Move) {
			legal = append(legal, c.Move)
		}
	}
	if len(legal) != 1 || legal[0] != m {
		t.Errorf("规则层应恰好筛出 %s 一个合法走法，实际 %v", m, legal)
	}
}

// 稳定判定必须能过滤掉走子动画。
func TestStabilityTracker(t *testing.T) {
	cal := newCal(t)
	b0 := game.NewBoard()
	frameA := render(t, b0, cal)

	b1 := b0.Clone()
	m, _ := game.ParseChinese(b0, "炮二平五")
	_ = b1.Apply(m)
	frameB := render(t, b1, cal)

	tracker := vision.NewStabilityTracker(cal, 20, 3)

	// 首帧只用于建立基准
	if settled, _ := tracker.Observe(frameA); settled {
		t.Error("首帧不应报告稳定")
	}
	// 连续静止 3 帧后才稳定
	for i := 1; i <= 2; i++ {
		if settled, _ := tracker.Observe(frameA); settled {
			t.Errorf("第 %d 帧不应报告稳定", i+1)
		}
	}
	if settled, _ := tracker.Observe(frameA); !settled {
		t.Error("连续 3 帧静止后应报告稳定")
	}

	// 画面发生变化：立刻回到不稳定，且不会重复报告
	if settled, d := tracker.Observe(frameB); settled {
		t.Error("画面变化时不应报告稳定")
	} else if d < 20 {
		t.Errorf("走子带来的差异应超过阈值，实际 %.1f", d)
	}
	// 稳定后再变化，同样需要重新累计 3 帧
	for i := 0; i < 2; i++ {
		if settled, _ := tracker.Observe(frameB); settled {
			t.Error("变化后累计不足时不应报告稳定")
		}
	}
	if settled, _ := tracker.Observe(frameB); !settled {
		t.Error("变化后连续 3 帧静止应再次报告稳定")
	}
}

// 模拟真实抓屏：同一静止画面被反复采样，中间夹着动画帧。
func TestStabilityWithAnimation(t *testing.T) {
	cal := newCal(t)
	b := game.NewBoard()
	frame := render(t, b, cal)

	tracker := vision.NewStabilityTracker(cal, 20, 2)
	tracker.Reset(frame)

	// Reset 只用于建立基准，之后仍需连续观测满 2 帧静止才算稳定
	if settled, _ := tracker.Observe(frame); settled {
		t.Fatal("Reset 后的第 1 帧不应报告稳定")
	}
	if settled, _ := tracker.Observe(frame); !settled {
		t.Fatal("Reset 后连续静止 2 帧应报告稳定")
	}

	// 用一步真实走子作为"画面变了"的帧。
	// 注意不能用高亮框来模拟：取样区域本来就避开了高亮（见
	// TestHighlightDoesNotAffectROI），那种帧在 ROI 上与静止帧完全相同。
	next := b.Clone()
	mv, _ := game.ParseChinese(b, "炮二平五")
	if err := next.Apply(mv); err != nil {
		t.Fatal(err)
	}
	moved := render(t, next, cal)

	if settled, d := tracker.Observe(moved); settled {
		t.Error("出现变化时不应报告稳定")
	} else if d < 20 {
		t.Errorf("走子应带来超过阈值的差异，实际 %.1f", d)
	}
	// moved → frame 本身也是一次变化，会再次把计数清零，这正是想要的保守行为：
	// 画面停止变化后还要再多看一帧，确认真的不动了才下结论。
	if settled, _ := tracker.Observe(frame); settled {
		t.Error("从变化帧回到静止帧的那一次仍算变化，不应报告稳定")
	}
	if settled, _ := tracker.Observe(frame); settled {
		t.Error("重新累计到 1 帧，不应报告稳定")
	}
	if settled, _ := tracker.Observe(frame); !settled {
		t.Error("重新静止满 2 帧后应报告稳定")
	}

	// 画面持续不变时不应反复报告，否则会对同一局面重复分析
	for i := 0; i < 5; i++ {
		if settled, _ := tracker.Observe(frame); settled {
			t.Fatalf("画面持续静止时第 %d 次观测又报告了一次稳定", i+1)
		}
	}
}

// 端到端：从初始局面连续走若干步，每一步都能被差分正确捕获并被规则层确认。
func TestSequentialMoveDetection(t *testing.T) {
	cal := newCal(t)

	b := game.NewBoard()
	prevFrame := render(t, b, cal)

	notations := []string{
		"炮二平五", "马8进7",
		"马二进三", "车9平8",
		"车一平二", "马2进3",
		"兵七进一", "卒7进1",
		"车二进六", "炮8平9",
	}

	for i, notation := range notations {
		want, err := game.ParseChinese(b, notation)
		if err != nil {
			t.Fatalf("第 %d 手 ParseChinese(%q): %v", i+1, notation, err)
		}

		next := b.Clone()
		if err := next.Apply(want); err != nil {
			t.Fatalf("第 %d 手 Apply: %v", i+1, err)
		}

		// 真实 App 会高亮"最后一步"，这里一并模拟
		curFrame := render(t, next, cal, want.From, want.To)

		diffs := vision.DiffCells(prevFrame, curFrame, cal)
		cands := vision.DetectMoveCandidates(diffs, 20, 4)
		if len(cands) == 0 {
			t.Fatalf("第 %d 手（%s）未能推断出候选走法，差异 %s",
				i+1, notation, vision.FormatTop(diffs, 3))
		}

		// 规则层裁决：候选里应当恰好有一个合法走法，且就是真实走法
		var legal []game.Move
		for _, c := range cands {
			if b.IsLegal(c.Move) {
				legal = append(legal, c.Move)
			}
		}
		if len(legal) != 1 {
			t.Fatalf("第 %d 手（%s）规则层筛出 %d 个合法走法，期望 1 个；候选 %v",
				i+1, notation, len(legal), cands)
		}
		if legal[0] != want {
			t.Fatalf("第 %d 手推断为 %s，实际是 %s", i+1, legal[0], want)
		}

		b = next
		prevFrame = curFrame
	}
}
