// Package capture 负责取得手机屏幕画面（《技术方案》§5）。
//
// 第一版直接使用 adb exec-out screencap -p。象棋是回合制，帧率要求不高，
// 2~5 FPS 已足够判断是否发生走棋。
package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrExhausted 表示帧序列已经播放完毕（仅 DirCapturer 会返回）。
var ErrExhausted = errors.New("帧序列已播放完毕")

// Capturer 是画面来源的抽象。
//
// 除安卓截图外，还有读取本地 PNG 的实现，便于在没有手机时开发与测试。
type Capturer interface {
	// Capture 取得一帧画面。
	Capture(ctx context.Context) (*image.RGBA, error)
	// Describe 返回画面来源的说明，用于日志。
	Describe() string
	// Close 释放资源。
	Close() error
}

// DecodePNG 把 PNG 字节解码为 RGBA。
func DecodePNG(data []byte) (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码 PNG 失败: %w", err)
	}
	return toRGBA(img), nil
}

// toRGBA 把任意图像转成 RGBA。
//
// 统一成 RGBA 之后，后续取像素不必再走 image.Image 接口的 At()，
// 纯 Go 的图像处理会快很多。
func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// LoadImage 从本地文件读取一张 PNG/JPEG。
func LoadImage(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("解码图片 %s 失败: %w", filepath.Base(path), err)
	}
	return toRGBA(img), nil
}

// SavePNG 把图像写入 PNG 文件（用于 debug/ 目录留档，§20）。
func SavePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("写入 PNG %s 失败: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ADB 截图
// ---------------------------------------------------------------------------

// ADBCapturer 通过 adb exec-out screencap -p 从安卓设备抓屏。
type ADBCapturer struct {
	// ADBPath 是 adb 可执行文件路径，留空则用 PATH 中的 adb。
	ADBPath string
	// Serial 是设备序列号；只有一台设备时可留空。
	Serial string
}

// NewADBCapturer 创建一个 adb 截图器。
func NewADBCapturer(adbPath, serial string) *ADBCapturer {
	if adbPath == "" {
		adbPath = "adb"
	}
	return &ADBCapturer{ADBPath: adbPath, Serial: serial}
}

// Describe 返回来源说明。
func (c *ADBCapturer) Describe() string {
	if c.Serial != "" {
		return "adb 设备 " + c.Serial
	}
	return "adb 默认设备"
}

// Close 实现 Capturer 接口。
func (c *ADBCapturer) Close() error { return nil }

// Capture 抓取一帧画面。
func (c *ADBCapturer) Capture(ctx context.Context) (*image.RGBA, error) {
	args := make([]string, 0, 5)
	if c.Serial != "" {
		args = append(args, "-s", c.Serial)
	}
	// exec-out 不会对二进制输出做换行转换，这也是必须用 exec-out 而不是
	// shell 的原因：shell 会把 PNG 里的 \n 变成 \r\n，图片就损坏了。
	args = append(args, "exec-out", "screencap", "-p")

	cmd := exec.CommandContext(ctx, c.ADBPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("adb 截图失败: %w（stderr: %s）", err, shortErr(stderr.String()))
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("adb 未返回画面数据（stderr: %s）", shortErr(stderr.String()))
	}

	img, err := DecodePNG(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("adb 返回的画面无法解码（前 16 字节 % x）: %w",
			firstBytes(stdout.Bytes(), 16), err)
	}
	return img, nil
}

// Devices 列出当前连接的设备序列号。
func (c *ADBCapturer) Devices(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, c.ADBPath, "devices")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("执行 adb devices 失败: %w", err)
	}

	var devices []string
	for _, line := range strings.Split(string(out), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			devices = append(devices, fields[0])
		}
	}
	return devices, nil
}

// ScreenSize 返回设备屏幕尺寸，用于日志与配置校验。
func (c *ADBCapturer) ScreenSize(ctx context.Context) (int, int, error) {
	args := make([]string, 0, 5)
	if c.Serial != "" {
		args = append(args, "-s", c.Serial)
	}
	args = append(args, "shell", "wm", "size")

	out, err := exec.CommandContext(ctx, c.ADBPath, args...).Output()
	if err != nil {
		return 0, 0, err
	}
	// 输出形如 "Physical size: 1080x2400"
	for _, line := range strings.Split(string(out), "\n") {
		if _, rest, ok := strings.Cut(line, ":"); ok {
			wh := strings.Split(strings.TrimSpace(rest), "x")
			if len(wh) == 2 {
				w, err1 := strconv.Atoi(strings.TrimSpace(wh[0]))
				h, err2 := strconv.Atoi(strings.TrimSpace(wh[1]))
				if err1 == nil && err2 == nil {
					return w, h, nil
				}
			}
		}
	}
	return 0, 0, errors.New("无法解析屏幕尺寸")
}

// ---------------------------------------------------------------------------
// 本地文件 / 帧序列
// ---------------------------------------------------------------------------

// FileCapturer 每次都读取同一个文件，用于调试单张截图。
type FileCapturer struct{ Path string }

// NewFileCapturer 创建一个单文件截图器。
func NewFileCapturer(path string) *FileCapturer { return &FileCapturer{Path: path} }

// Describe 返回来源说明。
func (c *FileCapturer) Describe() string { return "文件 " + c.Path }

// Close 实现 Capturer 接口。
func (c *FileCapturer) Close() error { return nil }

// Capture 读取该文件。
func (c *FileCapturer) Capture(ctx context.Context) (*image.RGBA, error) {
	return LoadImage(c.Path)
}

// DirCapturer 按文件名顺序依次返回目录中的图片。
//
// 它让整条链路可以在没有手机的情况下被确定性地回放：把一段对局录成
// 若干张 PNG，就能反复验证「变化 → 稳定 → 推断走法 → 更新棋盘」是否正确。
type DirCapturer struct {
	Files []string
	// RepeatEach 表示每个文件被连续返回多少次，用来模拟真实抓屏时
	// 同一个静止画面会被反复采到，从而驱动「连续 N 帧稳定」的状态机。
	RepeatEach int

	idx     int
	repeats int
}

// NewDirCapturer 扫描目录中的 PNG 文件，按文件名排序。
func NewDirCapturer(dir string, repeatEach int) (*DirCapturer, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".png", ".jpg", ".jpeg":
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("目录 %s 中没有图片文件", dir)
	}
	sort.Strings(files)
	if repeatEach < 1 {
		repeatEach = 1
	}
	return &DirCapturer{Files: files, RepeatEach: repeatEach}, nil
}

// Describe 返回来源说明。
func (c *DirCapturer) Describe() string {
	return fmt.Sprintf("帧序列 %d 张（每张重复 %d 次）", len(c.Files), c.RepeatEach)
}

// Close 实现 Capturer 接口。
func (c *DirCapturer) Close() error { return nil }

// Capture 返回下一帧。
func (c *DirCapturer) Capture(ctx context.Context) (*image.RGBA, error) {
	if c.idx >= len(c.Files) {
		return nil, ErrExhausted
	}
	img, err := LoadImage(c.Files[c.idx])
	if err != nil {
		return nil, err
	}
	c.repeats++
	if c.repeats >= c.RepeatEach {
		c.repeats = 0
		c.idx++
	}
	return img, nil
}

// ---------------------------------------------------------------------------

func shortErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func firstBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}
