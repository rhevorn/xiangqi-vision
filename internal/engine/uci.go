package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xiangqi-vision/internal/game"
)

// errEngineGone 表示引擎子进程已退出，管道被关闭。
var errEngineGone = errors.New("引擎进程已退出")

// Options 是启动 UCI 引擎的配置，对应《技术方案》§14 与 §19 的 engine 段。
type Options struct {
	// Path 是可执行文件路径，如 ./bin/pikafish。
	Path string
	// Threads 为搜索线程数。
	Threads int
	// HashMB 为置换表大小（MB）。
	HashMB int
	// MultiPV 为默认候选走法数量。
	MultiPV int
	// EvalFile 为 NNUE 权重路径，留空则使用引擎内建默认值（即与可执行文件同目录的 pikafish.nnue）。
	EvalFile string
	// Logger 为可选的日志器。
	Logger *slog.Logger
}

// UCIEngine 是一个通过 UCI 协议驱动的引擎子进程。
type UCIEngine struct {
	opts Options
	log  *slog.Logger

	// mu 保证同一时刻只有一次搜索在跑：UCI 是有状态的单会话协议。
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	w      *bufio.Writer
	lines  chan string
	ready  bool
	curMPV int
	name   string
}

// NewUCIEngine 创建一个引擎客户端，但不会立即启动进程。
func NewUCIEngine(opts Options) *UCIEngine {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &UCIEngine{opts: opts, log: log.With("component", "engine")}
}

// Name 返回引擎自报的名称，未启动时返回可执行文件路径。
func (e *UCIEngine) Name() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.name != "" {
		return e.name
	}
	return filepath.Base(e.opts.Path)
}

// Analyze 分析局面。返回的评分以 b.SideToMove() 的视角给出。
func (e *UCIEngine) Analyze(ctx context.Context, b *game.Board, opts AnalyzeOptions) (*Analysis, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.ensureStarted(ctx); err != nil {
		return nil, err
	}

	res, err := e.search(ctx, b, opts)
	if err == nil {
		return res, nil
	}
	if !errors.Is(err, errEngineGone) || ctx.Err() != nil {
		return nil, err
	}

	// 引擎进程意外退出：重启后重试一次（§23「引擎异常可自动重启」）
	e.log.Warn("引擎进程异常退出，正在重启后重试", "err", err)
	e.stop()
	if err := e.ensureStarted(ctx); err != nil {
		return nil, err
	}
	return e.search(ctx, b, opts)
}

// Close 关闭引擎进程。
func (e *UCIEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.ready {
		return nil
	}
	_ = e.send("quit")
	e.stop()
	return nil
}

// ensureStarted 在需要时启动子进程并完成 UCI 握手。调用方必须持有 e.mu。
func (e *UCIEngine) ensureStarted(ctx context.Context) error {
	if e.ready {
		return nil
	}
	e.stop()
	if err := e.start(); err != nil {
		return err
	}

	hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := e.handshake(hctx); err != nil {
		e.stop()
		return fmt.Errorf("引擎握手失败（%s）: %w", e.opts.Path, err)
	}
	e.ready = true
	e.log.Info("引擎已就绪", "name", e.name, "path", e.opts.Path)
	return nil
}

// start 拉起子进程并接好管道。调用方必须持有 e.mu。
func (e *UCIEngine) start() error {
	path, err := filepath.Abs(e.opts.Path)
	if err != nil {
		return err
	}

	cmd := exec.Command(path)
	// 引擎按工作目录查找 NNUE 权重文件，因此把工作目录设成可执行文件所在目录。
	cmd.Dir = filepath.Dir(path)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = &logWriter{log: e.log}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动引擎失败: %w", err)
	}

	e.cmd = cmd
	e.stdin = stdin
	e.w = bufio.NewWriter(stdin)
	e.lines = make(chan string, 4096)
	e.name = ""
	e.curMPV = 0
	go pumpLines(stdout, e.lines)
	return nil
}

// stop 杀掉子进程并复位连接状态。调用方必须持有 e.mu。
func (e *UCIEngine) stop() {
	if e.stdin != nil {
		_ = e.stdin.Close()
		e.stdin = nil
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}
	e.cmd = nil
	e.w = nil
	e.lines = nil
	e.ready = false
	e.curMPV = 0
}

