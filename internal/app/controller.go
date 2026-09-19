// Package app 实现主循环状态机。
//
// 核心分工：
//
//	视觉只负责发现变化  →  象棋规则维护真实状态  →  Pikafish 专门负责搜索
//
// 三者之间只有一条硬性纪律：视觉推断出的走法必须先通过规则校验，否则绝不
// 更新棋盘。宁可漏掉一手，也不能让错误的识别污染内部状态。
package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"xiangqi-vision/internal/analyzer"
	"xiangqi-vision/internal/capture"
	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/engine"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/vision"
)

// State 是主循环的状态。
type State int

const (
	StateInitializing State = iota
	StateWaiting
	StateDetectingMove
	StateStabilizing
	StateAnalyzing
	StateShowingResult
	StateError
)

func (s State) String() string {
	switch s {
	case StateInitializing:
		return "初始化"
	case StateWaiting:
		return "等待对方走棋"
	case StateDetectingMove:
		return "推断走法"
	case StateStabilizing:
		return "等待画面稳定"
	case StateAnalyzing:
		return "引擎分析中"
	case StateShowingResult:
		return "展示结果"
	case StateError:
		return "异常"
	}
	return "未知"
}

// Hooks 是控制器向 UI 汇报的回调，未设置的会被跳过。
//
// 所有回调都在主循环所在的 goroutine 上被调用，因此回调里可以安全地读取
// 传进来的值；但不要反过来调用控制器的方法，那会和主循环抢状态。
type Hooks struct {
	// OnState 在状态变化时被调用。
	OnState func(State)
	// OnDetected 在确认对方走子后、开始分析之前被调用。
	OnDetected func(move game.Move, notation string)
	// OnProgress 在引擎搜到更深一层时被调用，用于渐进式刷新。
	OnProgress func(*analyzer.Result)
	// OnResult 在分析完成时被调用。
	OnResult func(*analyzer.Result)
	// OnStatus 在棋盘、状态或分析结果发生变化时被调用，携带一份完整快照。
	// 面向网页这类需要"总是渲染完整界面"的 UI。
	OnStatus func(*Status)
}

// Controller 驱动整条链路。
type Controller struct {
	cfg   *config.Config
	cap   capture.Capturer
	cal   *vision.Calibration
	an    *analyzer.Analyzer
	log   *slog.Logger
	hooks Hooks

	board      *game.Board
	tracker    *vision.StabilityTracker
	lastStable *image.Gray
	engineName string

	state    State
	desynced bool
	frameNo  int
	lastMove game.Move
	hasLast  bool
	detected string
	notice   string
	moves    []string

	// lastResult 缓存最近一次分析结果，让每轮推出去的快照里始终带着它。
	lastResult *analyzer.Result
	lastFinal  bool

	// resyncReq 与 paused 是唯一会被跨 goroutine 触碰的状态，用原子量传递
	// 意图，真正的修改仍发生在主循环里，避免加锁。
	resyncReq atomic.Bool
	paused    atomic.Bool
}

// New 创建控制器。
func New(
	cfg *config.Config,
	cap capture.Capturer,
	cal *vision.Calibration,
	eng engine.Engine,
	hooks Hooks,
	log *slog.Logger,
) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{
		cfg: cfg,
		cap: cap,
		cal: cal,
		an: analyzer.New(eng, analyzer.Options{
			MoveTime: cfg.Engine.MoveTime(),
			MultiPV:  cfg.Engine.MultiPV,
		}),
		log:        log.With("component", "controller"),
		hooks:      hooks,
		board:      game.NewBoard(),
		engineName: eng.Name(),
		tracker: vision.NewStabilityTracker(
			cal, cfg.Vision.DiffThreshold, cfg.Vision.StableFrames),
	}
}

// Board 返回内部维护的棋盘。
func (c *Controller) Board() *game.Board { return c.board }

// State 返回当前状态。
func (c *Controller) State() State { return c.state }

// Desynced 报告是否处于失步状态。
func (c *Controller) Desynced() bool { return c.desynced }

