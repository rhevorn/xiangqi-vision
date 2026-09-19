package capture_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"xiangqi-vision/internal/capture"
)

// makePNG 生成一张小图并编码成 PNG 字节。
func makePNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeAndSavePNG(t *testing.T) {
	data := makePNG(t, 100)

	img, err := capture.DecodePNG(data)
	if err != nil {
		t.Fatalf("DecodePNG: %v", err)
	}
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
		t.Fatalf("尺寸错误: %v", img.Bounds())
	}
	if got := img.Pix[0]; got != 100 {
		t.Errorf("像素值 = %d，期望 100", got)
	}

	// 损坏的数据应报错而不是崩溃
	if _, err := capture.DecodePNG([]byte("这不是 PNG")); err == nil {
		t.Error("损坏的数据应报错")
	}

	// 落盘后应能原样读回
	path := filepath.Join(t.TempDir(), "sub", "shot.png")
	if err := capture.SavePNG(path, img); err != nil {
		t.Fatalf("SavePNG 应自动创建目录: %v", err)
	}
	back, err := capture.LoadImage(path)
	if err != nil {
		t.Fatalf("LoadImage: %v", err)
	}
	if back.Bounds() != img.Bounds() {
		t.Errorf("往返后尺寸不一致: %v vs %v", back.Bounds(), img.Bounds())
	}
}

func TestFileCapturer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.png")
	if err := os.WriteFile(path, makePNG(t, 42), 0o644); err != nil {
		t.Fatal(err)
	}

	c := capture.NewFileCapturer(path)
	defer c.Close()

	// 每次都返回同一张图
	for i := 0; i < 3; i++ {
		img, err := c.Capture(context.Background())
		if err != nil {
			t.Fatalf("第 %d 次 Capture: %v", i+1, err)
		}
		if img.Pix[0] != 42 {
			t.Errorf("第 %d 次像素值 = %d", i+1, img.Pix[0])
		}
	}
}

// DirCapturer 是"没有手机也能验证整条链路"的基础，它的播放顺序与重复
// 次数直接决定了稳定判定能否被正确驱动。
func TestDirCapturer(t *testing.T) {
	dir := t.TempDir()
	// 刻意用乱序的文件名写入，验证按名称排序而非写入顺序
	for name, shade := range map[string]uint8{
		"003.png": 30,
		"001.png": 10,
		"002.png": 20,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), makePNG(t, shade), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c, err := capture.NewDirCapturer(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 3 {
		t.Fatalf("应发现 3 个文件，实际 %d", len(c.Files))
	}

	// 期望顺序：001(10) ×2、002(20) ×2、003(30) ×2
	want := []uint8{10, 10, 20, 20, 30, 30}
	for i, w := range want {
		img, err := c.Capture(context.Background())
		if err != nil {
			t.Fatalf("第 %d 次 Capture: %v", i+1, err)
		}
		if got := img.Pix[0]; got != w {
			t.Errorf("第 %d 帧像素值 = %d，期望 %d", i+1, got, w)
		}
	}

	// 播完后必须明确报告耗尽，而不是重复最后一帧——
	// 否则回放永远不会结束。
	if _, err := c.Capture(context.Background()); !errors.Is(err, capture.ErrExhausted) {
		t.Errorf("播完后应返回 ErrExhausted，实际 %v", err)
	}
}

func TestDirCapturerEmptyDir(t *testing.T) {
	if _, err := capture.NewDirCapturer(t.TempDir(), 1); err == nil {
		t.Error("空目录应报错")
	}
}

// 非图片文件应被忽略。
func TestDirCapturerIgnoresNonImages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), makePNG(t, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := capture.NewDirCapturer(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 1 {
		t.Errorf("应只发现 1 张图片，实际 %d", len(c.Files))
	}
}

func TestADBCapturerDescribe(t *testing.T) {
	c := capture.NewADBCapturer("", "emulator-5554")
	if got := c.Describe(); got != "adb 设备 emulator-5554" {
		t.Errorf("Describe() = %q", got)
	}
	if got := capture.NewADBCapturer("/tmp/adb", "").Describe(); got != "adb 默认设备" {
		t.Errorf("Describe() = %q", got)
	}
}
