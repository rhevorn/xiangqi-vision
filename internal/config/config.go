// Package config 负责读写 YAML 配置。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"xiangqi-vision/internal/vision"
)

// Config 是整个程序的配置。
type Config struct {
	Device  Device  `yaml:"device"`
	Capture Capture `yaml:"capture"`
	Board   Board   `yaml:"board"`
	Vision  Vision  `yaml:"vision"`
	Engine  Engine  `yaml:"engine"`
	Log     Log     `yaml:"log"`
	Debug   Debug   `yaml:"debug"`
}

// Device 是安卓设备连接配置。
type Device struct {
	// ADBPath 是 adb 可执行文件路径，留空则自动探测（PATH、Android SDK 常见位置）。
	ADBPath string `yaml:"adb_path"`
	// Serial 是设备序列号，只有一台设备时可留空。
	Serial string `yaml:"adb_serial"`
}

// Capture 是抓屏配置。
type Capture struct {
	// FPS 为截图频率。象棋是回合制，2~5 已经足够。
	FPS int `yaml:"fps"`
	// FramesDir 用本地图片目录代替真机，按文件名顺序回放（干跑/回归用）。
	FramesDir string `yaml:"frames_dir"`
	// FrameFile 用固定的单张图片代替真机。
	FrameFile string `yaml:"frame_file"`
	// RepeatEach 为帧序列中每张图重复返回的次数，用于模拟真实抓屏的重复采样。
	RepeatEach int `yaml:"repeat_each"`
}

// Board 是棋盘在画面中的位置，由校准写入。
type Board struct {
	X      int `yaml:"x"`
	Y      int `yaml:"y"`
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
}

// Rect 转成视觉包使用的矩形。
func (b Board) Rect() vision.Rect {
	return vision.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height}
}

// Configured 报告棋盘是否已经校准过。
func (b Board) Configured() bool { return b.Width > 0 && b.Height > 0 }

// Vision 是视觉识别参数。
type Vision struct {
	// ROISize 为取样区域边长（像素），0 表示按格距自动推算。
	ROISize int `yaml:"roi_size"`
	// DiffThreshold 为判定"某个格点发生了变化"的平均灰度差阈值。
	DiffThreshold float64 `yaml:"diff_threshold"`
	// StableFrames 为判定棋局落定所需的连续静止帧数，用来滤掉走子动画。
	StableFrames int `yaml:"stable_frames"`
	// MaxCandidateCells 为参与组合成候选走法的最大格点数。
	MaxCandidateCells int `yaml:"max_candidate_cells"`
	// RetryFrames 为推断失败时额外抓取的帧数。
	// 稳定判定偶尔会落在动画的最后一帧上，多抓几帧通常就能拿到真正的终局画面。
	RetryFrames int `yaml:"retry_frames"`
}

// Engine 是引擎配置。
type Engine struct {
	Path       string `yaml:"path"`
	Threads    int    `yaml:"threads"`
	HashMB     int    `yaml:"hash_mb"`
	MoveTimeMS int    `yaml:"movetime_ms"`
	MultiPV    int    `yaml:"multipv"`
	// EvalFile 为 NNUE 权重路径，留空则用引擎同目录下的 pikafish.nnue。
	EvalFile string `yaml:"eval_file"`
}

// MoveTime 返回思考时间。
func (e Engine) MoveTime() time.Duration { return time.Duration(e.MoveTimeMS) * time.Millisecond }

// Log 是日志配置。
type Log struct {
	// Level 为 debug/info/warn/error。
	Level string `yaml:"level"`
	// File 为日志文件路径，留空则只输出到终端。
	File string `yaml:"file"`
}

// Debug 是排查辅助配置。
type Debug struct {
	// Dir 为异常截图与中间产物的存放目录。
	Dir string `yaml:"dir"`
	// SaveFrames 开启后会把每一帧都存下来，仅用于深度排查。
	SaveFrames bool `yaml:"save_frames"`
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		Device: Device{},
		Capture: Capture{
			FPS:        5,
			RepeatEach: 3,
		},
		Vision: Vision{
			ROISize:           0, // 自动
			DiffThreshold:     20,
			StableFrames:      3,
			MaxCandidateCells: 4,
			RetryFrames:       3,
		},
		Engine: Engine{
			Path:       "./bin/pikafish",
			Threads:    4,
			HashMB:     256,
			MoveTimeMS: 500,
			MultiPV:    3,
		},
		Log:   Log{Level: "info"},
		Debug: Debug{Dir: "debug"},
	}
}

// DefaultPath 返回默认配置文件路径。
func DefaultPath() string { return "configs/config.yaml" }

// Load 读取配置文件；文件不存在时返回默认配置且不报错。
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置 %s 失败: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}
	return cfg, nil
}

// Save 把配置写回文件。
func (c *Config) Save(path string) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	const header = "# Android 中国象棋 AI 辅助系统配置\n# 棋盘区域由 `assistant calibrate` 自动写入\n\n"
	return os.WriteFile(path, append([]byte(header), data...), 0o644)
}

// WithBoard 返回一份把棋盘区域替换为新值的配置副本。
func (c *Config) WithBoard(r vision.Rect) *Config {
	out := *c
	out.Board = Board{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
	return &out
}

// Describe 返回便于日志输出的配置摘要。
func (c *Config) Describe() string {
	return fmt.Sprintf("fps=%d 棋盘=(%d,%d %dx%d) 阈值=%.0f 稳定帧=%d 引擎=%s %dms",
		c.Capture.FPS, c.Board.X, c.Board.Y, c.Board.Width, c.Board.Height,
		c.Vision.DiffThreshold, c.Vision.StableFrames, c.Engine.Path, c.Engine.MoveTimeMS)
}
