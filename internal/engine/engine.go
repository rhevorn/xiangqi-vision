// Package engine 封装 UCI 象棋引擎（Pikafish），把局面送进去、把分析拿回来。
//
// 引擎作为独立子进程运行，通过 stdin/stdout 用 UCI 协议通信。
package engine

import (
	"context"
	"fmt"
	"time"

	"xiangqi-vision/internal/game"
)

// ErrNoLegalMove 表示引擎在当前局面下无棋可走（已被将死或困毙）。
var ErrNoLegalMove = fmt.Errorf("当前局面无合法走法")

// Score 是引擎给出的局面评分。
//
// 注意：与 UCI 协议一致，评分以**当前行棋方**的视角给出，正数表示行棋方占优。
type Score struct {
	// CP 为厘兵（centipawn）评分，仅在不是杀棋时有意义。
	CP int
	// Mate 为杀棋步数：正数表示行棋方 N 步内取胜，负数表示 N 步内被将死。
	Mate int
	// IsMate 报告该评分是否为杀棋分。
	IsMate bool
}

// String 返回便于阅读的评分，如 "+0.83"、"-1.20"、"杀 3"、"被杀 2"。
func (s Score) String() string {
	if s.IsMate {
		if s.Mate >= 0 {
			return fmt.Sprintf("杀 %d", s.Mate)
		}
		return fmt.Sprintf("被杀 %d", -s.Mate)
	}
	return fmt.Sprintf("%+.2f", float64(s.CP)/100)
}

// Candidate 是一条候选走法及其后续变化。
type Candidate struct {
	Move  game.Move
	Score Score
	Depth int
	// PV 为主要变化（principal variation），第一手即 Move。
	PV []game.Move
}

// Analysis 是一次引擎分析的完整结果。
type Analysis struct {
	BestMove   game.Move
	Ponder     game.Move
	HasPonder  bool
	Score      Score
	Depth      int
	SelDepth   int
	Nodes      int64
	NPS        int64
	Elapsed    time.Duration
	Candidates []Candidate
}

// Best 返回首选候选，没有候选时返回 false。
func (a *Analysis) Best() (Candidate, bool) {
	if a == nil || len(a.Candidates) == 0 {
		return Candidate{}, false
	}
	return a.Candidates[0], true
}

// AnalyzeOptions 控制一次分析。
type AnalyzeOptions struct {
	// MoveTime 为思考时间预算，优先级最高。为 0 时改用 Depth。
	MoveTime time.Duration
	// Depth 为搜索深度，仅在 MoveTime 为 0 时生效。
	Depth int
	// MultiPV 为候选走法数量，0 表示使用引擎默认配置。
	MultiPV int
	// OnUpdate 在每一次取得更深的搜索结果时被调用，用于渐进式展示。
	// 它可能在 Analyze 返回之前被调用多次，实现方需要自己保证线程安全。
	OnUpdate func(*Analysis)
}

// Engine 是象棋引擎的抽象。除真实的 Pikafish 外，还有供测试使用的 Mock 引擎。
type Engine interface {
	// Analyze 分析给定局面。返回的评分以 b.SideToMove() 的视角给出。
	// 局面无合法走法时返回 ErrNoLegalMove。
	Analyze(ctx context.Context, b *game.Board, opts AnalyzeOptions) (*Analysis, error)
	// Name 返回引擎标识，用于日志。
	Name() string
	// Close 关闭引擎进程。
	Close() error
}
