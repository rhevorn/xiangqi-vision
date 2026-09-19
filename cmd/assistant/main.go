// Command assistant 是 Android 中国象棋 AI 辅助系统的命令行入口。
//
// 子命令按《技术方案》§21 的 MVP 开发顺序组织：
//
//	board      打印棋盘
//	analyze    Phase 1：规则 + FEN + Pikafish 链路，输入走法直接出建议
//	capture    Phase 2：从安卓设备抓一张截图存成 PNG
//	calibrate  Phase 3：浏览器点击式棋盘校准
//	watch      Phase 5：完整链路，自动识别对方走子并给出建议
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"xiangqi-vision/internal/analyzer"
	"xiangqi-vision/internal/capture"
	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/engine"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/overlay"
	"xiangqi-vision/internal/vision"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "board":
		err = cmdBoard(args)
	case "analyze":
		err = cmdAnalyze(args)
	case "capture":
		err = cmdCapture(args)
	case "devices":
		err = cmdDevices(args)
	case "calibrate":
		err = cmdCalibrate(args)
	case "watch":
		err = cmdWatch(args)
	case "simulate":
		err = cmdSimulate(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return // 子命令已经打印过用法
		}
		fmt.Fprintln(os.Stderr, "错误: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Android 中国象棋 AI 辅助系统

用法:
  assistant <子命令> [选项] [参数]

子命令:
  board       打印棋盘（可指定 FEN）
  analyze     分析局面并给出建议走法（Phase 1）
  capture     从安卓设备抓一张截图存成 PNG（Phase 2）
  devices     列出已连接的安卓设备
  calibrate   浏览器点击式棋盘校准（Phase 3）
  watch       完整链路：自动识别对方走子并给出建议（Phase 5）
  simulate    合成一段对局的画面序列，不用手机也能验证整条链路

示例:
  assistant board
  assistant analyze 炮二平五 马8进7
  assistant analyze -fen "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"
  assistant capture -o shot.png
  assistant calibrate
  assistant watch                 # 连接真机
  assistant simulate              # 生成一段演示画面，不用手机也能验证
  assistant watch -frames testdata/game   # 回放本地帧序列

通用选项请在子命令后加 -h 查看。
`)
}

// ---------------------------------------------------------------------------
// 公共构件
// ---------------------------------------------------------------------------

// common 是所有子命令共享的初始化结果。
type common struct {
	cfg *config.Config
	log *slog.Logger
}

// setup 加载配置并配置日志（§20：从第一天就把日志做好）。
func setup(cfgPath string, verbose, quiet bool) (*common, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	level := slog.LevelInfo
	switch strings.ToLower(cfg.Log.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	if verbose {
		level = slog.LevelDebug
	}
	if quiet {
		level = slog.LevelWarn
	}

	var w io.Writer = os.Stderr
	if cfg.Log.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Log.File), 0o755); err == nil {
			f, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				defer f.Close()
				w = io.MultiWriter(os.Stderr, f)
			}
		}
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// §20 的日志样式：17:31:02 事件
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("15:04:05"))
			}
			return a
		},
	})
	return &common{cfg: cfg, log: slog.New(handler)}, nil
}

// findADB 定位 adb：优先配置，其次 PATH，最后找 Android SDK 的常见位置。
func findADB(cfg *config.Config) string {
	if cfg.Device.ADBPath != "" {
		return cfg.Device.ADBPath
	}
	if p, err := exec.LookPath("adb"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Library/Android/sdk/platform-tools/adb"),
		"/opt/homebrew/bin/adb",
		"/usr/local/bin/adb",
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return "adb"
}

// buildCapturer 决定画面来源：显式参数 > 配置 > 真机。
func buildCapturer(cfg *config.Config, framesDir, imagePath string) (capture.Capturer, error) {
	switch {
	case framesDir != "":
		return capture.NewDirCapturer(framesDir, cfg.Capture.RepeatEach)
	case imagePath != "":
		if _, err := os.Stat(imagePath); err != nil {
			return nil, fmt.Errorf("找不到图片 %s: %w", imagePath, err)
		}
		return capture.NewFileCapturer(imagePath), nil
	case cfg.Capture.FramesDir != "":
		return capture.NewDirCapturer(cfg.Capture.FramesDir, cfg.Capture.RepeatEach)
	case cfg.Capture.FrameFile != "":
		return capture.NewFileCapturer(cfg.Capture.FrameFile), nil
	default:
		return capture.NewADBCapturer(findADB(cfg), cfg.Device.Serial), nil
	}
}

// buildEngine 创建引擎。mock 为真时使用内置假引擎，便于无引擎环境自检。
func buildEngine(cfg *config.Config, mock bool, log *slog.Logger) (engine.Engine, error) {
	if mock {
		return engine.NewMock(), nil
	}
	path := cfg.Engine.Path
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("找不到引擎可执行文件 %s（可用 -mock 先跑通链路）: %w", path, err)
	}
	return engine.NewUCIEngine(engine.Options{
		Path:     path,
		Threads:  cfg.Engine.Threads,
		HashMB:   cfg.Engine.HashMB,
		MultiPV:  cfg.Engine.MultiPV,
		EvalFile: cfg.Engine.EvalFile,
		Logger:   log,
	}), nil
}

// buildCalibration 依据配置建立校准。
func buildCalibration(cfg *config.Config) (*vision.Calibration, error) {
	if !cfg.Board.Configured() {
		return nil, fmt.Errorf("尚未校准棋盘，请先运行 `assistant calibrate`" +
			"（或手工填写配置里的 board 段）")
	}
	return vision.NewCalibration(cfg.Board.Rect(), cfg.Vision.ROISize)
}

// splitFlags 把命令行参数拆成"选项"与"位置参数"两部分。
//
// 标准库 flag 在遇到第一个非选项参数时就停止解析，但本程序里走法是天然
// 的位置参数（assistant analyze 炮二平五 -movetime 400），因此先手工分拣
// 一遍，让选项写在走法之前或之后都能生效。
func splitFlags(fs *flag.FlagSet, args []string) (flags, positional []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]

		if a == "--" {
			return append(flags, args[i+1:]...), positional, nil
		}
		// 走法（中文记谱或 UCI 坐标）都不会以短横线开头
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}

		name := strings.TrimLeft(a, "-")
		inlineValue := false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, inlineValue = name[:eq], true
		}

		f := fs.Lookup(name)
		if f == nil {
			// -h / -help 按惯例打印用法后正常退出
			if name == "h" || name == "help" {
				fs.Usage()
				return nil, nil, flag.ErrHelp
			}
			return nil, nil, fmt.Errorf("未知选项 %s", a)
		}

		flags = append(flags, a)
		if inlineValue || isBoolFlag(f) {
			continue
		}
		// 该选项需要一个值，把紧随其后的参数一并归入选项
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("选项 %s 缺少参数", a)
		}
		i++
		flags = append(flags, args[i])
	}
	return flags, positional, nil
}

// isBoolFlag 报告该选项是否为布尔开关（布尔选项后面不跟值）。
func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

// parseArgs 先分拣再解析，返回位置参数。
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	flags, positional, err := splitFlags(fs, args)
	if err != nil {
		return nil, err
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}

// parseMoveArg 解析一招棋：先按中文记谱，再按 UCI 坐标。
func parseMoveArg(b *game.Board, s string) (game.Move, error) {
	if m, err := game.ParseChinese(b, s); err == nil {
		return m, nil
	}
	m, err := game.ParseMove(s)
	if err != nil {
		return game.Move{}, fmt.Errorf("无法解析走法 %q（既不是中文记谱，也不是 UCI 坐标）", s)
	}
	return m, nil
}

// isTTY 报告文件是否为终端，用于决定是否开启颜色。
func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// signalContext 返回一个在收到 Ctrl-C 时取消的 context。
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// ---------------------------------------------------------------------------
// board
// ---------------------------------------------------------------------------

func cmdBoard(args []string) error {
	fs := flag.NewFlagSet("board", flag.ContinueOnError)
	fen := fs.String("fen", game.StartFEN, "局面 FEN")
	color := fs.Bool("color", isTTY(os.Stdout), "彩色输出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	b, err := game.ParseFEN(*fen)
	if err != nil {
		return err
	}
	fmt.Print(overlay.RenderBoard(b, overlay.Options{Color: *color}))
	fmt.Printf("\nFEN   %s\n", b.FEN())
	fmt.Printf("行棋  %s方\n", b.SideToMove())
	fmt.Printf("状态  %s\n", b.Status())
	return nil
}

// ---------------------------------------------------------------------------
// analyze —— Phase 1 验收路径
// ---------------------------------------------------------------------------

func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "配置文件路径")
	fen := fs.String("fen", game.StartFEN, "起始局面 FEN")
	depth := fs.Int("depth", 0, "搜索深度（0 表示用配置里的 movetime）")
	movetime := fs.Int("movetime", 0, "思考时间（毫秒）")
	multipv := fs.Int("multipv", 0, "候选走法数量")
	showBoard := fs.Bool("board", true, "打印棋盘")
	color := fs.Bool("color", isTTY(os.Stdout), "彩色输出")
	mock := fs.Bool("mock", false, "使用内置假引擎（不需要 pikafish）")
	verbose := fs.Bool("v", false, "调试日志")
	moves, err := parseArgs(fs, args)
	if err != nil {
		return err
	}

	c, err := setup(*cfgPath, *verbose, false)
	if err != nil {
		return err
	}
	if *movetime > 0 {
		c.cfg.Engine.MoveTimeMS = *movetime
	}
	if *multipv > 0 {
		c.cfg.Engine.MultiPV = *multipv
	}

	b, err := game.ParseFEN(*fen)
	if err != nil {
		return err
	}

	// 依次执行命令行给出的走法，逐步走到待分析的局面
	for i, s := range moves {
		m, err := parseMoveArg(b, s)
		if err != nil {
			return fmt.Errorf("第 %d 手: %w", i+1, err)
		}
		notation := game.FormatChinese(b, m)
		if err := b.Apply(m); err != nil {
			return fmt.Errorf("第 %d 手 %s: %w", i+1, notation, err)
		}
		fmt.Printf("%2d. %-8s %s\n", i+1, notation, m)
	}
	if len(moves) > 0 {
		fmt.Println()
	}

	if *showBoard {
		last, hasLast := b.LastMove()
		var hl []game.Square
		if hasLast {
			hl = []game.Square{last.From, last.To}
		}
		fmt.Print(overlay.RenderBoard(b, overlay.Options{Color: *color, Highlight: hl}))
		fmt.Println()
	}

	if s := b.Status(); s != game.Ongoing {
		fmt.Printf("对局结束: %s\n", s)
		return nil
	}

	eng, err := buildEngine(c.cfg, *mock, c.log)
	if err != nil {
		return err
	}
	defer eng.Close()

	an := analyzer.New(eng, analyzer.Options{
		MoveTime: c.cfg.Engine.MoveTime(),
		Depth:    *depth,
		MultiPV:  c.cfg.Engine.MultiPV,
	})

	ctx, cancel := signalContext()
	defer cancel()

	if !*mock {
		fmt.Printf("分析中…（%s，%dms，MultiPV %d）\n\n",
			eng.Name(), c.cfg.Engine.MoveTimeMS, c.cfg.Engine.MultiPV)
	}

	start := time.Now()
	res, err := an.Analyze(ctx, b, nil)
	if err != nil {
		return err
	}
	c.log.Debug("分析完成", "耗时", time.Since(start))

	fmt.Print(overlay.RenderAnalysis(res, overlay.Options{Color: *color}))
	return nil
}

// ---------------------------------------------------------------------------
// capture / devices —— Phase 2
// ---------------------------------------------------------------------------

func cmdCapture(args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "配置文件路径")
	out := fs.String("o", "", "输出 PNG 路径（默认 debug/shot_<时间>.png）")
	serial := fs.String("serial", "", "设备序列号")
	adb := fs.String("adb", "", "adb 可执行文件路径")
	image := fs.String("image", "", "改为读取本地图片而不是抓屏")
	verbose := fs.Bool("v", false, "调试日志")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := setup(*cfgPath, *verbose, false)
	if err != nil {
		return err
	}

	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(c.cfg.Debug.Dir,
			fmt.Sprintf("shot_%s.png", time.Now().Format("20060102_150405")))
	}

	var src capture.Capturer
	if *image != "" {
		src = capture.NewFileCapturer(*image)
	} else {
		if *serial != "" {
			c.cfg.Device.Serial = *serial
		}
		if *adb != "" {
			c.cfg.Device.ADBPath = *adb
		}
		src = capture.NewADBCapturer(findADB(c.cfg), c.cfg.Device.Serial)
	}

	ctx, cancel := signalContext()
	defer cancel()

	img, err := src.Capture(ctx)
	if err != nil {
		return err
	}
	if err := capture.SavePNG(outPath, img); err != nil {
		return err
	}

	b := img.Bounds()
	fmt.Printf("已保存 %s\n", outPath)
	fmt.Printf("来源   %s\n", src.Describe())
	fmt.Printf("尺寸   %dx%d\n", b.Dx(), b.Dy())
	return nil
}

func cmdDevices(args []string) error {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "配置文件路径")
	adb := fs.String("adb", "", "adb 可执行文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := setup(*cfgPath, false, false)
	if err != nil {
		return err
	}
	if *adb != "" {
		c.cfg.Device.ADBPath = *adb
	}

	adbPath := findADB(c.cfg)
	fmt.Printf("adb    %s\n", adbPath)

	cap := capture.NewADBCapturer(adbPath, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	devices, err := cap.Devices(ctx)
	if err != nil {
		return fmt.Errorf("%w\n提示: 确认已安装 platform-tools，且手机已开启 USB 调试", err)
	}
	if len(devices) == 0 {
		fmt.Println("设备   （未发现已授权设备）")
		return nil
	}
	for _, d := range devices {
		fmt.Printf("设备   %s\n", d)
	}
	return nil
}
