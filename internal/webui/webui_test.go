package webui_test

import (
	"bufio"
	"context"
	"encoding/json"
	"image"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xiangqi-vision/internal/capture"
	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/engine"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/vision"
	"xiangqi-vision/internal/webui"
)

// fakeCapturer 按顺序播放一批合成画面，每个重复若干次。
//
// 重复是必须的：稳定判定要连续几帧不变才认账，一帧一换的画面永远等不到稳定。
// 播完最后一帧后停在它上面，模拟真机一直有画面。
type fakeCapturer struct {
	frames []*image.RGBA
	repeat int

	idx, reps int
}

func (c *fakeCapturer) Capture(context.Context) (*image.RGBA, error) {
	if len(c.frames) == 0 {
		return nil, capture.ErrExhausted
	}
	img := c.frames[c.idx]
	c.reps++
	if c.reps >= c.repeat && c.idx < len(c.frames)-1 {
		c.reps, c.idx = 0, c.idx+1
	}
	return img, nil
}

func (c *fakeCapturer) Describe() string { return "测试用画面" }
func (c *fakeCapturer) Close() error     { return nil }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestEndpoints 起一个真实的服务，把各路接口都过一遍。
func TestEndpoints(t *testing.T) {
	cal, err := vision.NewCalibration(vision.Rect{X: 120, Y: 310, Width: 870, Height: 970}, 0)
	if err != nil {
		t.Fatal(err)
	}

	// 造两张画面：初始局面 → 走一步，让主循环能认出走子
	b := game.NewBoard()
	m, err := game.ParseChinese(b, "炮二平五")
	if err != nil {
		t.Fatal(err)
	}
	next := b.Clone()
	if err := next.Apply(m); err != nil {
		t.Fatal(err)
	}
	size := image.Pt(1080, 2400)
	frames := []*image.RGBA{
		vision.RenderSyntheticBoard(b, cal, size),
		vision.RenderSyntheticBoard(next, cal, size),
	}

	cfg := config.Default()
	cfg.Board = config.Board{X: 120, Y: 310, Width: 870, Height: 970}
	cfg.Capture.FPS = 100
	cfg.Engine.MoveTimeMS = 1
	cfg.Debug.Dir = t.TempDir()

	srv := webui.New(webui.Options{
		Cfg:      cfg,
		CfgPath:  filepath.Join(t.TempDir(), "config.yaml"),
		Capturer: &fakeCapturer{frames: frames, repeat: 6},
		Engine:   engine.NewMock(),
		Logger:   quiet(),
	})

	addr, err := srv.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("服务未能正常关闭")
		}
	}()

	if !srv.Calibrated() {
		t.Fatal("该配置应当算作已校准")
	}
	if err := srv.StartController(ctx); err != nil {
		t.Fatalf("StartController: %v", err)
	}

	base := "http://" + addr

	// 首页
	body := getBody(t, base+"/")
	if !strings.Contains(body, "中国象棋 AI 辅助") {
		t.Error("首页内容不对")
	}
	if !strings.Contains(body, "EventSource") {
		t.Error("首页应当通过 SSE 接收状态")
	}

	// 等主循环认出走子
	var st statusJSON
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		json.Unmarshal([]byte(getBody(t, base+"/api/status")), &st)
		if len(st.Moves) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(st.Moves) == 0 {
		t.Fatal("主循环没有认出任何走子")
	}
	if st.Moves[0] != "炮二平五" {
		t.Errorf("认出的走法是 %q，期望 炮二平五", st.Moves[0])
	}
	if st.Board[0][0].N != "车" || st.Board[0][0].R {
		t.Errorf("棋盘左上角应是黑车，实际 %+v", st.Board[0][0])
	}
	if st.Board[9][4].N != "帅" || !st.Board[9][4].R {
		t.Errorf("棋盘底部中央应是红帅，实际 %+v", st.Board[9][4])
	}
	if st.FEN == "" {
		t.Error("状态里应当带 FEN")
	}

	// 缩略图
	resp, err := http.Get(base + "/api/frame")
	if err != nil {
		t.Fatal(err)
	}
	thumb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("取缩略图返回 %d", resp.StatusCode)
	}
	if !strings.HasPrefix(string(thumb[1:4]), "PNG") {
		t.Error("缩略图不是 PNG")
	}
	// 缩略图应当明显小于原图
	if len(thumb) > 120*1024 {
		t.Errorf("缩略图偏大: %d 字节", len(thumb))
	}

	// SSE：连上后应当立刻收到一份当前状态
	sseCtx, sseCancel := context.WithTimeout(ctx, 5*time.Second)
	defer sseCancel()
	req, _ := http.NewRequestWithContext(sseCtx, http.MethodGet, base+"/events", nil)
	sseResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()
	if ct := sseResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("SSE 的 Content-Type 是 %q", ct)
	}
	sc := bufio.NewScanner(sseResp.Body)
	var dataLine string
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if dataLine == "" {
		t.Fatal("SSE 连接后没有立刻收到状态")
	}
	var first statusJSON
	if err := json.Unmarshal([]byte(dataLine), &first); err != nil {
		t.Fatalf("SSE 首帧不是合法 JSON: %v", err)
	}
	if len(first.Moves) == 0 {
		t.Error("SSE 首帧应当带着已有局面")
	}
}

