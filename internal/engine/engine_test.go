package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xiangqi-vision/internal/game"
)

func TestParseInfoLine(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		check func(*testing.T, *infoLine)
	}{
		{
			name: "普通 cp 评分",
			line: "info depth 18 seldepth 33 multipv 1 score cp 30 nodes 2201775 nps 6967642 hashfull 43 tbhits 0 time 316 pv b2e2 b9c7 b0c2",
			check: func(t *testing.T, in *infoLine) {
				if in.Depth != 18 || in.SelDepth != 33 || in.MultiPV != 1 {
					t.Errorf("深度解析错误: %+v", in)
				}
				if in.Score.IsMate || in.Score.CP != 30 {
					t.Errorf("评分解析错误: %+v", in.Score)
				}
				if in.Nodes != 2201775 || in.TimeMs != 316 {
					t.Errorf("节点/时间解析错误: %+v", in)
				}
				if len(in.PV) != 3 || in.PV[0].UCI() != "b2e2" {
					t.Errorf("PV 解析错误: %v", in.PV)
				}
			},
		},
		{
			name: "负分与 multipv 3",
			line: "info depth 22 seldepth 29 multipv 3 score cp -145 nodes 5592041 nps 6990051 time 800 pv c3c4 g6g5",
			check: func(t *testing.T, in *infoLine) {
				if in.MultiPV != 3 || in.Score.CP != -145 {
					t.Errorf("解析错误: %+v", in)
				}
			},
		},
		{
			name: "杀棋分",
			line: "info depth 30 multipv 1 score mate 4 nodes 100 time 50 pv e1a1",
			check: func(t *testing.T, in *infoLine) {
				if !in.Score.IsMate || in.Score.Mate != 4 {
					t.Errorf("杀棋解析错误: %+v", in.Score)
				}
			},
		},
		{
			name: "被将死的负杀棋分",
			line: "info depth 30 multipv 2 score mate -3 nodes 100 time 50 pv e1a1",
			check: func(t *testing.T, in *infoLine) {
				if !in.Score.IsMate || in.Score.Mate != -3 {
					t.Errorf("负杀棋解析错误: %+v", in.Score)
				}
			},
		},
		{
			name: "启动时的空 pv 行",
			line: "info depth 1 seldepth 0 multipv 1 score cp 0 nodes 0 nps 0 hashfull 0 tbhits 0 time 1 pv ",
			check: func(t *testing.T, in *infoLine) {
				if len(in.PV) != 0 {
					t.Errorf("空 pv 行不应解析出走法，实际 %v", in.PV)
				}
			},
		},
		{
			name: "带 string 的无关行",
			line: "info string NNUE evaluation using pikafish.nnue",
			check: func(t *testing.T, in *infoLine) {
				if len(in.PV) != 0 {
					t.Errorf("string 行不应解析出走法，实际 %v", in.PV)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseInfoLine(c.line)
			if got == nil {
				t.Fatal("parseInfoLine 返回 nil")
			}
			c.check(t, got)
		})
	}

	if parseInfoLine("uciok") != nil {
		t.Error("非 info 行应返回 nil")
	}
}

func TestParseBestMove(t *testing.T) {
	cases := []struct {
		line   string
		move   string
		ponder string
	}{
		{"bestmove c3c4 ponder g6g5", "c3c4", "g6g5"},
		{"bestmove a3a4", "a3a4", ""},
		{"bestmove (none)", "", ""},
	}
	for _, c := range cases {
		mv, pd := parseBestMove(c.line)
		if mv != c.move || pd != c.ponder {
			t.Errorf("parseBestMove(%q) = (%q, %q)，期望 (%q, %q)", c.line, mv, pd, c.move, c.ponder)
		}
	}
}

func TestGoCommand(t *testing.T) {
	cases := []struct {
		opts AnalyzeOptions
		want string
	}{
		{AnalyzeOptions{MoveTime: 500 * time.Millisecond}, "go movetime 500"},
		{AnalyzeOptions{Depth: 12}, "go depth 12"},
		// 时间预算优先于深度
		{AnalyzeOptions{MoveTime: time.Second, Depth: 12}, "go movetime 1000"},
		{AnalyzeOptions{}, "go movetime 500"},
	}
	for _, c := range cases {
		if got := goCommand(c.opts); got != c.want {
			t.Errorf("goCommand(%+v) = %q，期望 %q", c.opts, got, c.want)
		}
	}
}

func TestScoreString(t *testing.T) {
	cases := []struct {
		score Score
		want  string
	}{
		{Score{CP: 83}, "+0.83"},
		{Score{CP: -120}, "-1.20"},
		{Score{CP: 0}, "+0.00"},
		{Score{Mate: 3, IsMate: true}, "杀 3"},
		{Score{Mate: -2, IsMate: true}, "被杀 2"},
	}
	for _, c := range cases {
		if got := c.score.String(); got != c.want {
			t.Errorf("Score%+v.String() = %q，期望 %q", c.score, got, c.want)
		}
	}
}

