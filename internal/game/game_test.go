package game

import (
	"testing"
)

// sq 是测试辅助函数，把 "e2" 形式的坐标转成 Square。
func sq(t *testing.T, s string) Square {
	t.Helper()
	v, err := ParseSquare(s)
	if err != nil {
		t.Fatalf("ParseSquare(%q): %v", s, err)
	}
	return v
}

// move 是测试辅助函数，把 "h2e2" 形式的走法转成 Move。
func move(t *testing.T, s string) Move {
	t.Helper()
	m, err := ParseMove(s)
	if err != nil {
		t.Fatalf("ParseMove(%q): %v", s, err)
	}
	return m
}

func TestInitialPosition(t *testing.T) {
	b := NewBoard()
	if got := b.FEN(); got != StartFEN {
		t.Fatalf("初始局面 FEN 往返失败\n got: %s\nwant: %s", got, StartFEN)
	}
	if b.SideToMove() != Red {
		t.Fatalf("初始局面应为红方行棋，实际 %s", b.SideToMove())
	}
	if s := b.Status(); s != Ongoing {
		t.Fatalf("初始局面应为进行中，实际 %s", s)
	}
	// 车九进一 是合法的（马在车的右侧，不挡车的正前方）
	if !b.IsLegal(move(t, "a0a1")) {
		t.Fatal("初始局面 a0a1 应合法")
	}
	// 但车不能横走，右侧紧邻自己的马
	if b.IsLegal(move(t, "a0b0")) {
		t.Fatal("初始局面 a0b0 应非法：车的右侧是自己的马")
	}
}

func TestSquareConversion(t *testing.T) {
	cases := []struct {
		uci  string
		iccs string
		row  int
		col  int
	}{
		{"a0", "A0", 9, 0}, // 红方底线最左
		{"i9", "I9", 0, 8}, // 黑方底线最右
		{"e2", "E2", 7, 4}, // 红炮原位
	}
	for _, c := range cases {
		s, err := ParseSquare(c.uci)
		if err != nil {
			t.Fatalf("ParseSquare(%q): %v", c.uci, err)
		}
		if s.Row != c.row || s.Col != c.col {
			t.Errorf("ParseSquare(%q) = (%d,%d)，期望 (%d,%d)", c.uci, s.Row, s.Col, c.row, c.col)
		}
		if s.UCI() != c.uci {
			t.Errorf("UCI() = %q，期望 %q", s.UCI(), c.uci)
		}
		if s.String() != c.iccs {
			t.Errorf("String() = %q，期望 %q", s.String(), c.iccs)
		}
	}
}

func TestChineseNotation(t *testing.T) {
	b := NewBoard()

	// 逐个验证红方常见开局着法的记谱与 UCI 互转
	for _, c := range []struct{ notation, uci string }{
		{"炮二平五", "h2e2"},
		{"马二进三", "h0g2"},
		{"兵七进一", "c3c4"},
	} {
		m, err := ParseChinese(b, c.notation)
		if err != nil {
			t.Errorf("ParseChinese(%q): %v", c.notation, err)
			continue
		}
		if m.UCI() != c.uci {
			t.Errorf("ParseChinese(%q) = %s，期望 %s", c.notation, m.UCI(), c.uci)
		}
		if got := FormatChinese(b, m); got != c.notation {
			t.Errorf("FormatChinese(%s) = %q，期望 %q", m.UCI(), got, c.notation)
		}
	}

	// 黑方使用阿拉伯数字
	b2 := NewBoard()
	if err := b2.Apply(mustChinese(t, b2, "炮二平五")); err != nil {
		t.Fatal(err)
	}
	m, err := ParseChinese(b2, "马8进7")
	if err != nil {
		t.Fatalf("ParseChinese(马8进7): %v", err)
	}
	if m.UCI() != "h9g7" {
		t.Errorf("马8进7 = %s，期望 h9g7", m.UCI())
	}
	if got := FormatChinese(b2, m); got != "马8进7" {
		t.Errorf("FormatChinese = %q，期望 马8进7", got)
	}

	// 异体字与全角/半角数字应当都能解析
	for _, variant := range []string{"炮2平5", "砲二平五", "炮二平五", "炮 二 平 五"} {
		if _, err := ParseChinese(NewBoard(), variant); err != nil {
			t.Errorf("ParseChinese(%q) 应当可解析: %v", variant, err)
		}
	}
}

