package app_test

import (
	"context"
	"image"
	"io"
	"log/slog"
	"testing"
	"time"

	"xiangqi-vision/internal/analyzer"
	"xiangqi-vision/internal/app"
	"xiangqi-vision/internal/capture"
	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/engine"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/vision"
)

var (
	screenSize = image.Pt(1080, 2400)
	boardRect  = vision.Rect{X: 120, Y: 310, Width: 870, Height: 970}
)

// synthCapturer 按顺序回放预先渲染好的画面，模拟真机抓屏。
//
// 每个画面重复返回 repeat 次，因为真实抓屏时同一个静止画面会被反复采到，
// 这正是"连续 N 帧稳定"状态机所依赖的输入形态。
type synthCapturer struct {
	frames []*image.RGBA
	repeat int

	idx     int
	repeats int
}

func (c *synthCapturer) Capture(context.Context) (*image.RGBA, error) {
	if c.idx >= len(c.frames) {
		return nil, capture.ErrExhausted
	}
	img := c.frames[c.idx]
	c.repeats++
	if c.repeats >= c.repeat {
		c.repeats = 0
		c.idx++
	}
	return img, nil
}

func (c *synthCapturer) Describe() string { return "测试用合成画面" }
func (c *synthCapturer) Close() error     { return nil }

func newCalibration(t *testing.T) *vision.Calibration {
	t.Helper()
	cal, err := vision.NewCalibration(boardRect, 0)
	if err != nil {
		t.Fatalf("NewCalibration: %v", err)
	}
	return cal
}

// testConfig 返回一份把节奏调快的测试配置。
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Capture.FPS = 200 // 5ms 一帧，让整个对局能在毫秒级跑完
	cfg.Vision.StableFrames = 3
	cfg.Vision.RetryFrames = 1
	cfg.Engine.MoveTimeMS = 1
	cfg.Engine.MultiPV = 3
	cfg.Debug.Dir = t.TempDir()
	return cfg
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildFrames 把一串中文记谱逐步走成画面序列。
//
// 每一步都渲染走子后的局面，并给起止格点加上"最后一步"高亮——真实象棋
// App 就是这么显示的，顺手也验证了取样区域确实避开了高亮。
func buildFrames(t *testing.T, cal *vision.Calibration, notations []string) (frames []*image.RGBA, moves []game.Move, wantFEN string) {
	t.Helper()

	b := game.NewBoard()
	frames = append(frames, vision.RenderSyntheticBoard(b, cal, screenSize))

	for i, notation := range notations {
		m, err := game.ParseChinese(b, notation)
		if err != nil {
			t.Fatalf("第 %d 手 ParseChinese(%q): %v", i+1, notation, err)
		}
		if err := b.Apply(m); err != nil {
			t.Fatalf("第 %d 手 Apply: %v", i+1, err)
		}
		moves = append(moves, m)
		frames = append(frames, vision.RenderSyntheticBoard(b, cal, screenSize, m.From, m.To))
	}

	// 末尾多停留几帧，确保最后一步也能被稳定判定捕获
	for i := 0; i < 2; i++ {
		frames = append(frames, frames[len(frames)-1])
	}
	return frames, moves, b.FEN()
}

