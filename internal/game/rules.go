package game

// 本节实现中国象棋完整走法规则（《技术方案》§11）。
//
// 分工：视觉层负责"猜"走法，规则层负责"确认"走法。所有候选走法都必须先
// 通过 IsLegal，才允许更新棋盘。

type delta struct{ dr, dc int }

var (
	orthoDeltas = [4]delta{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	diagDeltas  = [4]delta{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}}

	// horseDeltas 的 leg 为"马腿"偏移：走 2 格方向上的相邻格。
	horseDeltas = [8]struct {
		dr, dc       int
		legDr, legDc int
	}{
		{-2, -1, -1, 0}, {-2, 1, -1, 0},
		{2, -1, 1, 0}, {2, 1, 1, 0},
		{-1, -2, 0, -1}, {1, -2, 0, -1},
		{-1, 2, 0, 1}, {1, 2, 0, 1},
	}
)

// inPalace 报告 sq 是否在 c 方九宫内。
func inPalace(sq Square, c Color) bool {
	if sq.Col < 3 || sq.Col > 5 {
		return false
	}
	if c == Red {
		return sq.Row >= 7 // 红方九宫 Row 7-9
	}
	return sq.Row >= 0 && sq.Row <= 2 // 黑方九宫 Row 0-2
}

// ownHalf 报告 sq 是否仍在 c 方自己的一半棋盘（未过河）。
func ownHalf(sq Square, c Color) bool {
	if c == Red {
		return sq.Row >= 5
	}
	return sq.Row <= 4
}

// crossedRiver 报告 c 方的棋子位于 sq 时是否已过河。
func crossedRiver(sq Square, c Color) bool { return !ownHalf(sq, c) }

// addIfOk 在目标格合法且非己方棋子时追加走法。
func (b *Board) addIfOk(out []Move, from, to Square, c Color) []Move {
	if !to.Valid() {
		return out
	}
	dst := b.cells[to.Row][to.Col]
	if !dst.IsEmpty() && dst.Color == c {
		return out
	}
	return append(out, Move{From: from, To: to})
}

// PseudoMoves 返回 from 处棋子的全部伪合法走法：满足棋子自身走法规则，
// 但尚未校验走后是否自将或形成将帅照面。
func (b *Board) PseudoMoves(from Square) []Move {
	p := b.At(from)
	if p.IsEmpty() {
		return nil
	}
	switch p.Type {
	case King:
		return b.kingMoves(from, p.Color)
	case Advisor:
		return b.advisorMoves(from, p.Color)
	case Elephant:
		return b.elephantMoves(from, p.Color)
	case Horse:
		return b.horseMoves(from, p.Color)
	case Rook:
		return b.rookMoves(from, p.Color)
	case Cannon:
		return b.cannonMoves(from, p.Color)
	case Pawn:
		return b.pawnMoves(from, p.Color)
	}
	return nil
}

// 将/帅：九宫内直行一步。
func (b *Board) kingMoves(from Square, c Color) []Move {
	var out []Move
	for _, d := range orthoDeltas {
		to := Square{from.Row + d.dr, from.Col + d.dc}
		if !inPalace(to, c) {
			continue
		}
		out = b.addIfOk(out, from, to, c)
	}
	return out
}

// 士/仕：九宫内斜行一步。
func (b *Board) advisorMoves(from Square, c Color) []Move {
	var out []Move
	for _, d := range diagDeltas {
		to := Square{from.Row + d.dr, from.Col + d.dc}
		if !inPalace(to, c) {
			continue
		}
		out = b.addIfOk(out, from, to, c)
	}
	return out
}

// 象/相：斜行两格，不可过河，象眼不可被占（塞象眼）。
func (b *Board) elephantMoves(from Square, c Color) []Move {
	var out []Move
	for _, d := range diagDeltas {
		to := Square{from.Row + 2*d.dr, from.Col + 2*d.dc}
		if !to.Valid() || !ownHalf(to, c) {
			continue
		}
		eye := Square{from.Row + d.dr, from.Col + d.dc} // 象眼
		if !b.At(eye).IsEmpty() {
			continue
		}
		out = b.addIfOk(out, from, to, c)
	}
	return out
}

// 马：走"日"字，蹩马腿时不可行。
func (b *Board) horseMoves(from Square, c Color) []Move {
	var out []Move
	for _, d := range horseDeltas {
		to := Square{from.Row + d.dr, from.Col + d.dc}
		if !to.Valid() {
			continue
		}
		leg := Square{from.Row + d.legDr, from.Col + d.legDc}
		if !b.At(leg).IsEmpty() { // 蹩马腿
			continue
		}
		out = b.addIfOk(out, from, to, c)
	}
	return out
}

// 车：直线滑行，遇子止步，可吃对方棋子。
func (b *Board) rookMoves(from Square, c Color) []Move {
	var out []Move
	for _, d := range orthoDeltas {
		for step := 1; ; step++ {
			to := Square{from.Row + step*d.dr, from.Col + step*d.dc}
			if !to.Valid() {
				break
			}
			dst := b.cells[to.Row][to.Col]
			if dst.IsEmpty() {
				out = append(out, Move{From: from, To: to})
				continue
			}
			if dst.Color != c {
				out = append(out, Move{From: from, To: to})
			}
			break
		}
	}
	return out
}