// LastMove 返回最近确认的一步走法。
func (c *Controller) LastMove() (game.Move, bool) { return c.lastMove, c.hasLast }

// Run 启动主循环，直到 ctx 取消或帧序列播放完毕。
func (c *Controller) Run(ctx context.Context) error {
	fps := c.cfg.Capture.FPS
	if fps < 1 {
		fps = 5
	}
	interval := time.Second / time.Duration(fps)

	c.setState(StateInitializing)
	c.log.Info("开始监视画面",
		"来源", c.cap.Describe(),
		"间隔", interval,
		"校准", c.cal.String(),
		"稳定判定", c.tracker.Describe())

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("收到退出信号，停止监视")
			return nil
		case <-ticker.C:
		}

		err := c.tick(ctx)
		switch {
		case err == nil:
		case errors.Is(err, capture.ErrExhausted):
			c.log.Info("帧序列已播放完毕")
			return nil
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil
		default:
			// 单帧失败不应终止整条链路，记录后继续
			c.log.Warn("本轮处理出错", "err", err)
		}
	}
}

// tick 处理一帧。
func (c *Controller) tick(ctx context.Context) error {
	// 每轮结束都推一份快照，网页那边就能持续刷新
	defer c.publish()

	// 重新同步的请求从任意 goroutine 发来，在这里统一落地
	if c.resyncReq.Swap(false) {
		c.doResync()
	}

	img, err := c.cap.Capture(ctx)
	if err != nil {
		return err
	}
	c.frameNo++
	gray := vision.ToGray(img)

	if c.cfg.Debug.SaveFrames {
		_ = capture.SavePNG(
			filepath.Join(c.cfg.Debug.Dir, fmt.Sprintf("frame_%05d.png", c.frameNo)), img)
	}

	settled, maxDiff := c.tracker.Observe(gray)

	if c.paused.Load() {
		// 暂停期间不做任何推断，但让基线跟住当前画面，这样恢复时不会把暂停
		// 期间的画面变化误判成走子。代价是暂停期间的走子不会被记录。
		c.lastStable = gray
		return nil
	}

	if !settled {
		if maxDiff >= c.cfg.Vision.DiffThreshold {
			c.setState(StateStabilizing)
		}
		return nil
	}

	// 画面刚刚静止下来。首次静止只用来建立基线：MVP 只支持从标准初始
	// 局面启动，因此第一帧对应的就是初始局面。
	if c.lastStable == nil {
		c.lastStable = gray
		c.setState(StateWaiting)
		c.log.Info("已建立画面基线，开始等待走子",
			"前提", "手机画面此刻必须是标准初始局面（MVP 只支持从开局跟踪）")
		return nil
	}

	diffs := vision.DiffCells(c.lastStable, gray, c.cal)
	if vision.MaxCellDiff(diffs) < c.cfg.Vision.DiffThreshold {
		return nil // 没有实质变化
	}

	c.setState(StateDetectingMove)
	move, ok := c.detectMove(diffs)
	if !ok {
		// 可能是稳定判定放行了动画的最后一帧：再抓几帧，拿同一基线重新比对
		move, gray, ok = c.retryDetect(ctx)
		if !ok {
			return c.handleDesync(img, gray, diffs)
		}
		if err := c.acceptMove(move, diffs); err != nil {
			return err
		}
		c.tracker.Reset(gray)
		c.lastStable = gray
		return c.analyze(ctx)
	}

	if err := c.acceptMove(move, diffs); err != nil {
		return err
	}
	c.lastStable = gray
	return c.analyze(ctx)
}

// detectMove 在候选走法中找出唯一合法的那个。
//
// 视觉给出"哪两个格子在变"，规则层裁决"这是哪一步棋"。两格之间有两个
// 方向，通常只有一个方向在该局面下合法，因此能唯一定出真实走法。
func (c *Controller) detectMove(diffs []vision.CellDiff) (game.Move, bool) {
	cands := vision.DetectMoveCandidates(
		diffs, c.cfg.Vision.DiffThreshold, c.cfg.Vision.MaxCandidateCells)

	for _, cand := range cands {
		if c.board.IsLegal(cand.Move) {
			return cand.Move, true
		}
	}
	return game.Move{}, false
}

