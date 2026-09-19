// Package game 实现中国象棋的棋盘状态、走法规则、FEN 序列化与中文记谱。
//
// # 坐标约定
//
// 一律以红方视角（即屏幕上方为黑方）：
//
//	Row 0 = 黑方底线（黑将所在行）
//	Row 9 = 红方底线（红帅所在行）
//	Col 0 = 屏幕最左列，Col 8 = 屏幕最右列
//
// 河界位于 Row 4 与 Row 5 之间：Row 0-4 为黑方半场，Row 5-9 为红方半场。
// 九宫为 Col 3-5；黑方九宫 Row 0-2，红方九宫 Row 7-9。
//
// # 为什么不用 *Piece
//
// 把 NoPiece 放在 PieceType 首位，Piece 的零值就天然表示空位，于是
// [10][9]Piece 可以按值拷贝与比较，不需要 *Piece 指针——避免了整类空指针
// 与别名 bug。代价只是 King 的取值从 0 变成 1，仅此而已。
package game

// 棋盘交叉点行列数。中国象棋为 9 列 10 行共 90 个交叉点。
const (
	Rows = 10
	Cols = 9
)

// NumSquares 棋盘交叉点总数。
const NumSquares = Rows * Cols

// PieceType 棋子类型。
type PieceType uint8

const (
	NoPiece  PieceType = iota // 空位
	King                      // 将 / 帅
	Advisor                   // 士 / 仕
	Elephant                  // 象 / 相
	Horse                     // 马
	Rook                      // 车
	Cannon                    // 炮
	Pawn                      // 卒 / 兵
)

// NumPieceTypes 为有效棋子类型数量（不含 NoPiece）。
const NumPieceTypes = int(Pawn)

// Color 棋子颜色，同时也是行棋方。
type Color uint8

const (
	Red Color = iota
	Black
)

// Opposite 返回对方颜色。
func (c Color) Opposite() Color { return Red + Black - c }

func (c Color) String() string {
	if c == Red {
		return "红"
	}
	return "黑"
}

// FENChar 返回 FEN 中的行棋方字符：红方 w，黑方 b。
func (c Color) FENChar() byte {
	if c == Red {
		return 'w'
	}
	return 'b'
}

// forward 返回该方"前进一步"的行增量。红方在下方，向前即行号减小。
func (c Color) forward() int {
	if c == Red {
		return -1
	}
	return 1
}

// Piece 一枚棋子。零值表示空位。
type Piece struct {
	Type  PieceType
	Color Color
}

// IsEmpty 报告该位置是否为空。
func (p Piece) IsEmpty() bool { return p.Type == NoPiece }

// Name 返回棋子的中文名，红黑用字不同（如红"帅"黑"将"）。
func (p Piece) Name() string {
	if p.IsEmpty() {
		return "·"
	}
	return pieceName[p.Color][p.Type]
}

func (p Piece) String() string { return p.Name() }

// FENChar 返回 FEN 中该棋子的字母，红大写、黑小写。
func (p Piece) FENChar() byte { return fenChars[p.Color][p.Type] }

var pieceName = [2][NumPieceTypes + 1]string{
	{"", "帅", "仕", "相", "马", "车", "炮", "兵"}, // Red
	{"", "将", "士", "象", "马", "车", "炮", "卒"}, // Black
}

var fenChars = [2][NumPieceTypes + 1]byte{
	{' ', 'K', 'A', 'B', 'N', 'R', 'C', 'P'}, // Red
	{' ', 'k', 'a', 'b', 'n', 'r', 'c', 'p'}, // Black
}

// fenToPiece 由 fenChars 反查，保证两个方向永远一致。
var fenToPiece = func() map[byte]Piece {
	m := make(map[byte]Piece, 2*NumPieceTypes)
	for _, c := range []Color{Red, Black} {
		for t := PieceType(1); t <= Pawn; t++ {
			m[fenChars[c][t]] = Piece{Type: t, Color: c}
		}
	}
	return m
}()