func mustChinese(t *testing.T, b *Board, s string) Move {
	t.Helper()
	m, err := ParseChinese(b, s)
	if err != nil {
		t.Fatalf("ParseChinese(%q): %v", s, err)
	}
	return m
}

// 测试马的走法与蹩马腿。
func TestHorseMoves(t *testing.T) {
	b := NewBoard()
	// 初始局面马二（h0）被自己兵挡住一侧，可走 a? 实际可走 g2 与 i2 中的合法者
	from := sq(t, "h0")
	var targets []string
	for _, m := range b.PseudoMoves(from) {
		targets = append(targets, m.To.UCI())
	}
	// 马在 (9,7)：可到 (7,6)=g2 与 (7,8)=i2
	want := map[string]bool{"g2": true, "i2": true}
	if len(targets) != len(want) {
		t.Fatalf("初始局面 h0 马应有 %d 种走法，实际 %v", len(want), targets)
	}
	for _, tg := range targets {
		if !want[tg] {
			t.Errorf("马不应能走到 %s", tg)
		}
	}

	// 蹩马腿：在 (8,7) 放一枚子，马腿被蹩，g2 与 i2 都应不可行
	b.Set(sq(t, "h1"), Piece{Type: Pawn, Color: Red})
	if n := len(b.PseudoMoves(from)); n != 0 {
		t.Errorf("马腿被蹩后不应有走法，实际 %d 种", n)
	}
}

// 测试象的走法：塞象眼与不可过河。
func TestElephantMoves(t *testing.T) {
	b := NewBoard()
	from := sq(t, "c0") // 红相在 (9,2)
	// 象眼 (8,3)，象只能走一步（另一侧 (8,1) 也是象眼）
	targets := map[string]bool{}
	for _, m := range b.PseudoMoves(from) {
		targets[m.To.UCI()] = true
	}
	// 红相在 (9,2) 可走 (7,0)=a2 与 (7,4)=e2
	if !targets["a2"] || !targets["e2"] {
		t.Fatalf("红相 c0 应可走 a2/e2，实际 %v", targets)
	}

	// 塞象眼
	b.Set(sq(t, "d1"), Piece{Type: Pawn, Color: Black}) // (8,3)
	targets = map[string]bool{}
	for _, m := range b.PseudoMoves(from) {
		targets[m.To.UCI()] = true
	}
	if targets["e2"] {
		t.Error("象眼被占，红相不应能走到 e2")
	}

	// 不可过河：红相在 (6,4) 时不能走到 (4,4) 对面的 (4,2)/(4,6)
	b2 := NewBoard()
	b2.Set(sq(t, "e4"), Piece{Type: Elephant, Color: Red}) // (5,4) 已在河上，走到 (3,x) 需过河
	for _, m := range b2.PseudoMoves(sq(t, "e4")) {
		if m.To.Row < 5 {
			t.Errorf("红相不应过河，却可走到 %s", m.To)
		}
	}
}