func TestMockEngine(t *testing.T) {
	ctx := context.Background()
	m := NewMock()

	b := game.NewBoard()
	an, err := m.Analyze(ctx, b, AnalyzeOptions{MoveTime: time.Second, MultiPV: 3})
	if err != nil {
		t.Fatal(err)
	}
	if an.BestMove.IsZero() {
		t.Fatal("Mock 引擎应返回一步走法")
	}
	if !b.IsLegal(an.BestMove) {
		t.Errorf("Mock 引擎返回了非法走法 %s", an.BestMove)
	}
	if len(an.Candidates) != 3 {
		t.Errorf("MultiPV=3 应返回 3 个候选，实际 %d", len(an.Candidates))
	}
	if an.Candidates[0].Move != an.BestMove {
		t.Error("首选候选应与 BestMove 一致")
	}

	// 空局面应返回 ErrNoLegalMove
	mate := game.MustParseFEN("R3k4/R8/9/9/9/9/9/9/9/3K5 b - - 0 1")
	if _, err := m.Analyze(ctx, mate, AnalyzeOptions{}); !errors.Is(err, ErrNoLegalMove) {
		t.Errorf("无棋可走时应返回 ErrNoLegalMove，实际 %v", err)
	}

	// 已取消的 context 应直接返回错误
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.Analyze(cancelled, game.NewBoard(), AnalyzeOptions{}); err == nil {
		t.Error("已取消的 context 应返回错误")
	}
}

// findPikafish 定位本地 Pikafish 可执行文件，找不到就跳过测试。
func findPikafish(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../bin/pikafish",
		"../../third_party/Pikafish/src/pikafish",
	}
	// 也允许通过环境变量指定
	if p := os.Getenv("PIKAFISH_PATH"); p != "" {
		candidates = append([]string{p}, candidates...)
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			return abs
		}
	}
	t.Skip("未找到 pikafish 可执行文件，跳过真实引擎集成测试（可用 PIKAFISH_PATH 指定）")
	return ""
}

func TestPikafishIntegration(t *testing.T) {
	path := findPikafish(t)

	eng := NewUCIEngine(Options{Path: path, Threads: 2, HashMB: 32, MultiPV: 3})
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b := game.NewBoard()
	var updates int
	an, err := eng.Analyze(ctx, b, AnalyzeOptions{
		MoveTime: 400 * time.Millisecond,
		OnUpdate: func(*Analysis) { updates++ },
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if an.BestMove.IsZero() {
		t.Fatal("引擎未返回走法")
	}
	if !b.IsLegal(an.BestMove) {
		t.Errorf("引擎返回了非法走法 %s", an.BestMove)
	}
	if len(an.Candidates) != 3 {
		t.Errorf("MultiPV=3 应返回 3 个候选，实际 %d", len(an.Candidates))
	}
	if an.Candidates[0].Move != an.BestMove {
		t.Errorf("首选候选 %s 与 bestmove %s 不一致", an.Candidates[0].Move, an.BestMove)
	}
	if an.Depth < 8 {
		t.Errorf("400ms 内搜索深度只有 %d，似乎没有真正在搜索", an.Depth)
	}
	if len(an.Candidates[0].PV) < 2 {
		t.Errorf("PV 过短：%v", an.Candidates[0].PV)
	}
	if updates == 0 {
		t.Error("渐进式回调 OnUpdate 从未被调用")
	}

	// 引擎自报名称应能读到
	if name := eng.Name(); name == "" || name == filepath.Base(path) {
		t.Logf("引擎名称解析为 %q（可能仍在用路径兜底）", name)
	}

	// 复用同一个引擎进程再分析一次，验证会话状态没有串味
	b2 := game.NewBoard()
	m, err := game.ParseChinese(b2, "炮二平五")
	if err != nil {
		t.Fatal(err)
	}
	if err := b2.Apply(m); err != nil {
		t.Fatal(err)
	}
	an2, err := eng.Analyze(ctx, b2, AnalyzeOptions{MoveTime: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("第二次 Analyze: %v", err)
	}
	if !b2.IsLegal(an2.BestMove) {
		t.Errorf("第二次分析返回了非法走法 %s", an2.BestMove)
	}
}

func TestPikafishNoLegalMove(t *testing.T) {
	path := findPikafish(t)

	eng := NewUCIEngine(Options{Path: path, Threads: 1, HashMB: 16})
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 黑方已被将死
	mate := game.MustParseFEN("R3k4/R8/9/9/9/9/9/9/9/3K5 b - - 0 1")
	if _, err := eng.Analyze(ctx, mate, AnalyzeOptions{MoveTime: 100 * time.Millisecond}); !errors.Is(err, ErrNoLegalMove) {
		t.Errorf("将死局面应返回 ErrNoLegalMove，实际 %v", err)
	}
}
