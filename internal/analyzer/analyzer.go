// Package analyzer 把棋盘状态与引擎串起来，产出便于展示的分析结果。
//
// 它负责两件引擎本身不关心的事：把 UCI 走法翻译成中文记谱，以及把评分、
// 深度、变化图整理成 UI 可以直接渲染的结构。
package analyzer

import (
	"context"
	"time"

	"xiangqi-vision/internal/engine"
	"xiangqi-vision/internal/game"
)

// Options 是分析参数。
type Options struct {
	MoveTime time.Duration
	Depth    int
	MultiPV  int
}

// Candidate 是一条候选走法及其展示所需的信息。
type Candidate struct {
	Move     game.Move
	Notation string
	Score    engine.Score
	// PV 为后续变化的记谱，已经推演成中文记谱。
	PV []string
}

// Result 是一次分析的对外结果。
type Result struct {
	FEN        string
	SideToMove game.Color
	Status     game.Status

	BestMove     game.Move
	BestNotation string
	// BestFor 指明最佳走法是给哪一方走的。
	BestFor game.Color

	Score   engine.Score
	Depth   int
	Nodes   int64
	Elapsed time.Duration

	Candidates []Candidate
}

// Analyzer 封装引擎，把结果翻译成便于展示的形式。
type Analyzer struct {
	eng  engine.Engine
	opts Options
}

// New 创建分析器。
func New(eng engine.Engine, opts Options) *Analyzer {
	return &Analyzer{eng: eng, opts: opts}
}

// Engine 返回底层引擎。
func (a *Analyzer) Engine() engine.Engine { return a.eng }

// Analyze 分析棋盘当前局面。
//
// onProgress 会在引擎每搜到更深一层时被调用，可用于渐进式展示：
// 先给出一个快速答案，随后不断刷新。
func (a *Analyzer) Analyze(
	ctx context.Context,
	b *game.Board,
	onProgress func(*Result),
) (*Result, error) {
	an, err := a.eng.Analyze(ctx, b, engine.AnalyzeOptions{
		MoveTime: a.opts.MoveTime,
		Depth:    a.opts.Depth,
		MultiPV:  a.opts.MultiPV,
		OnUpdate: func(partial *engine.Analysis) {
			if onProgress != nil {
				onProgress(a.build(b, partial))
			}
		},
	})
	if err != nil {
		return nil, err
	}
	return a.build(b, an), nil
}

// build 把引擎结果翻译成展示结果。
//
// b 必须是分析前的局面：中文记谱依赖走子前的站位，PV 也需要沿着变化推演。
func (a *Analyzer) build(b *game.Board, an *engine.Analysis) *Result {
	res := &Result{
		FEN:        b.FEN(),
		SideToMove: b.SideToMove(),
		Status:     b.Status(),
		BestMove:   an.BestMove,
		BestFor:    b.SideToMove(),
		Score:      an.Score,
		Depth:      an.Depth,
		Nodes:      an.Nodes,
		Elapsed:    an.Elapsed,
	}
	if !an.BestMove.IsZero() {
		res.BestNotation = game.FormatChinese(b, an.BestMove)
	}

	for _, c := range an.Candidates {
		res.Candidates = append(res.Candidates, Candidate{
			Move:     c.Move,
			Notation: game.FormatChinese(b, c.Move),
			Score:    c.Score,
			PV:       FormatPV(b, c.PV),
		})
	}
	return res
}

// FormatPV 把一串 UCI 走法翻译成中文记谱。
//
// 记谱依赖走子前的站位（"前/后"与纵线编号都在变），所以必须沿着变化
// 逐手推演，不能独立翻译每一手。
func FormatPV(b *game.Board, moves []game.Move) []string {
	cur := b.Clone()
	out := make([]string, 0, len(moves))
	for _, m := range moves {
		if !cur.IsLegal(m) {
			break // 变化图与实际局面不符时停止，不要输出错误记谱
		}
		out = append(out, game.FormatChinese(cur, m))
		if err := cur.Apply(m); err != nil {
			break
		}
	}
	return out
}
