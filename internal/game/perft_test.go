package game

import "testing"

// perft 统计从 b 出发、深度 depth 内的合法走法序列总数。
//
// 它是走法生成器最严格的检验：任何一个棋子走法、将军判定或将帅照面规则
// 写错，节点数都会立刻对不上。
func perft(b *Board, depth int) int {
	if depth == 0 {
		return 1
	}
	moves := b.LegalMoves(b.SideToMove())
	if depth == 1 {
		return len(moves)
	}
	total := 0
	for _, m := range moves {
		nb := b.cloneSurface()
		nb.applyUnchecked(m)
		total += perft(nb, depth-1)
	}
	return total
}

// TestPerftMatchesPikafish 用 Pikafish 的 perft 结果作为标准答案，
// 交叉验证走法生成、将军判定与将帅照面规则。
//
// 基准值由 third_party/Pikafish 的 "go perft N" 命令得到，可用以下方式复现：
//
//	{ echo uci; echo isready; echo "position fen <FEN>"; echo "go perft 3"; } | ./pikafish
func TestPerftMatchesPikafish(t *testing.T) {
	cases := []struct {
		name string
		fen  string
		want []int // 下标即深度，want[1] 为深度 1 的节点数
	}{
		{
			name: "初始局面",
			fen:  StartFEN,
			want: []int{1, 44, 1920, 79666},
		},
		{
			name: "中炮对屏风马第 3 回合",
			fen:  "r1bakabr1/9/1cn3nc1/p1p1p1p1p/9/9/P1P1P1P1P/1C2C1N2/9/RNBAKABR1 w - - 6 4",
			want: []int{1, 37, 1292, 49161},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := MustParseFEN(tc.fen)
			for depth := 1; depth < len(tc.want); depth++ {
				if got := perft(b, depth); got != tc.want[depth] {
					t.Errorf("perft(%d) = %d，期望 %d", depth, got, tc.want[depth])
				}
			}
		})
	}
}

// TestPerftInitialDeep 在 -short 模式下跳过，用于偶尔做一次更深的回归。
func TestPerftInitialDeep(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过耗时的深度 perft")
	}
	if got := perft(NewBoard(), 4); got != 3290240 {
		t.Errorf("perft(4) = %d，期望 3290240", got)
	}
}

// TestOpeningSequenceMatchesPikafish 验证：按中文记谱走完 6 步开局后，
// 内部棋盘导出的 FEN 与 Pikafish 对同一走法序列给出的 FEN 完全一致。
//
// 这一条同时覆盖了中文记谱解析、合法性校验、走子执行与 FEN 生成四个环节。
func TestOpeningSequenceMatchesPikafish(t *testing.T) {
	const want = "r1bakabr1/9/1cn3nc1/p1p1p1p1p/9/9/P1P1P1P1P/1C2C1N2/9/RNBAKABR1 w - - 6 4"

	b := NewBoard()
	for _, notation := range []string{
		"炮二平五", // h2e2
		"马8进7", // h9g7
		"马二进三", // h0g2
		"车9平8", // i9h9
		"车一平二", // i0h0
		"马2进3", // b9c7
	} {
		m, err := ParseChinese(b, notation)
		if err != nil {
			t.Fatalf("ParseChinese(%q): %v", notation, err)
		}
		if err := b.Apply(m); err != nil {
			t.Fatalf("Apply(%s): %v", m, err)
		}
	}

	if got := b.FEN(); got != want {
		t.Errorf("6 步开局后 FEN 与 Pikafish 不一致\n got: %s\nwant: %s", got, want)
	}

	// 同样的走法序列，引擎收到的 position 命令应携带完整历史
	wantCmd := "position fen " + StartFEN + " moves h2e2 h9g7 h0g2 i9h9 i0h0 b9c7"
	if got := b.PositionCommand(); got != wantCmd {
		t.Errorf("PositionCommand()\n got: %s\nwant: %s", got, wantCmd)
	}
}