// 测试炮：不吃子时同车，吃子必须隔一个炮架。
func TestCannonMoves(t *testing.T) {
	b := NewBoard()
	from := sq(t, "b2") // 红炮在 (7,1)
	// 初始局面 b 列自上而下是：黑马 b9、空 b8、黑炮 b7、黑卒位 b6? 不 ——
	// 黑卒在 a6/c6/e6/g6/i6（偶数列），故 b6 为空，红炮可一路推进到 b6。
	targets := map[string]bool{}
	for _, m := range b.PseudoMoves(from) {
		targets[m.To.UCI()] = true
	}
	for _, want := range []string{"b3", "b4", "b5", "b6"} {
		if !targets[want] {
			t.Errorf("红炮 b2 应能平移到 %s，实际 %v", want, targets)
		}
	}
	// b7 是黑炮，不在无炮架的平移范围内
	if targets["b7"] {
		t.Error("红炮 b2 无炮架时不能越过黑炮到 b7")
	}
	// 黑炮 b7 恰好是炮架，越过它可以吃 b9 的黑马
	if !targets["b9"] {
		t.Errorf("红炮 b2 应能隔黑炮吃 b9 的黑马，实际 %v", targets)
	}

	// 无炮架时不能吃子：清掉 b7 的黑炮后，b9 的黑马就吃不到了
	b2 := NewBoard()
	b2.Set(sq(t, "b7"), Piece{})
	targets = map[string]bool{}
	for _, m := range b2.PseudoMoves(from) {
		targets[m.To.UCI()] = true
	}
	if targets["b9"] {
		t.Error("炮架被移走后，红炮不应能吃 b9 的黑马")
	}
	if !targets["b8"] {
		t.Errorf("炮架被移走后，红炮应能推进到 b8，实际 %v", targets)
	}
}

// 测试兵/卒：未过河只能直前，过河后可横走，永不后退。
func TestPawnMoves(t *testing.T) {
	b := NewBoard()
	from := sq(t, "c3") // 红兵在 (6,2)
	targets := map[string]bool{}
	for _, m := range b.PseudoMoves(from) {
		targets[m.To.UCI()] = true
	}
	if !targets["c4"] {
		t.Error("红兵未过河应能直进一步")
	}
	if len(targets) != 1 {
		t.Errorf("红兵未过河只应有 1 种走法，实际 %v", targets)
	}

	// c4 是河界红方一侧（Row 5），仍未过河，只能直前
	b1 := NewBoard()
	b1.Set(sq(t, "c4"), Piece{Type: Pawn, Color: Red})
	targets = map[string]bool{}
	for _, m := range b1.PseudoMoves(sq(t, "c4")) {
		targets[m.To.UCI()] = true
	}
	if len(targets) != 1 || !targets["c5"] {
		t.Errorf("Row 5 的红兵仍未过河，只应能直走到 c5，实际 %v", targets)
	}

	// 过河后（Row <= 4）可左右横走
	b2 := NewBoard()
	b2.Set(sq(t, "c5"), Piece{Type: Pawn, Color: Red}) // (4,2) 已过河
	targets = map[string]bool{}
	for _, m := range b2.PseudoMoves(sq(t, "c5")) {
		targets[m.To.UCI()] = true
	}
	if !targets["b5"] || !targets["d5"] || !targets["c6"] {
		t.Errorf("过河红兵应能左右与前进，实际 %v", targets)
	}
}

// 测试九宫限制。
func TestPalaceRestriction(t *testing.T) {
	b := NewBoard()
	// 红帅 e0 (9,4) 只能走到 (8,4)=e1
	targets := map[string]bool{}
	for _, m := range b.PseudoMoves(sq(t, "e0")) {
		targets[m.To.UCI()] = true
	}
	if !targets["e1"] || len(targets) != 1 {
		t.Errorf("开局红帅应只能走到 e1，实际 %v", targets)
	}

	// 士只能斜走且不出九宫。
	// 先把初始局面里占住 d0/f0 的两枚红士清掉，否则会被己方棋子挡住。
	b2 := NewBoard()
	b2.Set(sq(t, "d0"), Piece{})
	b2.Set(sq(t, "f0"), Piece{})
	b2.Set(sq(t, "e1"), Piece{Type: Advisor, Color: Red}) // 九宫中心 (8,4)
	targets = map[string]bool{}
	for _, m := range b2.PseudoMoves(sq(t, "e1")) {
		targets[m.To.UCI()] = true
	}
	for _, want := range []string{"d0", "f0", "d2", "f2"} {
		if !targets[want] {
			t.Errorf("九宫中心的红士应能走到 %s，实际 %v", want, targets)
		}
	}

	// 九宫外的士走不出九宫：把士放到九宫角 d0，可走的只有九宫内的 e1
	b3 := NewBoard()
	b3.Set(sq(t, "d0"), Piece{Type: Advisor, Color: Red})
	targets = map[string]bool{}
	for _, m := range b3.PseudoMoves(sq(t, "d0")) {
		targets[m.To.UCI()] = true
	}
	if len(targets) != 1 || !targets["e1"] {
		t.Errorf("九宫角的红士只应能走到 e1，实际 %v", targets)
	}
}

