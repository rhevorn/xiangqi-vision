package main

import (
	"flag"
	"fmt"
	"image"
	"path/filepath"
	"strings"

	"xiangqi-vision/internal/capture"
	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/vision"
)

// defaultSimulateMoves 是一段常见开局，用来生成演示用的画面序列。
var defaultSimulateMoves = []string{
	"炮二平五", "马8进7",
	"马二进三", "车9平8",
	"车一平二", "马2进3",
	"兵七进一", "卒7进1",
	"车二进六", "炮8平9",
}

// cmdSimulate 合成一段对局的画面序列，用来在没有手机的情况下验证整条链路。
//
// 它把"棋盘 → 画面"这一步反过来做一遍：按校准参数把棋盘画成图，再让
// watch 从这些图里把走法认出来。只要 simulate 出来的序列能被 watch 正确
// 还原，就说明校准、差分、规则校验、引擎这几环是通的。
func cmdSimulate(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	outDir := fs.String("o", "testdata/game", "输出目录")
	movesArg := fs.String("moves", strings.Join(defaultSimulateMoves, " "), "要模拟的走法（中文记谱，空格分隔）")
	boardX := fs.Int("x", 120, "棋盘左上角横坐标")
	boardY := fs.Int("y", 310, "棋盘左上角纵坐标")
	boardW := fs.Int("w", 870, "棋盘宽度（左上到右下角点的横向跨度）")
	boardH := fs.Int("h", 970, "棋盘高度")
	width := fs.Int("width", 1080, "画面宽度")
	height := fs.Int("height", 2400, "画面高度")
	repeat := fs.Int("repeat", 5, "每个画面重复生成的张数")
	cfgPath := fs.String("config", "configs/config.yaml", "同时写出配置文件到该路径（留空则不写）")
	if err := parseArgsNoPositional(fs, args); err != nil {
		return err
	}

	rect := vision.Rect{X: *boardX, Y: *boardY, Width: *boardW, Height: *boardH}
	cal, err := vision.NewCalibration(rect, 0)
	if err != nil {
		return err
	}

	size := image.Pt(*width, *height)
	if err := cal.Validate(image.Rect(0, 0, size.X, size.Y)); err != nil {
		return fmt.Errorf("棋盘参数与画面尺寸不匹配: %w", err)
	}

	moves := strings.Fields(*movesArg)
	if len(moves) == 0 {
		return fmt.Errorf("请至少给出一手走法（-moves）")
	}

	b := game.NewBoard()
	seq := []struct {
		label string
		img   *image.RGBA
	}{
		{"初始局面", vision.RenderSyntheticBoard(b, cal, size)},
	}

	for i, notation := range moves {
		m, err := game.ParseChinese(b, notation)
		if err != nil {
			return fmt.Errorf("第 %d 手 %q: %w", i+1, notation, err)
		}
		if err := b.Apply(m); err != nil {
			return fmt.Errorf("第 %d 手 %q: %w", i+1, notation, err)
		}
		seq = append(seq, struct {
			label string
			img   *image.RGBA
		}{
			// 带上"最后一步"高亮，尽量贴近真实 App 的显示
			fmt.Sprintf("第 %d 手 %s", i+1, notation),
			vision.RenderSyntheticBoard(b, cal, size, m.From, m.To),
		})
	}

	// 写出画面
	for i, s := range seq {
		for r := 0; r < *repeat; r++ {
			path := filepath.Join(*outDir, fmt.Sprintf("%03d_%02d.png", i, r))
			if err := capture.SavePNG(path, s.img); err != nil {
				return err
			}
		}
	}

	// 写出配套配置，让 watch 可以直接用
	if *cfgPath != "" {
		cfg := config.Default()
		cfg.Board = config.Board{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
		cfg.Capture.RepeatEach = 1
		if err := cfg.Save(*cfgPath); err != nil {
			return err
		}
	}

	fmt.Printf("已生成 %d 个画面（每个 %d 张）到 %s\n", len(seq), *repeat, *outDir)
	for i, s := range seq {
		fmt.Printf("  %03d  %s\n", i, s.label)
	}
	fmt.Printf("\n最终局面 %s\n", b.FEN())
	if *cfgPath != "" {
		fmt.Printf("配置已写入 %s\n", *cfgPath)
	}
	fmt.Printf("\n接下来可以这样回放:\n")
	fmt.Printf("  assistant capture -image %s/000_00.png -o /tmp/shot.png   # 也适用于校准演示\n", *outDir)
	fmt.Printf("  assistant watch -frames %s -config %s\n", *outDir, *cfgPath)
	return nil
}

// parseArgsNoPositional 解析不接受位置参数的子命令。
func parseArgsNoPositional(fs *flag.FlagSet, args []string) error {
	_, err := parseArgs(fs, args)
	return err
}
