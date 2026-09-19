package app

import (
	"xiangqi-vision/internal/analyzer"
	"xiangqi-vision/internal/game"
)

// Status 是控制器状态的一份完整快照，供 UI 渲染。
//
// 它按值携带棋盘与走子记录，可以安全地跨 goroutine 传递：UI 不必去读控制器
// 内部的可变状态，也就不会和主循环抢数据。主循环在每轮结束以及每次引擎有
// 新进展时推一份新快照出来。
type Status struct {
	// State 是状态机的当前状态。
	State State

	// Board 是内部维护的棋盘快照。
	Board [game.Rows][game.Cols]game.Piece
	// SideToMove 是当前行棋方。
	SideToMove game.Color
	// FEN 是当前局面。
	FEN string
	// Result 是对局状态（进行中 / 红方胜 / 黑方胜）。
	Result game.Status

	// Moves 是自开局以来的走子记录（中文记谱）。
	Moves []string
	// LastMove、HasLast 描述最近一步走法。
	LastMove game.Move
	HasLast  bool
	// Detected 是最近一次从画面里认出来的走法记谱。
	Detected string

	// Desynced 表示内部棋盘可能与手机画面不一致，需要重新同步。
	Desynced bool
	// Paused 表示已暂停分析。
	Paused bool

	// Engine 是引擎名称。
	Engine string

	// Analysis 是最新一次分析结果；渐进式刷新期间它是中间结果。
	Analysis *analyzer.Result
	// AnalysisFinal 表示 Analysis 是否已经是该局面的最终结果。
	AnalysisFinal bool

	// Notice 是最近一条需要提示给用户的信息（例如识别失败）。
	Notice string
}

// At 返回棋盘上指定位置的棋子，越界返回空。
func (s *Status) At(row, col int) game.Piece {
	if row < 0 || row >= game.Rows || col < 0 || col >= game.Cols {
		return game.Piece{}
	}
	return s.Board[row][col]
}

// Clone 深拷贝一份快照，主要供需要长期持有或修改的调用方使用。
func (s *Status) Clone() *Status {
	out := *s
	out.Moves = append([]string(nil), s.Moves...)
	return &out
}