// retryDetect 重新抓取画面，仍以同一个基线做比对。
//
// 适用场景：稳定判定恰好落在动画的最后一帧上，导致起止格点算错。此时
// 再抓几帧通常就能拿到真正的终局画面。注意基线**没有**推进，所以重试
// 比对的仍然是"走子前"那一帧，语义与首次检测完全一致。
func (c *Controller) retryDetect(ctx context.Context) (game.Move, *image.Gray, bool) {
	n := c.cfg.Vision.RetryFrames
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			return game.Move{}, nil, false
		case <-time.After(time.Second / time.Duration(max(1, c.cfg.Capture.FPS))):
		}

		img, err := c.cap.Capture(ctx)
		if err != nil {
			return game.Move{}, nil, false
		}
		gray := vision.ToGray(img)
		diffs := vision.DiffCells(c.lastStable, gray, c.cal)

		if m, ok := c.detectMove(diffs); ok {
			c.log.Info("重试后成功推断出走法", "第几次", i+1)
			return m, gray, true
		}
	}
	return game.Move{}, nil, false
}

// acceptMove 把已通过合法性校验的走法写入棋盘。
func (c *Controller) acceptMove(m game.Move, diffs []vision.CellDiff) error {
	notation := game.FormatChinese(c.board, m)
	if err := c.board.Apply(m); err != nil {
		// 理论上不可达：detectMove 已经用 IsLegal 校验过
		return fmt.Errorf("应用走法失败: %w", err)
	}
	c.lastMove, c.hasLast = m, true
	c.detected, c.notice = notation, ""
	c.moves = append(c.moves, notation)
	c.desynced = false
	// 上一条建议是针对走子前的局面算的，已经作废，先清掉避免误导。
	// 新局面的分析会在几百毫秒后补上。
	c.lastResult, c.lastFinal = nil, false

	c.log.Info("检测到走子",
		"走法", notation,
		"坐标", m,
		"变化格", vision.FormatTop(diffs, 3),
		"局面", c.board.FEN())

	if c.hooks.OnDetected != nil {
		c.hooks.OnDetected(m, notation)
	}
	return nil
}

// handleDesync 处理"画面变了但推断不出合法走法"的情况。
//
// 此时绝不能更新棋盘。我们只把视觉基线推进到新画面，否则会对同一帧
// 反复报错；同时标记失步，等待人工重新同步。
func (c *Controller) handleDesync(img *image.RGBA, gray *image.Gray, diffs []vision.CellDiff) error {
	c.lastStable = gray
	c.desynced = true
	c.notice = "画面变了但推断不出合法走法，内部棋盘保持不变。请重新开局后在网页上点「重新同步」。"
	c.setState(StateError)

	path := c.saveDebugImage(img, diffs)
	c.log.Error("无法从画面变化推断出合法走法，内部棋盘保持不变",
		"变化格", vision.FormatTop(diffs, 4),
		"调试图", path,
		"提示", "请重新开局后执行重新同步")

	return nil
}

// saveDebugImage 把出错时的画面连同变化格标注一起落盘。
func (c *Controller) saveDebugImage(img *image.RGBA, diffs []vision.CellDiff) string {
	dir := c.cfg.Debug.Dir
	if dir == "" {
		return ""
	}
	annotated := vision.AnnotateDiffs(img, c.cal, diffs, 4)
	path := filepath.Join(dir, fmt.Sprintf("desync_%05d.png", c.frameNo))
	if err := capture.SavePNG(path, annotated); err != nil {
		c.log.Warn("保存调试图失败", "err", err)
		return ""
	}
	return path
}

