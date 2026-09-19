package webui

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sync"
	"time"

	"xiangqi-vision/internal/capture"
)

// FrameTap 包装一个 Capturer，顺带把最近一帧缩成小图，供网页做"抓屏是否
// 正常"的实时预览。
//
// 缩略图有最小生成间隔：预览图只是个辅助信息，没必要为了它每一帧都做一次
// 降采样，那会把 CPU 白白吃掉。
type FrameTap struct {
	Inner       capture.Capturer
	MaxWidth    int
	MinInterval time.Duration

	mu     sync.Mutex
	thumb  []byte
	at     time.Time
	serial int64
}

// NewFrameTap 创建包装器。maxWidth<=0 时取 320。
func NewFrameTap(inner capture.Capturer, maxWidth int) *FrameTap {
	if maxWidth <= 0 {
		maxWidth = 320
	}
	return &FrameTap{Inner: inner, MaxWidth: maxWidth, MinInterval: time.Second}
}

// Capture 实现 Capturer，转发给被包装的实现并在需要时更新缩略图。
func (t *FrameTap) Capture(ctx context.Context) (*image.RGBA, error) {
	img, err := t.Inner.Capture(ctx)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	due := time.Since(t.at) >= t.MinInterval
	t.mu.Unlock()

	if due {
		if data, err := encodeThumb(img, t.MaxWidth); err == nil {
			t.mu.Lock()
			t.thumb, t.at, t.serial = data, time.Now(), t.serial+1
			t.mu.Unlock()
		}
	}
	return img, nil
}

// Thumbnail 返回最近一帧的缩略图 PNG 及其版本号；还没有图时返回 nil。
//
// 版本号每次更新都会变，网页据此决定要不要重新拉图，而不必轮询比对内容。
func (t *FrameTap) Thumbnail() ([]byte, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.thumb, t.serial
}

// Describe 实现 Capturer。
func (t *FrameTap) Describe() string { return t.Inner.Describe() }

// Close 实现 Capturer。
func (t *FrameTap) Close() error { return t.Inner.Close() }

// encodeThumb 缩放并编码成 PNG。
func encodeThumb(img image.Image, maxWidth int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, Downscale(img, maxWidth)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Downscale 用盒式滤波把图像等比缩小到不超过 maxWidth 宽。
//
// 用盒式滤波而不是最近邻：手机截图缩小后网格线很细，最近邻会丢线，看着像
// 坏了一样；盒式滤波取平均，缩略图仍然能看出棋盘的样子。
func Downscale(img image.Image, maxWidth int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	if w <= maxWidth {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
		return dst
	}

	scale := float64(w) / float64(maxWidth)
	nw := maxWidth
	nh := int(math.Round(float64(h) / scale))
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))

	// 源图就是 RGBA 时直接读字节，比走 image.Image 接口快一个数量级
	src, direct := img.(*image.RGBA)

	for y := 0; y < nh; y++ {
		y0 := b.Min.Y + int(float64(y)*scale)
		y1 := b.Min.Y + int(float64(y+1)*scale)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > b.Max.Y {
			y1 = b.Max.Y
		}

		for x := 0; x < nw; x++ {
			x0 := b.Min.X + int(float64(x)*scale)
			x1 := b.Min.X + int(float64(x+1)*scale)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if x1 > b.Max.X {
				x1 = b.Max.X
			}

			var sr, sg, sb, n uint64
			if direct {
				for sy := y0; sy < y1; sy++ {
					off := src.PixOffset(x0, sy)
					for sx := x0; sx < x1; sx++ {
						sr += uint64(src.Pix[off+0])
						sg += uint64(src.Pix[off+1])
						sb += uint64(src.Pix[off+2])
						off += 4
					}
					n += uint64(x1 - x0)
				}
			} else {
				for sy := y0; sy < y1; sy++ {
					for sx := x0; sx < x1; sx++ {
						r, g, bl, _ := img.At(sx, sy).RGBA()
						sr += uint64(r >> 8)
						sg += uint64(g >> 8)
						sb += uint64(bl >> 8)
					}
					n += uint64(x1 - x0)
				}
			}
			if n == 0 {
				n = 1
			}

			off := dst.PixOffset(x, y)
			dst.Pix[off+0] = uint8(sr / n)
			dst.Pix[off+1] = uint8(sg / n)
			dst.Pix[off+2] = uint8(sb / n)
			dst.Pix[off+3] = 0xFF
		}
	}
	return dst
}
