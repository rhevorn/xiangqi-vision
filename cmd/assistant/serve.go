package main

import (
	"flag"
	"fmt"

	"xiangqi-vision/internal/vision"
	"xiangqi-vision/internal/webui"
)

// cmdServe 启动网页仪表盘：在浏览器里实时看棋盘、最佳走法与引擎搜索过程。
//
// 相比 watch：网页能把棋盘真正画出来、把建议画成箭头、让搜索结果平滑刷新，
// 也不用记那些按键。未校准时页面会先引导完成校准，不用另跑一条命令。
func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "配置文件路径")
	addr := fs.String("addr", "127.0.0.1:8765", "监听地址")
	framesDir := fs.String("frames", "", "用本地帧序列代替真机（回放/干跑）")
	imagePath := fs.String("image", "", "用固定图片代替真机")
	mock := fs.Bool("mock", false, "使用内置假引擎（不需要 pikafish）")
	noOpen := fs.Bool("no-open", false, "不要自动打开浏览器")
	verbose := fs.Bool("v", false, "调试日志")
	if err := parseArgsNoPositional(fs, args); err != nil {
		return err
	}

	c, err := setup(*cfgPath, *verbose, false)
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

	srv := webui.New(webui.Options{
		Cfg:      c.cfg,
		CfgPath:  defaultOr(*cfgPath),
		Capturer: src,
		Engine:   eng,
		Logger:   c.log,
	})

	listenAddr, err := srv.Listen(*addr)
	if err != nil {
		return err
	}
	url := "http://" + listenAddr + "/"

	fmt.Printf("网页仪表盘已启动: %s\n", url)
	if !srv.Calibrated() {
		fmt.Println("\n棋盘尚未校准，页面会先引导你完成校准，然后自动开始。")
	} else {
		fmt.Println("\n请确认手机画面上此刻是标准初始局面，程序会把第一帧当作开局。")
	}

	ctx, cancel := signalContext()
	defer cancel()

	if srv.Calibrated() {
		if err := srv.StartController(ctx); err != nil {
			c.log.Warn("主循环启动失败", "err", err)
		}
	}
	// 校准完成（或日后重新校准）后把主循环接上
	srv.OnCalibrated(func(vision.Rect) {
		if err := srv.StartController(ctx); err != nil {
			c.log.Error("校准后启动主循环失败", "err", err)
		}
	})

	if !*noOpen {
		if err := openBrowser(url); err != nil {
			c.log.Debug("无法自动打开浏览器", "err", err)
		}
	}

	return srv.Serve(ctx)
}