// handshake 完成 uci 握手并下发选项。调用方必须持有 e.mu。
func (e *UCIEngine) handshake(ctx context.Context) error {
	if err := e.send("uci"); err != nil {
		return err
	}
	if err := e.waitFor(ctx, "uciok"); err != nil {
		return err
	}

	type option struct {
		name  string
		value string
	}
	var opts []option
	if e.opts.Threads > 0 {
		opts = append(opts, option{"Threads", strconv.Itoa(e.opts.Threads)})
	}
	if e.opts.HashMB > 0 {
		opts = append(opts, option{"Hash", strconv.Itoa(e.opts.HashMB)})
	}
	if e.opts.EvalFile != "" {
		opts = append(opts, option{"EvalFile", e.opts.EvalFile})
	}
	if e.opts.MultiPV > 0 {
		opts = append(opts, option{"MultiPV", strconv.Itoa(e.opts.MultiPV)})
	}
	for _, o := range opts {
		if err := e.send(fmt.Sprintf("setoption name %s value %s", o.name, o.value)); err != nil {
			return err
		}
	}
	if e.opts.MultiPV > 0 {
		e.curMPV = e.opts.MultiPV
	}

	if err := e.send("isready"); err != nil {
		return err
	}
	return e.waitFor(ctx, "readyok")
}

// waitFor 读取输出直到出现指定标记。调用方必须持有 e.mu。
func (e *UCIEngine) waitFor(ctx context.Context, token string) error {
	for {
		line, err := e.readLine(ctx)
		if err != nil {
			return err
		}
		if line == token {
			return nil
		}
		if name, ok := strings.CutPrefix(line, "id name "); ok {
			e.name = strings.TrimSpace(name)
		}
	}
}

// search 执行一次完整搜索。调用方必须持有 e.mu。
func (e *UCIEngine) search(ctx context.Context, b *game.Board, opts AnalyzeOptions) (*Analysis, error) {
	if opts.MultiPV > 0 && opts.MultiPV != e.curMPV {
		if err := e.send(fmt.Sprintf("setoption name MultiPV value %d", opts.MultiPV)); err != nil {
			return nil, err
		}
		if err := e.send("isready"); err != nil {
			return nil, err
		}
		if err := e.waitFor(ctx, "readyok"); err != nil {
			return nil, err
		}
		e.curMPV = opts.MultiPV
	}

	if err := e.send(b.PositionCommand()); err != nil {
		return nil, err
	}
	if err := e.send(goCommand(opts)); err != nil {
		return nil, err
	}

	an := &Analysis{}
	byMultiPV := map[int]*Candidate{}

	for {
		line, err := e.readLine(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// 调用方取消了本次分析。必须让引擎停下来并把剩余输出排空，
				// 否则残留的 info/bestmove 会串到下一次搜索里。
				_ = e.send("stop")
				dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				_ = e.drainUntilBestMove(dctx)
				cancel()
				return nil, ctx.Err()
			}
			return nil, err
		}

		switch {
		case strings.HasPrefix(line, "bestmove"):
			final := buildAnalysis(an, byMultiPV)
			moveStr, ponderStr := parseBestMove(line)
			if moveStr == "" {
				return final, ErrNoLegalMove
			}
			m, err := game.ParseMove(moveStr)
			if err != nil {
				return nil, fmt.Errorf("引擎返回了无法解析的走法 %q: %w", moveStr, err)
			}
			final.BestMove = m
			if ponderStr != "" {
				if p, err := game.ParseMove(ponderStr); err == nil {
					final.Ponder, final.HasPonder = p, true
				}
			}
			return final, nil

		case strings.HasPrefix(line, "info "):
			info := parseInfoLine(line)
			if info == nil || len(info.PV) == 0 {
				continue // 引擎启动瞬间会发一条空 pv 的 info，忽略
			}
			byMultiPV[info.MultiPV] = &Candidate{
				Move:  info.PV[0],
				Score: info.Score,
				Depth: info.Depth,
				PV:    info.PV,
			}
			an.Depth = max(an.Depth, info.Depth)
			an.SelDepth = max(an.SelDepth, info.SelDepth)
			an.Nodes = info.Nodes
			an.NPS = info.NPS
			an.Elapsed = time.Duration(info.TimeMs) * time.Millisecond
			if info.MultiPV == 1 {
				an.Score = info.Score
				if opts.OnUpdate != nil {
					opts.OnUpdate(buildAnalysis(an, byMultiPV))
				}
			}

		default:
			e.log.Debug("引擎输出", "line", line)
		}
	}
}

// drainUntilBestMove 读取并丢弃输出，直到 bestmove 或超时。
// 调用方必须持有 e.mu。
func (e *UCIEngine) drainUntilBestMove(ctx context.Context) error {
	for {
		line, err := e.readLine(ctx)
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "bestmove") {
			return nil
		}
	}
}

// send 向引擎写入一行命令并立即刷新。调用方必须持有 e.mu。
func (e *UCIEngine) send(cmd string) error {
	if e.w == nil {
		return errEngineGone
	}
	e.log.Debug("发送命令", "cmd", cmd)
	if _, err := e.w.WriteString(cmd + "\n"); err != nil {
		return errEngineGone
	}
	if err := e.w.Flush(); err != nil {
		return errEngineGone
	}
	return nil
}

