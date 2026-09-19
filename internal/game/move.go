package game

import (
	"fmt"
	"strings"
)

// Square 棋盘上的一个交叉点。
type Square struct {
	Row int // 0..9，0 为黑方底线
	Col int // 0..8
}

// Valid 报告坐标是否落在棋盘内。
func (s Square) Valid() bool {
	return s.Row >= 0 && s.Row < Rows && s.Col >= 0 && s.Col < Cols
}

// String 返回用于日志与显示的坐标，如 "H2"。
//
// 字母为列（a-i → A-I），数字为行号且以红方底线为 0，即 ICCS 记法。
// 与《技术方案》§7/§20 中 "H3"、"E3" 的写法保持一致。
func (s Square) String() string {
	if !s.Valid() {
		return "??"
	}
	return string(rune('A'+s.Col)) + fmt.Sprint(9-s.Row)
}

// UCI 返回 Pikafish 使用的小写坐标，如 "h2"。
func (s Square) UCI() string {
	if !s.Valid() {
		return "??"
	}
	return string(rune('a'+s.Col)) + fmt.Sprint(9-s.Row)
}

// ParseSquare 解析 "h2"、"H2" 形式的坐标。
func ParseSquare(s string) (Square, error) {
	s = strings.TrimSpace(s)
	if len(s) != 2 {
		return Square{}, fmt.Errorf("坐标 %q 格式错误，应为字母+数字，如 h2", s)
	}
	c := s[0]
	if c >= 'A' && c <= 'Z' {
		c += 'a' - 'A'
	}
	if c < 'a' || c > 'a'+Cols-1 {
		return Square{}, fmt.Errorf("坐标 %q 的列超出范围（a-%c）", s, rune('a'+Cols-1))
	}
	d := s[1]
	if d < '0' || d > '0'+Rows-1 {
		return Square{}, fmt.Errorf("坐标 %q 的行超出范围（0-%d）", s, Rows-1)
	}
	return Square{Row: Rows - 1 - int(d-'0'), Col: int(c - 'a')}, nil
}

// Move 一步走法。
type Move struct {
	From Square
	To   Square
}

// UCI 返回引擎使用的走法串，如 "h2e2"。
func (m Move) UCI() string { return m.From.UCI() + m.To.UCI() }

// String 返回紧凑的可读形式，如 "H2-E2"。
func (m Move) String() string { return m.From.String() + "-" + m.To.String() }

// IsZero 报告是否为零值走法。
func (m Move) IsZero() bool { return m == Move{} }

// ParseMove 解析 "h2e2"、"h2-e2"、"H2 E2" 等形式的走法。
func ParseMove(s string) (Move, error) {
	s = strings.NewReplacer("-", "", ">", "", " ", "", "\t", "").Replace(strings.TrimSpace(s))
	if len(s) != 4 {
		return Move{}, fmt.Errorf("走法 %q 格式错误，应为 4 个字符，如 h2e2", s)
	}
	from, err := ParseSquare(s[:2])
	if err != nil {
		return Move{}, err
	}
	to, err := ParseSquare(s[2:])
	if err != nil {
		return Move{}, err
	}
	return Move{From: from, To: to}, nil
}

// abs 返回整数绝对值。
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
