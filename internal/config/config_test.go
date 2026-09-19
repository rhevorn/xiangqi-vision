package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/vision"
)

// 配置文件必须能原样读回来。
//
// 这里特别覆盖 board.y 这个字段：YAML 1.1 会把裸写的 y 当成布尔真值，
// 因此 yaml.v3 序列化时会把它写成 "y"。只要往返结果一致就没问题，
// 但这个细节值得用测试钉住。
func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	orig := config.Default()
	orig.Board = config.Board{X: 120, Y: 310, Width: 870, Height: 970}
	orig.Engine.Path = "./bin/pikafish"
	orig.Engine.MoveTimeMS = 750
	orig.Vision.StableFrames = 4
	orig.Device.Serial = "emulator-5554"

	if err := orig.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Board != orig.Board {
		t.Errorf("board 往返不一致\n got: %+v\nwant: %+v", got.Board, orig.Board)
	}
	if got.Engine != orig.Engine {
		t.Errorf("engine 往返不一致\n got: %+v\nwant: %+v", got.Engine, orig.Engine)
	}
	if got.Vision != orig.Vision {
		t.Errorf("vision 往返不一致\n got: %+v\nwant: %+v", got.Vision, orig.Vision)
	}
	if got.Device != orig.Device {
		t.Errorf("device 往返不一致\n got: %+v\nwant: %+v", got.Device, orig.Device)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "不存在.yaml"))
	if err != nil {
		t.Fatalf("文件不存在时不应报错: %v", err)
	}
	if cfg.Capture.FPS != 5 || cfg.Vision.StableFrames != 3 {
		t.Errorf("应回落到默认配置，实际 %+v", cfg)
	}
	if cfg.Board.Configured() {
		t.Error("默认配置里棋盘应当是未校准状态")
	}
}

func TestBoardConfiguredAndRect(t *testing.T) {
	if (config.Board{}).Configured() {
		t.Error("零值 board 不应算作已校准")
	}

	b := config.Board{X: 10, Y: 20, Width: 800, Height: 900}
	if !b.Configured() {
		t.Error("有宽高的 board 应算作已校准")
	}
	if got, want := b.Rect(), (vision.Rect{X: 10, Y: 20, Width: 800, Height: 900}); got != want {
		t.Errorf("Rect() = %+v，期望 %+v", got, want)
	}
}

// 手工写一份文档 §19 风格的配置，确认能被正确解析。
func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	const text = `
device:
  adb_serial: "ABC123"

capture:
  fps: 5

board:
  x: 120
  y: 310
  width: 870
  height: 970

vision:
  roi_size: 36
  diff_threshold: 20
  stable_frames: 3

engine:
  path: ./bin/pikafish
  threads: 4
  hash_mb: 256
  movetime_ms: 500
  multipv: 3
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(text)), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Device.Serial != "ABC123" {
		t.Errorf("device.adb_serial = %q", cfg.Device.Serial)
	}
	if cfg.Board.Y != 310 || cfg.Board.Width != 870 {
		t.Errorf("board 解析错误: %+v", cfg.Board)
	}
	if cfg.Vision.ROISize != 36 || cfg.Vision.DiffThreshold != 20 {
		t.Errorf("vision 解析错误: %+v", cfg.Vision)
	}
	if cfg.Engine.MoveTime() != 500*1e6 {
		t.Errorf("engine.movetime_ms = %d", cfg.Engine.MoveTimeMS)
	}
	// 未出现在文件里的字段应保留默认值
	if cfg.Vision.RetryFrames != 3 {
		t.Errorf("未指定的 retry_frames 应保留默认值 3，实际 %d", cfg.Vision.RetryFrames)
	}
}
