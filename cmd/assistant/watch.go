package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"xiangqi-vision/internal/analyzer"
	"xiangqi-vision/internal/app"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/overlay"
)

// cmdWatch 跑完整链路（Phase 5）：抓屏 → 差分 → 推断走法 → 规则校验 →
// 更新棋盘 → 引擎分析 → 展示建议。
func cmdWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "配置文件路径")
	framesDir := fs.String("frames", "", "用本地帧序列代替真机（回放/干跑）")
	imagePath := fs.String("image", "", "用固定图片代替真机")
	mock := fs.Bool("mock", false, "使用内置假引擎（不需要 pikafish）")
	color := fs.Bool("color", isTTY(os.Stdout), "彩色输出")
	showBoard := fs.Bool("board", false, "每步都打印棋盘")
	progress := fs.Bool("progress", true, "显示渐进式搜索进度")
	verbose := fs.Bool("v", false, "调试日志")
	quiet := fs.Bool("q", false, "只输出警告与错误")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := setup(*cfgPath, *verbose, *quiet)
	if err != nil {
		return err
	}

	cal, err := buildCalibration(c.cfg)
	if err != nil {
		return err
	}

	src, err := buildCapturer(c.cfg, *framesDir, *imagePath)
	if err != nil {
		return err
	}

	eng, err := buildEngine(c.cfg, *mock, c.log)
	if err != nil {
		return err
	}
	defer eng.Close()

	ctx, cancel := signalContext()
	defer cancel()

	out := os.Stdout
	hooks := app.Hooks{
		OnDetected: func(m game.Move, notation string) {
			fmt.Fprintln(out)
			fmt.Fprintln(out, overlay.RenderMove(notation, m, *color))
		},
		OnProgress: func(r *analyzer.Result) {
			if !*progress || *quiet || r.BestNotation == "" {
				return
			}
			// 单行刷新，让"先给快速答案、再不断加深"的过程可见
			fmt.Fprintf(out, "\r  分析中… 深度 %-3d %s %-8s   ",
				r.Depth, r.Score, r.BestNotation)
		},
		OnResult: func(r *analyzer.Result) {
			if *progress && !*quiet {
				fmt.Fprint(out, "\r\x1b[K") // 清掉进度行
			}
			if !*quiet {
				fmt.Fprint(out, overlay.RenderAnalysis(r, overlay.Options{Color: *color}))
			}
		},
	}

	ctrl := app.New(c.cfg, src, cal, eng, hooks, c.log)

	// 交互按键：r 重新同步，q 退出
	go watchKeys(ctx, cancel, ctrl, c)

	c.log.Info("准备就绪",
		"引擎", eng.Name(),
		"棋盘", c.cfg.Board,
		"提示", "按 r 重新同步，按 q 退出")

	if *showBoard {
		fmt.Fprint(out, overlay.RenderBoard(ctrl.Board(), overlay.Options{Color: *color}))
	}

	err = ctrl.Run(ctx)
	if ctrl.Desynced() {
		fmt.Fprintln(os.Stderr, "\n⚠ 本次运行中出现过失步：内部棋盘与手机画面可能已经不一致。")
		fmt.Fprintln(os.Stderr, "  请在手机上重新开局，然后在本程序里按 r 重新同步。")
	}
	return err
}

// watchKeys 处理终端交互按键。stdin 不是终端时（例如回放脚本里）会立即结束。
func watchKeys(ctx context.Context, cancel func(), ctrl *app.Controller, c *common) {
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		switch buf[0] {
		case 'r', 'R':
			ctrl.Resync()
		case 'q', 'Q':
			c.log.Info("收到退出指令")
			cancel()
			return
		case 'b', 'B':
			fmt.Fprint(os.Stdout, overlay.RenderBoard(ctrl.Board(), overlay.Options{Color: true}))
		case 'f', 'F':
			move, ok := ctrl.LastMove()
			if !ok {
				fmt.Fprintln(os.Stdout, "尚未确认任何走法")
				continue
			}
			fmt.Fprintf(os.Stdout, "最近一步 %s  局面 %s\n", move, ctrl.Board().FEN())
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}
