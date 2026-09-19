// Package overlay 负责终端展示（《技术方案》§15）。
//
// 第一版 UI 是 CLI/TUI（§2），这里实现棋盘的文本渲染与分析结果的排版。
// 后续要换成 Wails 或原生 macOS 浮层时，这一层可以整体替换。
package overlay

import (
	"fmt"
	"strings"

	"xiangqi-vision/internal/analyzer"
	"xiangqi-vision/internal/game"
)

// 终端 ANSI 样式。
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiRed   = "\x1b[31m"
	ansiCyan  = "\x1b[36m"
	ansiGreen = "\x1b[32m"
	ansiYell  = "\x1b[33m"
)

// Options 控制渲染样式。
type Options struct {
	// Color 打开 ANSI 颜色。
	Color bool
	// Highlight 是需要高亮（框出）的格点，通常是最近一步的起止点。
	Highlight []game.Square
}

// RenderBoard 把棋盘渲染成适合终端显示的文本。
//
// 每个格点固定占 3 个显示列：汉字宽 2 列 + 1 空格，空点则用 1 列的点加 2 空格，
// 这样中英文混排也能对齐。
func RenderBoard(b *game.Board, opts Options) string {
	var sb strings.Builder

	sb.WriteString("    ")
	for c := 0; c < game.Cols; c++ {
		fmt.Fprintf(&sb, "%-3s", string(rune('a'+c)))
	}
	sb.WriteString("\n")

	highlight := map[game.Square]bool{}
	for _, sq := range opts.Highlight {
		highlight[sq] = true
	}

	for r := 0; r < game.Rows; r++ {
		fmt.Fprintf(&sb, "%2d  ", 9-r)
		for c := 0; c < game.Cols; c++ {
			sq := game.Square{Row: r, Col: c}
			sb.WriteString(cellText(b.At(sq), highlight[sq], opts.Color))
		}
		fmt.Fprintf(&sb, " %d", 9-r)
		sb.WriteString("\n")

		// 河界
		if r == 4 {
			sb.WriteString("    ")
			if opts.Color {
				sb.WriteString(ansiDim)
			}
			sb.WriteString("楚 河          汉 界")
			if opts.Color {
				sb.WriteString(ansiReset)
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("    ")
	for c := 0; c < game.Cols; c++ {
		fmt.Fprintf(&sb, "%-3s", string(rune('a'+c)))
	}
	sb.WriteString("\n")
	return sb.String()
}

// cellText 返回一个格点的固定宽度文本。
func cellText(p game.Piece, highlighted, color bool) string {
	if p.IsEmpty() {
		if highlighted {
			if color {
				return ansiYell + "×" + ansiReset + "  "
			}
			return "×  "
		}
		return "·  "
	}

	name := p.Name()
	if !color {
		if highlighted {
			return "[" + name + "]"
		}
		return name + " "
	}

	c := ansiRed
	if p.Color == game.Black {
		c = ansiCyan
	}
	if highlighted {
		return ansiBold + ansiYell + "[" + ansiReset + c + name + ansiReset + ansiBold + ansiYell + "]" + ansiReset
	}
	return c + name + ansiReset + " "
}

// RenderAnalysis 按《技术方案》§15 的样式排版分析结果。
func RenderAnalysis(res *analyzer.Result, opts Options) string {
	var sb strings.Builder

	side := res.BestFor.String()
	fmt.Fprintf(&sb, "%s轮到 %s方走棋%s\n", dim(opts.Color), side, reset(opts.Color))
	sb.WriteString(separator(opts.Color))

	// 最佳走法
	if res.BestNotation != "" {
		fmt.Fprintf(&sb, "%s最佳走法%s  %s%s%s\n",
			dim(opts.Color), reset(opts.Color),
			bold(opts.Color), res.BestNotation, reset(opts.Color))
	} else {
		sb.WriteString("（无合法走法）\n")
	}
	fmt.Fprintf(&sb, "%s局面评分%s  %s\n", dim(opts.Color), reset(opts.Color), scoreText(res.Score, opts.Color))

	// 搜索信息
	fmt.Fprintf(&sb, "%s搜索信息%s  深度 %d  用时 %s  节点 %s\n",
		dim(opts.Color), reset(opts.Color), res.Depth, res.Elapsed.Round(1e6), humanNodes(res.Nodes))

	// 候选
	if len(res.Candidates) > 1 {
		sb.WriteString("\n")
		fmt.Fprintf(&sb, "%s候选走法%s\n", dim(opts.Color), reset(opts.Color))
		for i, c := range res.Candidates {
			fmt.Fprintf(&sb, "  %d. %-10s %s\n", i+1, c.Notation, scoreText(c.Score, opts.Color))
			if len(c.PV) > 1 {
				fmt.Fprintf(&sb, "     %s%s%s\n",
					dim(opts.Color), formatPVLine(c.PV[1:]), reset(opts.Color))
			}
		}
	}

	fmt.Fprintf(&sb, "\n%s局面%s  %s\n", dim(opts.Color), reset(opts.Color), res.FEN)
	if res.Status != game.Ongoing {
		fmt.Fprintf(&sb, "%s对局结束: %s%s\n", bold(opts.Color), res.Status, reset(opts.Color))
	}
	return sb.String()
}

// MaxPVDisplay 是终端上展示变化图时最多显示的手数。
//
// 引擎给出的 PV 动辄几十手，整条贴出来会淹没真正重要的信息；截断到
// 前几手已经足够判断这条线路的意图。
const MaxPVDisplay = 8

// formatPVLine 把变化图截断并连成一行。
func formatPVLine(pv []string) string {
	if len(pv) > MaxPVDisplay {
		return strings.Join(pv[:MaxPVDisplay], " ") + " …"
	}
	return strings.Join(pv, " ")
}

// RenderMove 渲染"检测到对方走子"这一行。
func RenderMove(notation string, move game.Move, color bool) string {
	return fmt.Sprintf("%s对方走子%s  %s%s%s  %s%s%s",
		dim(color), reset(color),
		bold(color), notation, reset(color),
		dim(color), move, reset(color))
}

// scoreText 给评分上色：正分绿、负分红。
func scoreText(s interface{ String() string }, color bool) string {
	text := s.String()
	if !color {
		return text
	}
	if strings.HasPrefix(text, "-") {
		return ansiRed + text + ansiReset
	}
	return ansiGreen + text + ansiReset
}

func humanNodes(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprint(n)
	}
}

func separator(color bool) string {
	line := strings.Repeat("─", 46)
	if color {
		return ansiDim + line + ansiReset + "\n"
	}
	return line + "\n"
}

func dim(on bool) string {
	if on {
		return ansiDim
	}
	return ""
}

func bold(on bool) string {
	if on {
		return ansiBold
	}
	return ""
}

func reset(on bool) string {
	if on {
		return ansiReset
	}
	return ""
}
