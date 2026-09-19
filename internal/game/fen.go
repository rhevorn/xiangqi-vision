package game

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// StartFEN 是中国象棋标准初始局面的 FEN（红方先行）。
//
// 行序自上而下：第 1 行是黑方底线，第 10 行是红方底线。
const StartFEN = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

// FEN 返回当前局面的 FEN 字符串。
//
// 视觉层永远不要自己拼 FEN：FEN 只能由已确认的棋盘状态导出（§12）。
func (b *Board) FEN() string {
	var sb strings.Builder
	for r := 0; r < Rows; r++ {
		if r > 0 {
			sb.WriteByte('/')
		}
		empty := 0
		for c := 0; c < Cols; c++ {
			p := b.cells[r][c]
			if p.IsEmpty() {
				empty++
				continue
			}
			if empty > 0 {
				sb.WriteString(strconv.Itoa(empty))
				empty = 0
			}
			sb.WriteByte(p.FENChar())
		}
		if empty > 0 {
			sb.WriteString(strconv.Itoa(empty))
		}
	}
	halfmove := b.halfmove
	fullmove := b.fullmove
	if fullmove < 1 {
		fullmove = 1
	}
	return fmt.Sprintf("%s %c - - %d %d", sb.String(), b.side.FENChar(), halfmove, fullmove)
}

// ParseFEN 解析中国象棋 FEN。行棋方与回合数字段可省略。
func ParseFEN(fen string) (*Board, error) {
	fields := strings.Fields(strings.TrimSpace(fen))
	if len(fields) == 0 {
		return nil, errors.New("FEN 为空")
	}

	rows := strings.Split(fields[0], "/")
	if len(rows) != Rows {
		return nil, fmt.Errorf("FEN 应有 %d 行，实际 %d 行", Rows, len(rows))
	}

	b := &Board{}
	for r, row := range rows {
		col := 0
		for i := 0; i < len(row); i++ {
			ch := row[i]
			if ch >= '1' && ch <= '9' {
				col += int(ch - '0')
				continue
			}
			p, ok := fenToPiece[ch]
			if !ok {
				return nil, fmt.Errorf("FEN 第 %d 行含未知字符 %q", r+1, string(ch))
			}
			if col >= Cols {
				return nil, fmt.Errorf("FEN 第 %d 行超过 %d 列", r+1, Cols)
			}
			b.cells[r][col] = p
			col++
		}
		if col != Cols {
			return nil, fmt.Errorf("FEN 第 %d 行共 %d 列，应为 %d 列", r+1, col, Cols)
		}
	}

	b.side = Red
	if len(fields) > 1 {
		switch fields[1] {
		case "w", "W", "r", "R":
			b.side = Red
		case "b", "B":
			b.side = Black
		default:
			return nil, fmt.Errorf("FEN 行棋方 %q 无法识别（应为 w 或 b）", fields[1])
		}
	}
	if len(fields) > 4 {
		if n, err := strconv.Atoi(fields[4]); err == nil && n >= 0 {
			b.halfmove = n
		}
	}
	if len(fields) > 5 {
		if n, err := strconv.Atoi(fields[5]); err == nil && n >= 1 {
			b.fullmove = n
		}
	}
	if b.fullmove == 0 {
		b.fullmove = 1
	}

	// 记住起始局面，供 PositionCommand 重建重复局面历史。
	b.start = b.FEN()
	return b, nil
}

// MustParseFEN 与 ParseFEN 相同，但解析失败时 panic，用于确定合法的常量局面。
func MustParseFEN(fen string) *Board {
	b, err := ParseFEN(fen)
	if err != nil {
		panic("game: 非法 FEN: " + err.Error())
	}
	return b
}
