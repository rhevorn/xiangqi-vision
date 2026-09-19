package game

import (
	"fmt"
	"slices"
	"strings"
)

// moveRecord 记录一步已执行的走法，用于悔棋与向引擎提供重复局面历史。
type moveRecord struct {
	move     Move
	captured Piece
	halfmove int
	fullmove int
}

// Board 是一局中国象棋的完整状态：90 个交叉点 + 行棋方 + 走子历史。
//
// Board 可以按值安全地深拷贝（见 Clone）。视觉层永远只通过 Apply 更新棋盘，
// 非法走法会被拒绝，从而保证"识别错误不污染内部棋盘状态"。
type Board struct {
	cells    [Rows][Cols]Piece
	side     Color
	start    string // 起始（或上次同步）局面的 FEN，用于重建引擎的重复局面历史
	history  []moveRecord
	halfmove int // 距上次吃子的半回合数
	fullmove int // 回合数
}

// NewBoard 返回标准初始局面，红方先行。
func NewBoard() *Board { return MustParseFEN(StartFEN) }

// At 返回指定交叉点的棋子。
func (b *Board) At(sq Square) Piece {
	if !sq.Valid() {
		return Piece{}
	}
	return b.cells[sq.Row][sq.Col]
}

// Set 直接放置棋子（主要用于构造测试局面与人工纠错）。
func (b *Board) Set(sq Square, p Piece) {
	if !sq.Valid() {
		return
	}
	b.cells[sq.Row][sq.Col] = p
}

// SideToMove 返回当前行棋方。
func (b *Board) SideToMove() Color { return b.side }

// Cells 返回棋盘的按值快照，供需要在别处长期持有或跨 goroutine 传递的调用方使用。
func (b *Board) Cells() [Rows][Cols]Piece { return b.cells }

// SetSideToMove 强制设置行棋方（人工纠错用）。
func (b *Board) SetSideToMove(c Color) { b.side = c }

// Clone 返回棋盘与历史的完整深拷贝。
func (b *Board) Clone() *Board {
	nb := &Board{
		cells:    b.cells,
		side:     b.side,
		start:    b.start,
		halfmove: b.halfmove,
		fullmove: b.fullmove,
	}
	nb.history = slices.Clone(b.history)
	return nb
}

// cloneSurface 只复制棋盘与行棋方，供走法合法性检查使用。
// 合法性检查不需要历史，这样可以避免 append 到共享底层数组。
func (b *Board) cloneSurface() *Board {
	return &Board{
		cells:    b.cells,
		side:     b.side,
		halfmove: b.halfmove,
		fullmove: b.fullmove,
	}
}

// Apply 校验并执行一步走法。非法走法返回错误，且棋盘保持不变。
func (b *Board) Apply(m Move) error {
	if !b.IsLegal(m) {
		return fmt.Errorf("非法走法 %s（当前行棋方：%s）", m, b.side)
	}
	b.applyUnchecked(m)
	return nil
}

// applyUnchecked 执行走法而不做任何校验。调用方必须已确认其合法性。
func (b *Board) applyUnchecked(m Move) {
	p := b.cells[m.From.Row][m.From.Col]
	captured := b.cells[m.To.Row][m.To.Col]

	b.history = append(b.history, moveRecord{
		move:     m,
		captured: captured,
		halfmove: b.halfmove,
		fullmove: b.fullmove,
	})

	b.cells[m.To.Row][m.To.Col] = p
	b.cells[m.From.Row][m.From.Col] = Piece{}

	if captured.IsEmpty() {
		b.halfmove++
	} else {
		b.halfmove = 0
	}
	if b.side == Black {
		b.fullmove++
	}
	b.side = b.side.Opposite()
}

// Undo 撤销最近一步走法，返回该走法。棋盘为空时返回 false。
func (b *Board) Undo() (Move, bool) {
	if len(b.history) == 0 {
		return Move{}, false
	}
	rec := b.history[len(b.history)-1]
	b.history = b.history[:len(b.history)-1]

	b.cells[rec.move.From.Row][rec.move.From.Col] = b.cells[rec.move.To.Row][rec.move.To.Col]
	b.cells[rec.move.To.Row][rec.move.To.Col] = rec.captured
	b.side = b.side.Opposite()
	b.halfmove = rec.halfmove
	b.fullmove = rec.fullmove
	return rec.move, true
}

// Moves 返回自局面载入以来已执行的走法。
func (b *Board) Moves() []Move {
	out := make([]Move, len(b.history))
	for i, rec := range b.history {
		out[i] = rec.move
	}
	return out
}

// LastMove 返回最近一步走法。
func (b *Board) LastMove() (Move, bool) {
	if len(b.history) == 0 {
		return Move{}, false
	}
	return b.history[len(b.history)-1].move, true
}

// NumMoves 返回已执行的走法数。
func (b *Board) NumMoves() int { return len(b.history) }

// LoadFEN 用给定局面重置棋盘，并清空走子历史。
func (b *Board) LoadFEN(fen string) error {
	nb, err := ParseFEN(fen)
	if err != nil {
		return err
	}
	*b = *nb
	return nil
}

// PositionCommand 返回可直接发给引擎的 UCI position 命令。
//
// 优先使用 "position fen <起始局面> moves <全部走法>" 的形式，这样 Pikafish
// 能看到完整对局历史，从而正确处理长将与重复局面；历史为空时退化为
// "position fen <当前局面>"。
func (b *Board) PositionCommand() string {
	if len(b.history) == 0 {
		return "position fen " + b.FEN()
	}
	var sb strings.Builder
	sb.WriteString("position fen ")
	sb.WriteString(b.start)
	sb.WriteString(" moves")
	for _, rec := range b.history {
		sb.WriteByte(' ')
		sb.WriteString(rec.move.UCI())
	}
	return sb.String()
}
