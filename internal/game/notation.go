package game

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// 中文记谱，即"炮二平五"这种格式。
//
// 格式：[前/后/中] 棋子名 起始纵线 动作 目标
//
//   - 纵线编号：红方用汉字"一~九"，自红方右侧数起（屏幕最右列是"一"）；
//     黑方用阿拉伯数字"1~9"，自黑方右侧数起（屏幕最左列是"1"）。
//   - 动作：平（横走）、进（向前）、退（向后）。
//   - 目标：车/炮/兵/将写行进步数；马/象/士写落点纵线编号。
//   - 同一纵线上有两枚同名棋子时，用"前/后/中"代替起始纵线编号。

var cnNumerals = [10]string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
var arNumerals = [10]string{"", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

// fileNum 返回 c 方视角下 col 列的纵线编号。
func fileNum(c Color, col int) int {
	if c == Red {
		return Cols - col // 红方从右往左数：col 8 → 一，col 0 → 九
	}
	return col + 1 // 黑方从左往右数：col 0 → 1，col 8 → 9
}

// numeral 返回 c 方的数字写法。
func numeral(c Color, n int) string {
	if n < 1 || n > 9 {
		return fmt.Sprint(n)
	}
	if c == Red {
		return cnNumerals[n]
	}
	return arNumerals[n]
}

// FormatChinese 返回走法的中文记谱，如"炮二平五"。
//
// b 必须是走这步棋之前的局面：记谱中的"前/后"与纵线编号都依赖走子前的站位。
func FormatChinese(b *Board, m Move) string {
	p := b.At(m.From)
	if p.IsEmpty() {
		return ""
	}
	c := p.Color

	var sb strings.Builder
	if prefix := disambiguation(b, m.From, p); prefix != "" {
		sb.WriteString(prefix)
		sb.WriteString(p.Name())
	} else {
		sb.WriteString(p.Name())
		sb.WriteString(numeral(c, fileNum(c, m.From.Col)))
	}

	if m.From.Row == m.To.Row {
		sb.WriteString("平")
		sb.WriteString(numeral(c, fileNum(c, m.To.Col)))
		return sb.String()
	}

	// 红方向上（行号减小）为"进"，黑方向下（行号增大）为"进"。
	if (m.To.Row-m.From.Row)*c.forward() > 0 {
		sb.WriteString("进")
	} else {
		sb.WriteString("退")
	}

	switch p.Type {
	case Horse, Elephant, Advisor:
		// 斜行棋子写落点纵线
		sb.WriteString(numeral(c, fileNum(c, m.To.Col)))
	default:
		// 直行棋子写步数
		sb.WriteString(numeral(c, abs(m.To.Row-m.From.Row)))
	}
	return sb.String()
}

// disambiguation 在同一纵线上有多枚同色同种棋子时返回"前/后/中"等前缀，
// 否则返回空串。
func disambiguation(b *Board, from Square, p Piece) string {
	var sameFile []Square
	for r := 0; r < Rows; r++ {
		sq := Square{Row: r, Col: from.Col}
		q := b.At(sq)
		if q.Type == p.Type && q.Color == p.Color {
			sameFile = append(sameFile, sq)
		}
	}
	if len(sameFile) < 2 {
		return ""
	}

	// 从"前"到"后"排序："前"指更靠近对方底线的那枚。
	sort.SliceStable(sameFile, func(i, j int) bool {
		if p.Color == Red {
			return sameFile[i].Row < sameFile[j].Row // 红方行号小者在更前方
		}
		return sameFile[i].Row > sameFile[j].Row
	})

	idx := -1
	for i, sq := range sameFile {
		if sq == from {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}

	switch len(sameFile) {
	case 2:
		if idx == 0 {
			return "前"
		}
		return "后"
	case 3:
		switch idx {
		case 0:
			return "前"
		case 1:
			return "中"
		default:
			return "后"
		}
	default:
		// 四枚及以上（多见于兵/卒）：用"一~五"自前向后编号
		return numeral(p.Color, idx+1)
	}
}

// ParseChinese 解析中文记谱并返回对应的合法走法。b 必须是走这步棋之前的局面。
//
// 实现上先枚举当前全部合法走法、逐条格式化成记谱再比对，因此记谱的生成与
// 解析共用同一套规则，不存在两边写岔的可能。
func ParseChinese(b *Board, s string) (Move, error) {
	side := b.SideToMove()
	want := normalizeNotation(side, s)
	if want == "" {
		return Move{}, fmt.Errorf("记谱为空")
	}
	for _, m := range b.LegalMoves(side) {
		if normalizeNotation(side, FormatChinese(b, m)) == want {
			return m, nil
		}
	}
	return Move{}, fmt.Errorf("无法解析记谱 %q（当前为%s方行棋）", s, side)
}

// normalizeNotation 把用户输入统一成记谱的标准写法，以便宽松比对：
// 去掉空白、统一异体字、把数字统一到当前行棋方的写法。
func normalizeNotation(c Color, s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))

	// 异体字统一
	s = strings.NewReplacer(
		"車", "车",
		"馬", "马",
		"砲", "炮",
		"礮", "炮",
		"帥", "帅",
		"將", "将",
	).Replace(s)

	// 红黑同义字统一到当前行棋方的字形
	if c == Red {
		s = strings.NewReplacer("士", "仕", "象", "相", "将", "帅", "卒", "兵").Replace(s)
		for i := 1; i <= 9; i++ {
			s = strings.ReplaceAll(s, arNumerals[i], cnNumerals[i])
		}
	} else {
		s = strings.NewReplacer("仕", "士", "相", "象", "帅", "将", "兵", "卒").Replace(s)
		for i := 1; i <= 9; i++ {
			s = strings.ReplaceAll(s, cnNumerals[i], arNumerals[i])
		}
	}
	return s
}