// 测试将帅照面：走出照面的一方走法必须非法。
func TestKingsFacing(t *testing.T) {
	// FEN 第 6 行对应棋盘 Row 5，即 ICCS 的 e4
	b := MustParseFEN("4k4/9/9/9/9/4R4/9/9/9/4K4 w - - 0 1")
	if b.At(sq(t, "e4")).Type != Rook {
		t.Fatalf("测试局面构造有误：e4 应为红车，实际 %s", b.At(sq(t, "e4")))
	}
	if b.KingsFacing() {
		t.Fatal("有红车在 e4 阻挡，双方将帅不应照面")
	}
	// 红车离开第 e 列 → 形成照面 → 非法
	if b.IsLegal(move(t, "e4d4")) {
		t.Error("红车离开 e 列会造成将帅照面，该走法应非法")
	}
	// 红车沿 e 列移动，仍阻挡 → 合法
	if !b.IsLegal(move(t, "e4e5")) {
		t.Error("红车沿 e 列移动不产生照面，应合法")
	}
}

// 测试将军与将死判定。
func TestCheckAndMate(t *testing.T) {
	// 黑将在 d9(0,4)，红车在 a9、a8 形成双车错杀
	mate := MustParseFEN("R3k4/R8/9/9/9/9/9/9/9/3K5 b - - 0 1")
	if !mate.InCheck(Black) {
		t.Fatal("黑方应处于被将军状态")
	}
	if !mate.IsCheckmate(Black) {
		t.Fatalf("该局面应为将死，合法走法：%v", mate.LegalMoves(Black))
	}
	if s := mate.Status(); s != RedWins {
		t.Errorf("Status() = %s，期望红方胜", s)
	}

	// 红方不能走成自将：帅 e0，黑车在 e5 将军，红帅只能离开 e 列
	chk := MustParseFEN("4k4/9/9/9/9/4r4/9/9/9/4K4 w - - 0 1")
	if !chk.InCheck(Red) {
		t.Fatal("红方应处于被将军状态")
	}
	if chk.IsLegal(move(t, "e0e1")) {
		t.Error("红帅仍在 e 列且被车照面，e0e1 应非法")
	}
	if !chk.IsLegal(move(t, "e0d0")) {
		t.Error("红帅走到 d0 可解将，应合法")
	}
}

// 测试困毙（无子可动但未被将军）在中国象棋中同样判负。
func TestStalemateIsLoss(t *testing.T) {
	// 黑将在 e9(0,4)。红车 d8(1,3) 控制 e9 的左右两个退路 d9(0,3) 与 e8(1,4)，
	// 红车 f8(1,5) 控制 f9(0,5)。两车都不在黑将所在的行/列上，故黑将未被将军，
	// 但三个可走的格子全被控制 → 困毙，按中国象棋规则判负。
	b := MustParseFEN("4k4/3R1R3/9/9/9/9/9/9/9/3K5 b - - 0 1")

	if b.InCheck(Black) {
		t.Fatal("黑将不应被将军")
	}
	if !b.IsStalemate(Black) {
		t.Fatalf("该局面应为困毙，合法走法：%v", b.LegalMoves(Black))
	}
	if s := b.Status(); s != RedWins {
		t.Errorf("困毙应判红方胜，实际 %s", s)
	}
}

