package engine

import (
	"context"
	"sort"
	"sync"
	"time"

	"xiangqi-vision/internal/game"
)

// Mock 是一个无需外部进程的假引擎，用于单元测试与无引擎环境下的链路自检。
//
// 它不做真正的搜索：按"能吃到的子价值最大"挑一步棋，评分用简单的子力差。
// 结果完全确定，因此适合断言。
type Mock struct {
	name string

	mu    sync.Mutex
	calls int
}

// NewMock 创建一个 Mock 引擎。
func NewMock() *Mock { return &Mock{name: "MockEngine"} }

// Name 返回引擎名。
func (m *Mock) Name() string { return m.name }

// Calls 返回 Analyze 被调用的次数，便于测试断言"没有重复分析"。
func (m *Mock) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// Close 实现 Engine 接口。
func (m *Mock) Close() error { return nil }

// Analyze 挑选一步合法走法并给出基于子力的粗略评分。
func (m *Mock) Analyze(ctx context.Context, b *game.Board, opts AnalyzeOptions) (*Analysis, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.calls++
	m.mu.Unlock()

	moves := b.LegalMoves(b.SideToMove())
	if len(moves) == 0 {
		return &Analysis{}, ErrNoLegalMove
	}

	// 按"吃子收益"降序排列，收益相同时保持走法生成的稳定顺序。
	type scored struct {
		move game.Move
		gain int
	}
	rated := make([]scored, 0, len(moves))
	for _, mv := range moves {
		gain := 0
		if captured := b.At(mv.To); !captured.IsEmpty() {
			gain = PieceValue(captured.Type)
		}
		rated = append(rated, scored{move: mv, gain: gain})
	}
	sort.SliceStable(rated, func(i, j int) bool { return rated[i].gain > rated[j].gain })

	an := &Analysis{
		BestMove: rated[0].move,
		Depth:    opts.Depth,
		Nodes:    int64(len(moves)),
		Elapsed:  time.Millisecond,
	}
	if an.Depth == 0 {
		an.Depth = 1
	}

	limit := opts.MultiPV
	if limit <= 0 {
		limit = 1
	}
	if limit > len(rated) {
		limit = len(rated)
	}

	balance := MaterialBalance(b, b.SideToMove())
	for i := 0; i < limit; i++ {
		an.Candidates = append(an.Candidates, Candidate{
			Move:  rated[i].move,
			Score: Score{CP: balance},
			Depth: an.Depth,
			PV:    []game.Move{rated[i].move},
		})
	}
	an.Score = an.Candidates[0].Score

	if opts.OnUpdate != nil {
		opts.OnUpdate(an)
	}
	return an, nil
}

// 棋子基础价值（厘兵）。将/帅给一个极大的值，它本不该出现在吃子列表里。
var pieceValues = [game.NumPieceTypes + 1]int{
	game.NoPiece:  0,
	game.King:     100000,
	game.Advisor:  200,
	game.Elephant: 200,
	game.Horse:    400,
	game.Rook:     900,
	game.Cannon:   450,
	game.Pawn:     100,
}

// PieceValue 返回棋子的基础价值。
func PieceValue(t game.PieceType) int { return pieceValues[t] }

// MaterialBalance 返回以 c 方视角的子力差（厘兵）。
func MaterialBalance(b *game.Board, c game.Color) int {
	total := 0
	for r := 0; r < game.Rows; r++ {
		for col := 0; col < game.Cols; col++ {
			p := b.At(game.Square{Row: r, Col: col})
			if p.IsEmpty() || p.Type == game.King {
				continue
			}
			if p.Color == c {
				total += pieceValues[p.Type]
			} else {
				total -= pieceValues[p.Type]
			}
		}
	}
	return total
}