// readLine 读取一行引擎输出，ctx 取消时立即返回。
// 调用方必须持有 e.mu。
func (e *UCIEngine) readLine(ctx context.Context) (string, error) {
	if e.lines == nil {
		return "", errEngineGone
	}
	select {
	case line, ok := <-e.lines:
		if !ok {
			return "", errEngineGone
		}
		return line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// pumpLines 把引擎 stdout 逐行送进通道，进程退出时关闭通道。
func pumpLines(r io.Reader, out chan<- string) {
	defer close(out)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		out <- sc.Text()
	}
}

// goCommand 构造 go 命令。时间预算优先于深度。
func goCommand(opts AnalyzeOptions) string {
	switch {
	case opts.MoveTime > 0:
		return fmt.Sprintf("go movetime %d", opts.MoveTime.Milliseconds())
	case opts.Depth > 0:
		return fmt.Sprintf("go depth %d", opts.Depth)
	default:
		return "go movetime 500"
	}
}

// buildAnalysis 深拷贝当前累积的候选，按 MultiPV 顺序排列。
func buildAnalysis(an *Analysis, byMultiPV map[int]*Candidate) *Analysis {
	out := *an
	if len(byMultiPV) == 0 {
		out.Candidates = nil
		return &out
	}
	keys := make([]int, 0, len(byMultiPV))
	for k := range byMultiPV {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	out.Candidates = make([]Candidate, 0, len(keys))
	for _, k := range keys {
		c := *byMultiPV[k]
		c.PV = append([]game.Move(nil), c.PV...)
		out.Candidates = append(out.Candidates, c)
	}
	return &out
}

// parseBestMove 解析 "bestmove c3c4 ponder g6g5"。
// 返回空字符串表示引擎报告无棋可走（bestmove (none)）。
func parseBestMove(line string) (move, ponder string) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", ""
	}
	move = fields[1]
	if move == "(none)" {
		return "", ""
	}
	for i := 2; i+1 < len(fields); i++ {
		if fields[i] == "ponder" {
			ponder = fields[i+1]
			break
		}
	}
	return move, ponder
}

// infoLine 是解析后的一条 info 行。
type infoLine struct {
	Depth    int
	SelDepth int
	MultiPV  int
	Score    Score
	Nodes    int64
	NPS      int64
	TimeMs   int64
	PV       []game.Move
}

// parseInfoLine 解析引擎的 info 行，例如：
//
//	info depth 18 seldepth 33 multipv 1 score cp 30 nodes 2201775 nps 6967642 hashfull 43 tbhits 0 time 316 pv b2e2 b9c7 ...
//
// 无法识别的键会被跳过，因此对引擎版本差异是宽容的。
func parseInfoLine(line string) *infoLine {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "info" {
		return nil
	}
	info := &infoLine{MultiPV: 1}

	for i := 1; i < len(fields); i++ {
		key := fields[i]
		next := func() (string, bool) {
			if i+1 >= len(fields) {
				return "", false
			}
			i++
			return fields[i], true
		}

		switch key {
		case "depth":
			if v, ok := next(); ok {
				info.Depth = atoi(v)
			}
		case "seldepth":
			if v, ok := next(); ok {
				info.SelDepth = atoi(v)
			}
		case "multipv":
			if v, ok := next(); ok {
				info.MultiPV = max(1, atoi(v))
			}
		case "nodes":
			if v, ok := next(); ok {
				info.Nodes = atoi64(v)
			}
		case "nps":
			if v, ok := next(); ok {
				info.NPS = atoi64(v)
			}
		case "time":
			if v, ok := next(); ok {
				info.TimeMs = atoi64(v)
			}
		case "score":
			v, ok := next()
			if !ok {
				break
			}
			switch v {
			case "cp":
				if n, ok := next(); ok {
					info.Score = Score{CP: atoi(n)}
				}
			case "mate":
				if n, ok := next(); ok {
					info.Score = Score{Mate: atoi(n), IsMate: true}
				}
			case "lowerbound", "upperbound":
				// 边界值不是确定评分，跳过
			}
		case "pv":
			// pv 之后全部是走法
			for _, s := range fields[i+1:] {
				m, err := game.ParseMove(s)
				if err != nil {
					break
				}
				info.PV = append(info.PV, m)
			}
			i = len(fields)
		default:
			// hashfull / tbhits / currmove / string 等一律跳过。
			// 多值键（如 string）留下的残余 token 匹配不到任何已知键，会被安全忽略。
		}
	}
	return info
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// logWriter 把引擎 stderr 转发到日志。
type logWriter struct{ log *slog.Logger }

func (w *logWriter) Write(p []byte) (int, error) {
	if msg := strings.TrimSpace(string(p)); msg != "" {
		w.log.Debug("引擎 stderr", "msg", msg)
	}
	return len(p), nil
}