// 测试悔棋与克隆的独立性。
func TestUndoAndClone(t *testing.T) {
	b := NewBoard()
	clone := b.Clone()

	m := mustChinese(t, b, "炮二平五")
	if err := b.Apply(m); err != nil {
		t.Fatal(err)
	}
	if b.NumMoves() != 1 {
		t.Errorf("走子后应有 1 步历史，实际 %d", b.NumMoves())
	}
	if clone.NumMoves() != 0 {
		t.Error("克隆体不应受原棋盘走子影响")
	}
	// 克隆体的 h2 仍应是红炮，而原棋盘的 h2 已空
	if p := clone.At(sq(t, "h2")); p.Type != Cannon || p.Color != Red {
		t.Errorf("克隆体的 h2 应仍是红炮，实际 %s", p)
	}
	if !b.At(sq(t, "h2")).IsEmpty() {
		t.Error("走子后原棋盘的 h2 应为空")
	}

	// 吃子后悔棋应恢复被吃子
	if err := b.Apply(mustChinese(t, b, "马8进7")); err != nil {
		t.Fatal(err)
	}
	before := b.FEN()
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo 应返回 true")
	}
	if b.FEN() == before {
		t.Error("悔棋后局面应发生变化")
	}
	if _, ok := b.Undo(); !ok {
		t.Fatal("第二次 Undo 应返回 true")
	}
	if got := b.FEN(); got != StartFEN {
		t.Errorf("全部悔棋后应回到初始局面\n got: %s\nwant: %s", got, StartFEN)
	}
}

// 测试 position 命令携带完整历史，供引擎判断重复局面。
func TestPositionCommand(t *testing.T) {
	b := NewBoard()
	if got := b.PositionCommand(); got != "position fen "+StartFEN {
		t.Errorf("无历史时应退化为 position fen <当前局面>，实际 %q", got)
	}
	must := func(m Move) {
		t.Helper()
		if err := b.Apply(m); err != nil {
			t.Fatal(err)
		}
	}
	must(mustChinese(t, b, "炮二平五"))
	must(mustChinese(t, b, "马8进7"))
	want := "position fen " + StartFEN + " moves h2e2 h9g7"
	if got := b.PositionCommand(); got != want {
		t.Errorf("PositionCommand() = %q\n期望 %q", got, want)
	}
}

// 测试 FEN 往返与非法输入。
func TestFENRoundTrip(t *testing.T) {
	fens := []string{
		StartFEN,
		"4k4/9/9/9/9/4R4/9/9/9/4K4 w - - 0 1",
		"R3k4/R8/9/9/9/9/9/9/9/3K5 b - - 3 12",
	}
	for _, fen := range fens {
		b, err := ParseFEN(fen)
		if err != nil {
			t.Errorf("ParseFEN(%q): %v", fen, err)
			continue
		}
		if got := b.FEN(); got != fen {
			t.Errorf("FEN 往返不一致\n got: %s\nwant: %s", got, fen)
		}
	}

	for _, bad := range []string{
		"",
		"rnbakabnr/9/9/9/9/9/9/9/RNBAKABNR w - - 0 1", // 只有 9 行
		"rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR x - - 0 1",
		"zzzzzzzzz/9/9/9/9/9/9/9/9/RNBAKABNR w - - 0 1",
		"rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABN w - - 0 1", // 末行只有 8 列
	} {
		if _, err := ParseFEN(bad); err == nil {
			t.Errorf("ParseFEN(%q) 应当报错", bad)
		}
	}
}

// 测试合法走法数量，用于回归保护。
func TestInitialLegalMoveCount(t *testing.T) {
	b := NewBoard()
	got := len(b.LegalMoves(Red))
	// 中国象棋开局红方共有 44 种合法走法
	if got != 44 {
		t.Errorf("开局红方合法走法数 = %d，期望 44", got)
	}
}