// TestCalibrateRejectsBadRect 校准接口必须挡住越界与畸形的坐标。
func TestCalibrateRejectsBadRect(t *testing.T) {
	cal, _ := vision.NewCalibration(vision.Rect{X: 0, Y: 0, Width: 800, Height: 900}, 0)
	img := vision.RenderSyntheticBoard(game.NewBoard(), cal, image.Pt(1080, 2400))

	cases := []struct {
		name string
		body string
	}{
		{"超出画面", `{"x":900,"y":2000,"width":800,"height":900}`},
		{"尺寸为零", `{"x":100,"y":100,"width":0,"height":0}`},
		{"尺寸过小", `{"x":100,"y":100,"width":3,"height":3}`},
		{"不是 JSON", `这不是 json`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, "/api/calibrate", strings.NewReader(c.body))
			if _, err := webui.ParseCalibrationRect(req, img); err == nil {
				t.Errorf("%s 的坐标应当被拒绝", c.name)
			}
		})
	}

	// 反着点（先右下后左上）应当被规范化成正确的矩形
	req, _ := http.NewRequest(http.MethodPost, "/api/calibrate",
		strings.NewReader(`{"x":900,"y":1000,"width":-800,"height":-900}`))
	rect, err := webui.ParseCalibrationRect(req, img)
	if err != nil {
		t.Fatalf("反向拖拽应当被接受: %v", err)
	}
	if rect.X != 100 || rect.Y != 100 || rect.Width != 800 || rect.Height != 900 {
		t.Errorf("规范化结果不对: %+v", rect)
	}
}

// TestDownscale 验证降采样确实缩小了、且保留了画面结构。
func TestDownscale(t *testing.T) {
	cal, _ := vision.NewCalibration(vision.Rect{X: 120, Y: 310, Width: 870, Height: 970}, 0)
	src := vision.RenderSyntheticBoard(game.NewBoard(), cal, image.Pt(1080, 2400))

	out := webui.Downscale(src, 320)
	if out.Bounds().Dx() != 320 {
		t.Errorf("宽度 = %d，期望 320", out.Bounds().Dx())
	}
	// 等比缩放：1080x2400 → 320x711
	if got := out.Bounds().Dy(); got != 711 {
		t.Errorf("高度 = %d，期望 711（保持比例）", got)
	}

	// 不放大：比目标还小的图应原样返回
	small := webui.Downscale(src, 4000)
	if small.Bounds() != src.Bounds() {
		t.Errorf("不应对图片做放大: %v", small.Bounds())
	}

	// 棋盘区域应当仍然"有内容"：缩略图里不该是一整片纯色
	seen := map[uint32]bool{}
	for y := 0; y < out.Bounds().Dy(); y += 3 {
		for x := 0; x < out.Bounds().Dx(); x += 3 {
			seen[uint32(out.Pix[out.PixOffset(x, y)])] = true
		}
	}
	if len(seen) < 3 {
		t.Errorf("缩略图只有 %d 种灰度，画面结构可能丢了", len(seen))
	}
}

// statusJSON 是测试侧对服务端 JSON 的最小映射。
type statusJSON struct {
	State string       `json:"state"`
	Board [][]cellJSON `json:"board"`
	FEN   string       `json:"fen"`
	Moves []string     `json:"moves"`
}

type cellJSON struct {
	N string `json:"n"`
	R bool   `json:"r"`
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s 返回 %d: %s", url, resp.StatusCode, data)
	}
	return string(data)
}
