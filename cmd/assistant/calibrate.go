package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"bytes"
	"image"
	"image/png"

	"xiangqi-vision/internal/capture"
	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/vision"
)

// cmdCalibrate 实现第一次启动时的棋盘校准。
//
// 交互方式：把截图放进一个本地网页，用户按顺序点击棋盘的左上角与右下角
// 交叉点，页面实时把 90 个取样点画出来供核对。看不到点得准不准就填坐标
// 数字，几乎不可能一次配对；直接画出来则一眼就能判断。
func cmdCalibrate(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "配置文件路径")
	imagePath := fs.String("image", "", "用本地图片代替抓屏")
	serial := fs.String("serial", "", "设备序列号")
	adb := fs.String("adb", "", "adb 可执行文件路径")
	port := fs.Int("port", 0, "本地服务端口（0 表示自动选择）")
	noOpen := fs.Bool("no-open", false, "不要自动打开浏览器")
	verbose := fs.Bool("v", false, "调试日志")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := setup(*cfgPath, *verbose, false)
	if err != nil {
		return err
	}
	if *serial != "" {
		c.cfg.Device.Serial = *serial
	}
	if *adb != "" {
		c.cfg.Device.ADBPath = *adb
	}

	ctx, cancel := signalContext()
	defer cancel()

	// 1. 取一张用于校准的画面
	src, err := buildCapturer(c.cfg, "", *imagePath)
	if err != nil {
		return err
	}
	c.log.Info("正在获取画面", "来源", src.Describe())
	img, err := src.Capture(ctx)
	if err != nil {
		return err
	}
	c.log.Info("画面已就绪", "尺寸", fmt.Sprintf("%dx%d", img.Bounds().Dx(), img.Bounds().Dy()))

	// 2. 起本地服务
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("启动本地服务失败: %w", err)
	}
	defer listener.Close()

	saved := make(chan vision.Rect, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, calibratePage(img))
	})
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		rect, err := handleCalibrateSave(w, r, img)
		if err != nil {
			c.log.Error("校准结果无效", "err", err)
			return
		}
		select {
		case saved <- rect:
		default:
		}
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			c.log.Warn("本地服务退出", "err", err)
		}
	}()

	url := fmt.Sprintf("http://%s/", listener.Addr().String())
	fmt.Printf("校准页面已就绪: %s\n\n", url)
	fmt.Println("请在打开的页面中依次点击棋盘的左上角与右下角交叉点（最外侧两条线的交点），")
	fmt.Println("确认预览中的 90 个红点都落在交叉点上之后，点击「保存」。")

	if !*noOpen {
		if err := openBrowser(url); err != nil {
			c.log.Debug("无法自动打开浏览器", "err", err)
		}
	}

	// 3. 等待用户保存
	timeout := time.NewTimer(15 * time.Minute)
	defer timeout.Stop()

	var rect vision.Rect
	select {
	case rect = <-saved:
	case <-ctx.Done():
		shutdown(srv)
		return nil
	case <-timeout.C:
		shutdown(srv)
		return fmt.Errorf("等待校准超时，请重新运行 `assistant calibrate`")
	}
	shutdown(srv)

	// 4. 落盘
	cal, err := vision.NewCalibration(rect, c.cfg.Vision.ROISize)
	if err != nil {
		return err
	}
	if err := cal.Validate(img.Bounds()); err != nil {
		fmt.Printf("\n注意: %v\n", err)
	}

	c.cfg = c.cfg.WithBoard(rect)
	if err := c.cfg.Save(*cfgPath); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}

	// 5. 输出一张带网格的校验图，便于肉眼复核
	preview := vision.DrawCalibration(img, cal)
	previewPath := filepath.Join(c.cfg.Debug.Dir, "calibration.png")
	if err := capture.SavePNG(previewPath, preview); err != nil {
		c.log.Warn("保存校验图失败", "err", err)
	} else {
		fmt.Printf("\n校验图已保存: %s\n", previewPath)
		fmt.Println("请打开确认 90 个红点是否都落在棋盘交叉点上。")
	}

	fmt.Printf("\n校准完成\n")
	fmt.Printf("  棋盘区域  (%d,%d) %dx%d\n", rect.X, rect.Y, rect.Width, rect.Height)
	fmt.Printf("  格点间距  %.1f x %.1f\n", float64(rect.Width)/float64(game.Cols-1), float64(rect.Height)/float64(game.Rows-1))
	fmt.Printf("  取样边长  %d px\n", cal.ROISize)
	fmt.Printf("  配置已写入 %s\n", defaultOr(*cfgPath))
	return nil
}

// handleCalibrateSave 校验并接收浏览器提交的棋盘区域。
func handleCalibrateSave(w http.ResponseWriter, r *http.Request, img image.Image) (vision.Rect, error) {
	var rect vision.Rect
	if err := json.NewDecoder(r.Body).Decode(&rect); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return rect, err
	}

	// 规范化：保证从左上到右下
	if rect.Width < 0 {
		rect.X += rect.Width
		rect.Width = -rect.Width
	}
	if rect.Height < 0 {
		rect.Y += rect.Height
		rect.Height = -rect.Height
	}

	if !rect.Valid() {
		err := fmt.Errorf("棋盘尺寸无效: %dx%d", rect.Width, rect.Height)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return rect, err
	}
	if _, err := vision.NewCalibration(rect, 0); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return rect, err
	}

	b := img.Bounds()
	if rect.X < b.Min.X || rect.Y < b.Min.Y ||
		rect.X+rect.Width > b.Max.X || rect.Y+rect.Height > b.Max.Y {
		err := fmt.Errorf("棋盘区域超出画面范围 %dx%d", b.Dx(), b.Dy())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return rect, err
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
	return rect, nil
}

func shutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return fmt.Errorf("不支持的系统 %s", runtime.GOOS)
}

func defaultOr(s string) string {
	if s == "" {
		return config.DefaultPath()
	}
	return s
}

// encodePNGBase64 把画面编码成可直接嵌进 HTML 的 data URI。
func encodePNGBase64(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