// analyze 调用引擎分析当前局面。
func (c *Controller) analyze(ctx context.Context) error {
	if s := c.board.Status(); s != game.Ongoing {
		c.setState(StateShowingResult)
		c.log.Info("对局结束", "结果", s, "局面", c.board.FEN())
		return nil
	}

	c.setState(StateAnalyzing)
	res, err := c.an.Analyze(ctx, c.board, func(partial *analyzer.Result) {
		if c.hooks.OnProgress != nil {
			c.hooks.OnProgress(partial)
		}
		if c.hooks.OnStatus != nil {
			c.hooks.OnStatus(c.snapshot(partial, false))
		}
	})
	if err != nil {
		if errors.Is(err, engine.ErrNoLegalMove) {
			c.setState(StateShowingResult)
			c.log.Info("当前局面无合法走法")
			return nil
		}
		c.setState(StateError)
		return fmt.Errorf("引擎分析失败: %w", err)
	}

	c.setState(StateShowingResult)
	if c.hooks.OnResult != nil {
		c.hooks.OnResult(res)
	}
	if c.hooks.OnStatus != nil {
		c.hooks.OnStatus(c.snapshot(res, true))
	}
	return nil
}

// publish 把当前状态推给 UI。
func (c *Controller) publish() {
	if c.hooks.OnStatus == nil {
		return
	}
	c.hooks.OnStatus(c.snapshot(nil, false))
}

// snapshot 组装一份状态快照。
//
// analysis 非 nil 时顺带更新缓存：这样每轮循环末尾推的快照里始终带着最近一次
// 分析结果，而不会在引擎已经算完、下一轮循环刚开始时把界面上的建议抹掉。
func (c *Controller) snapshot(analysis *analyzer.Result, final bool) *Status {
	if analysis != nil {
		c.lastResult, c.lastFinal = analysis, final
	}
	return &Status{
		State:         c.state,
		Board:         c.board.Cells(),
		SideToMove:    c.board.SideToMove(),
		FEN:           c.board.FEN(),
		Result:        c.board.Status(),
		Moves:         slices.Clone(c.moves),
		LastMove:      c.lastMove,
		HasLast:       c.hasLast,
		Detected:      c.detected,
		Desynced:      c.desynced,
		Paused:        c.paused.Load(),
		Engine:        c.engineName,
		Analysis:      c.lastResult,
		AnalysisFinal: c.lastFinal,
		Notice:        c.notice,
	}
}

// Resync 请求放弃当前局面，回到标准初始局面重新开始。
//
// 这是 MVP 的失步恢复手段：完整棋盘识别留到后续版本实现。
//
// 重置动作会被推迟到主循环的下一轮开头执行，因此可以从任意 goroutine
// 安全调用（命令行里按 r、网页上点按钮走的是同一条路径）。
func (c *Controller) Resync() { c.resyncReq.Store(true) }

// SetPaused 暂停或恢复画面分析。
//
// 暂停期间仍然抓屏并跟随画面，但不再推断走法；恢复时会以当时的画面作为
// 新基线，因此暂停期间手机上的走子不会被记录。
func (c *Controller) SetPaused(p bool) {
	if c.paused.Swap(p) == p {
		return
	}
	if p {
		c.log.Info("已暂停分析")
	} else {
		c.log.Info("已恢复分析")
	}
}

// Paused 报告是否已暂停。
func (c *Controller) Paused() bool { return c.paused.Load() }

// doResync 真正执行重置。只在主循环里调用。
func (c *Controller) doResync() {
	c.board = game.NewBoard()
	c.lastStable = nil
	c.desynced = false
	c.hasLast = false
	c.lastMove = game.Move{}
	c.detected, c.notice = "", ""
	c.moves = nil
	c.lastResult, c.lastFinal = nil, false
	c.tracker = vision.NewStabilityTracker(
		c.cal, c.cfg.Vision.DiffThreshold, c.cfg.Vision.StableFrames)
	c.setState(StateInitializing)
	c.log.Info("已重新同步到标准初始局面，等待画面建立基线")
}

// setState 更新状态并通知 UI。
func (c *Controller) setState(s State) {
	if c.state == s {
		return
	}
	c.state = s
	if c.hooks.OnState != nil {
		c.hooks.OnState(s)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