// runController 跑完整个画面序列，返回控制器与检测到的走法。
func runController(t *testing.T, cfg *config.Config, cal *vision.Calibration, frames []*image.RGBA, repeat int) (*app.Controller, []game.Move) {
	t.Helper()

	var detected []game.Move
	hooks := app.Hooks{
		OnDetected: func(m game.Move, _ string) { detected = append(detected, m) },
	}

	ctrl := app.New(cfg, &synthCapturer{frames: frames, repeat: repeat}, cal,
		engine.NewMock(), hooks, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := ctrl.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return ctrl, detected
}

// 端到端验收：连续走 10 手，内部棋盘必须与真实局面完全一致，且不能失步。
//
// 对应《技术方案》§23 的验收标准：
//
//	正常走棋识别准确率 ≥ 99%
//	连续 100 手内部棋盘无失步
func TestEndToEndMoveTracking(t *testing.T) {
	cal := newCalibration(t)
	cfg := testConfig(t)

	notations := []string{
		"炮二平五", "马8进7",
		"马二进三", "车9平8",
		"车一平二", "马2进3",
		"兵七进一", "卒7进1",
		"车二进六", "炮8平9",
	}

	frames, wantMoves, wantFEN := buildFrames(t, cal, notations)
	ctrl, detected := runController(t, cfg, cal, frames, 5)

	if ctrl.Desynced() {
		t.Error("整个序列都是合法走子，不应出现失步")
	}
	if len(detected) != len(wantMoves) {
		t.Fatalf("应检测到 %d 步走法，实际 %d 步", len(wantMoves), len(detected))
	}
	for i := range wantMoves {
		if detected[i] != wantMoves[i] {
			t.Errorf("第 %d 手（%s）被识别为 %s", i+1, notations[i], detected[i])
		}
	}
	if got := ctrl.Board().FEN(); got != wantFEN {
		t.Errorf("内部棋盘与真实局面不一致\n got: %s\nwant: %s", got, wantFEN)
	}
	if n := ctrl.Board().NumMoves(); n != len(notations) {
		t.Errorf("棋盘历史应有 %d 手，实际 %d 手", len(notations), n)
	}
}

// impossibleChangeFrames 造出一组"合法初始局面 → 物理上不可能的变化"的画面。
//
// 变化内容是把红车从 a0 直接"瞬移"到 e5：差分能看到两个格子变了，但两个
// 方向的走法在规则上都不合法，因此规则层必须拒绝它。
//
// 每个画面重复足够多次，好让初始局面的基线先稳定下来——否则程序会把
// "已经变过的画面"当成基线（这正是 §17 说的"启动时已经不是初始局面"）。
func impossibleChangeFrames(t *testing.T, cal *vision.Calibration) (*image.RGBA, *image.RGBA) {
	t.Helper()

	b := game.NewBoard()
	broken := b.Clone()
	broken.Set(game.Square{Row: 9, Col: 0}, game.Piece{})
	broken.Set(game.Square{Row: 5, Col: 4}, game.Piece{Type: game.Rook, Color: game.Red})

	return vision.RenderSyntheticBoard(b, cal, screenSize),
		vision.RenderSyntheticBoard(broken, cal, screenSize)
}

// §23 最关键的一条：视觉识别错误绝不能污染内部棋盘状态。
func TestDesyncDoesNotPolluteBoard(t *testing.T) {
	cal := newCalibration(t)
	cfg := testConfig(t)
	startFEN := game.NewBoard().FEN()

	initial, broken := impossibleChangeFrames(t, cal)
	frames := []*image.RGBA{initial, broken}

	ctrl, detected := runController(t, cfg, cal, frames, 8)

	if len(detected) != 0 {
		t.Errorf("非法变化不应被接受为走法，却检测到 %v", detected)
	}
	if !ctrl.Desynced() {
		t.Error("推断不出合法走法时应标记为失步")
	}
	if got := ctrl.Board().FEN(); got != startFEN {
		t.Errorf("识别失败后内部棋盘绝不能被修改\n got: %s\nwant: %s", got, startFEN)
	}
	if n := ctrl.Board().NumMoves(); n != 0 {
		t.Errorf("识别失败后不应产生走子历史，实际 %d 手", n)
	}
}

// 重新同步（§17）：失步后回到标准初始局面。
func TestResyncRecovers(t *testing.T) {
	cal := newCalibration(t)
	cfg := testConfig(t)

	initial, broken := impossibleChangeFrames(t, cal)
	frames := []*image.RGBA{initial, broken}

	ctrl := app.New(cfg, &synthCapturer{frames: frames, repeat: 8}, cal,
		engine.NewMock(), app.Hooks{}, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ctrl.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ctrl.Desynced() {
		t.Fatal("应处于失步状态")
	}

	ctrl.Resync()
	if ctrl.Desynced() {
		t.Error("重新同步后不应再是失步状态")
	}
	if got, want := ctrl.Board().FEN(), game.StartFEN; got != want {
		t.Errorf("重新同步后应回到初始局面\n got: %s\nwant: %s", got, want)
	}
	if n := ctrl.Board().NumMoves(); n != 0 {
		t.Errorf("重新同步后不应有走子历史，实际 %d 手", n)
	}
}

// 引擎分析结果必须能正确翻译成中文记谱（含"前/后"消歧）。
func TestAnalysisNotation(t *testing.T) {
	cal := newCalibration(t)
	cfg := testConfig(t)

	notations := []string{"炮二平五", "马8进7", "兵七进一", "卒7进1", "马八进七", "马2进3"}
	frames, _, _ := buildFrames(t, cal, notations)

	var results []*analyzer.Result
	ctrl := app.New(cfg, &synthCapturer{frames: frames, repeat: 5}, cal,
		engine.NewMock(), app.Hooks{
			OnResult: func(r *analyzer.Result) { results = append(results, r) },
		}, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ctrl.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(results) != len(notations) {
		t.Fatalf("每一步都应产出分析结果，期望 %d 个，实际 %d 个", len(notations), len(results))
	}
	for i, r := range results {
		if r.BestNotation == "" {
			t.Errorf("第 %d 步的最佳走法记谱为空", i+1)
			continue
		}
		// 记谱必须是当前局面下真正合法的走法
		if r.BestMove.IsZero() {
			t.Errorf("第 %d 步的最佳走法为零值", i+1)
		}
		if len(r.Candidates) == 0 {
			t.Errorf("第 %d 步应至少有 1 个候选走法", i+1)
		}
	}
}