// 炮：不吃子时同车；吃子必须隔且仅隔一个棋子（炮架）。
func (b *Board) cannonMoves(from Square, c Color) []Move {
	var out []Move
	for _, d := range orthoDeltas {
		to := from
		overScreen := false
		for {
			to = Square{to.Row + d.dr, to.Col + d.dc}
			if !to.Valid() {
				break
			}
			dst := b.cells[to.Row][to.Col]

			if !overScreen {
				if dst.IsEmpty() {
					out = append(out, Move{From: from, To: to})
					continue
				}
				overScreen = true // 这枚棋子成为炮架
				continue
			}
			// 已越过炮架：只能吃炮架之后的第一个棋子
			if dst.IsEmpty() {
				continue
			}
			if dst.Color != c {
				out = append(out, Move{From: from, To: to})
			}
			break
		}
	}
	return out
}

// 兵/卒：向前一步；过河后可左右横走一步，不可后退。
func (b *Board) pawnMoves(from Square, c Color) []Move {
	var out []Move
	out = b.addIfOk(out, from, Square{from.Row + c.forward(), from.Col}, c)
	if crossedRiver(from, c) {
		out = b.addIfOk(out, from, Square{from.Row, from.Col - 1}, c)
		out = b.addIfOk(out, from, Square{from.Row, from.Col + 1}, c)
	}
	return out
}

// FindKing 返回 c 方将/帅所在位置，找不到时返回非法坐标。
func (b *Board) FindKing(c Color) Square {
	for r := 0; r < Rows; r++ {
		for col := 0; col < Cols; col++ {
			p := b.cells[r][col]
			if p.Type == King && p.Color == c {
				return Square{Row: r, Col: col}
			}
		}
	}
	return Square{Row: -1, Col: -1}
}

// isAttacked 报告 sq 是否被 by 方任一棋子攻击（含吃子走法）。
func (b *Board) isAttacked(sq Square, by Color) bool {
	for r := 0; r < Rows; r++ {
		for col := 0; col < Cols; col++ {
			from := Square{Row: r, Col: col}
			p := b.cells[r][col]
			if p.IsEmpty() || p.Color != by {
				continue
			}
			for _, m := range b.PseudoMoves(from) {
				if m.To == sq {
					return true
				}
			}
		}
	}
	return false
}

// KingsFacing 报告双方将帅是否在同一纵线上直接照面（中间无子）。
// 中国象棋中这是非法局面，走成照面的一方等同于送将。
func (b *Board) KingsFacing() bool {
	rk := b.FindKing(Red)
	bk := b.FindKing(Black)
	if !rk.Valid() || !bk.Valid() || rk.Col != bk.Col {
		return false
	}
	lo, hi := rk.Row, bk.Row
	if lo > hi {
		lo, hi = hi, lo
	}
	for r := lo + 1; r < hi; r++ {
		if !b.cells[r][rk.Col].IsEmpty() {
			return false
		}
	}
	return true
}

// InCheck 报告 c 方是否正被将军（含将帅照面）。
func (b *Board) InCheck(c Color) bool {
	k := b.FindKing(c)
	if !k.Valid() {
		return true // 将帅不在盘上，视为已被将死
	}
	return b.isAttacked(k, c.Opposite()) || b.KingsFacing()
}

// isLegalFor 报告 m 是否为 c 方的合法走法。
func (b *Board) isLegalFor(c Color, m Move) bool {
	p := b.At(m.From)
	if p.IsEmpty() || p.Color != c || !m.To.Valid() || m.From == m.To {
		return false
	}
	dst := b.At(m.To)
	if !dst.IsEmpty() && dst.Color == c {
		return false // 不能吃己方棋子
	}
	if !slicesContains(b.PseudoMoves(m.From), m.To) {
		return false
	}
	// 走子后不能自将，也不能形成将帅照面
	nb := b.cloneSurface()
	nb.applyUnchecked(m)
	return !nb.InCheck(c)
}

// IsLegal 报告 m 是否为当前行棋方的合法走法。
func (b *Board) IsLegal(m Move) bool { return b.isLegalFor(b.side, m) }

// LegalMoves 返回 c 方的全部合法走法。
func (b *Board) LegalMoves(c Color) []Move {
	var out []Move
	for r := 0; r < Rows; r++ {
		for col := 0; col < Cols; col++ {
			from := Square{Row: r, Col: col}
			p := b.cells[r][col]
			if p.IsEmpty() || p.Color != c {
				continue
			}
			for _, m := range b.PseudoMoves(from) {
				if b.isLegalFor(c, m) {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// Status 表示对局状态。
type Status int

const (
	Ongoing Status = iota
	RedWins
	BlackWins
	Draw
)

func (s Status) String() string {
	switch s {
	case RedWins:
		return "红方胜"
	case BlackWins:
		return "黑方胜"
	case Draw:
		return "和棋"
	default:
		return "进行中"
	}
}

// Status 返回当前对局状态。
//
// 注意中国象棋与国际象棋不同：困毙（无子可动但未被将军）同样判负。
func (b *Board) Status() Status {
	if !b.FindKing(Red).Valid() {
		return BlackWins
	}
	if !b.FindKing(Black).Valid() {
		return RedWins
	}
	if len(b.LegalMoves(b.side)) > 0 {
		return Ongoing
	}
	if b.side == Red {
		return BlackWins
	}
	return RedWins
}

// IsCheckmate 报告 c 方是否被将死。
func (b *Board) IsCheckmate(c Color) bool {
	return b.InCheck(c) && len(b.LegalMoves(c)) == 0
}

// IsStalemate 报告 c 方是否被困毙（无子可动但未被将军）。
func (b *Board) IsStalemate(c Color) bool {
	return !b.InCheck(c) && len(b.LegalMoves(c)) == 0
}

// slicesContains 报告 sq 是否出现在走法列表的目标格中。
func slicesContains(moves []Move, sq Square) bool {
	for _, m := range moves {
		if m.To == sq {
			return true
		}
	}
	return false
}
