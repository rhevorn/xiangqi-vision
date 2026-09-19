// Package webui 提供一个网页仪表盘，把识别到的局面与引擎建议实时画在浏览器里。
//
// 相比终端输出，网页能做几件终端做不到的事：把棋盘真正画出来、把最佳走法画成
// 箭头、让引擎的渐进式搜索结果平滑刷新，以及不用记命令就能重新同步。
package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"xiangqi-vision/internal/analyzer"
	"xiangqi-vision/internal/app"
	"xiangqi-vision/internal/capture"
	"xiangqi-vision/internal/config"
	"xiangqi-vision/internal/engine"
	"xiangqi-vision/internal/game"
	"xiangqi-vision/internal/vision"
)

// Options 是网页服务的配置。
type Options struct {
	Cfg      *config.Config
	CfgPath  string
	Capturer capture.Capturer
	Engine   engine.Engine
	Logger   *slog.Logger
}

// Server 提供网页仪表盘与校准页面。
type Server struct {
	cfgPath string
	log     *slog.Logger
	src     capture.Capturer
	eng     engine.Engine
	tap     *FrameTap

	mu       sync.Mutex
	cfg      *config.Config
	cal      *vision.Calibration
	ctrl     *app.Controller
	status   *app.Status
	subs     map[chan []byte]struct{}
	listener net.Listener
	cancel   context.CancelFunc
	onCalibr func(vision.Rect)

	// shotMu 单独保护截图缓存：抓屏可能耗时几百毫秒，不该占着主锁。
	shotMu sync.Mutex
	shot   *image.RGBA
}

// New 创建网页服务。传入的 Capturer 会被包装，以便顺便产出预览缩略图。
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		cfgPath: opts.CfgPath,
		cfg:     opts.Cfg,
		log:     log.With("component", "webui"),
		src:     opts.Capturer,
		eng:     opts.Engine,
		tap:     NewFrameTap(opts.Capturer, 320),
		subs:    make(map[chan []byte]struct{}),
	}
}

// Calibrated 报告棋盘是否已经校准过。
func (s *Server) Calibrated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Board.Configured()
}

// OnCalibrated 注册校准完成后的回调。
func (s *Server) OnCalibrated(fn func(vision.Rect)) {
	s.mu.Lock()
	s.onCalibr = fn
	s.mu.Unlock()
}

// Listen 开始监听并返回实际地址。
func (s *Server) Listen(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	return ln.Addr().String(), nil
}

// Serve 在已监听的端口上提供服务，直到 ctx 取消。
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if ln == nil {
		return errors.New("尚未调用 Listen")
	}

	srv := &http.Server{Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler 返回路由表。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/calibrate", s.handleCalibratePage)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/frame", s.handleFrame)
	mux.HandleFunc("/api/screenshot", s.handleScreenshot)
	mux.HandleFunc("/api/calibrate", s.handleCalibrateSave)
	mux.HandleFunc("/api/resync", s.handleResync)
	mux.HandleFunc("/api/pause", s.handlePause)
	return mux
}

// StartController 构建校准并启动主循环。
//
// 若主循环已在运行（例如用户改了校准参数后重新保存），会先把旧的停掉——
// 校准变了，旧控制器手里的坐标映射就作废了。
func (s *Server) StartController(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel, s.ctrl, s.status = nil, nil, nil
	}
	cfg := s.cfg
	s.mu.Unlock()

	if !cfg.Board.Configured() {
		return errors.New("尚未校准棋盘")
	}
	cal, err := vision.NewCalibration(cfg.Board.Rect(), cfg.Vision.ROISize)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	ctrl := app.New(cfg, s.tap, cal, s.eng, app.Hooks{
		OnStatus: s.onStatus,
	}, s.log)

	s.mu.Lock()
	s.cal, s.ctrl, s.cancel = cal, ctrl, cancel
	s.mu.Unlock()

	go func() {
		if err := ctrl.Run(runCtx); err != nil {
			s.log.Warn("主循环退出", "err", err)
		}
	}()
	s.log.Info("主循环已启动", "校准", cal.String())
	return nil
}

// Controller 返回当前控制器，未启动时为 nil。
func (s *Server) Controller() *app.Controller {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctrl
}

// FrameTap 返回画面包装器，供调用方替换或检查来源。
func (s *Server) FrameTap() *FrameTap { return s.tap }

// ---------------------------------------------------------------------------
// 路由
// ---------------------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.Calibrated() {
		http.Redirect(w, r, "/calibrate", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, dashboardHTML)
}

func (s *Server) handleCalibratePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, CalibratePage("/api/screenshot", "/"))
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	img, err := s.screenshot(r.URL.Query().Has("refresh"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	if err := png.Encode(w, img); err != nil {
		s.log.Warn("输出截图失败", "err", err)
	}
}

func (s *Server) handleCalibrateSave(w http.ResponseWriter, r *http.Request) {
	img, err := s.screenshot(false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rect, err := ParseCalibrationRect(r, img)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.cfg = s.cfg.WithBoard(rect)
	cfg := s.cfg
	cb := s.onCalibr
	s.mu.Unlock()

	if err := cfg.Save(s.cfgPath); err != nil {
		http.Error(w, "写入配置失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	cal, err := vision.NewCalibration(rect, cfg.Vision.ROISize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Info("校准完成", "棋盘", rect, "取样边长", cal.ROISize)

	// 输出一张带网格的校验图供事后复核
	preview := vision.DrawCalibration(img, cal)
	_ = capture.SavePNG(s.cfg.Debug.Dir+"/calibration.png", preview)

	if cb != nil {
		cb(rect)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	data := s.currentJSON()
	if data == nil {
		data = []byte("{}")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	thumb, serial := s.tap.Thumbnail()
	if thumb == nil {
		http.Error(w, "还没有抓到画面", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Version", fmt.Sprint(serial))
	w.Write(thumb)
}

func (s *Server) handleResync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	if ctrl := s.Controller(); ctrl != nil {
		ctrl.Resync()
	}
	fmt.Fprint(w, `{"ok":true}`)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "需要 POST", http.StatusMethodNotAllowed)
		return
	}
	pause := r.URL.Query().Get("paused") != "false"
	if ctrl := s.Controller(); ctrl != nil {
		ctrl.SetPaused(pause)
	}
	fmt.Fprint(w, `{"ok":true}`)
}

// handleEvents 用 Server-Sent Events 把状态推给浏览器。
//
// 选 SSE 而不是 WebSocket：只有服务端往客户端单向推，标准库就能实现，
// 浏览器端一个 EventSource 搞定，断线还会自动重连。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "当前连接不支持流式响应", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// 新连上的客户端立刻拿一份当前状态，不用等下一次变化
	if data := s.currentJSON(); data != nil {
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
	}
	flusher.Flush()

	ch := s.subscribe()
	defer s.unsubscribe(ch)

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case data := <-ch:
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ---------------------------------------------------------------------------
// 内部
// ---------------------------------------------------------------------------

func (s *Server) screenshot(refresh bool) (*image.RGBA, error) {
	s.shotMu.Lock()
	defer s.shotMu.Unlock()

	if s.shot != nil && !refresh {
		return s.shot, nil
	}
	// 校准阶段控制器还没启动，这里独占抓屏，不会和主循环抢
	img, err := s.src.Capture(context.Background())
	if err != nil {
		return nil, fmt.Errorf("抓取画面失败: %w", err)
	}
	s.shot = img
	return img, nil
}

func (s *Server) onStatus(st *app.Status) {
	data, err := json.Marshal(s.toJSON(st))
	if err != nil {
		s.log.Warn("序列化状态失败", "err", err)
		return
	}
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	s.broadcast(data)
}

func (s *Server) currentJSON() []byte {
	s.mu.Lock()
	st := s.status
	s.mu.Unlock()
	if st == nil {
		return nil
	}
	data, err := json.Marshal(s.toJSON(st))
	if err != nil {
		return nil
	}
	return data
}

func (s *Server) subscribe() chan []byte {
	ch := make(chan []byte, 8)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Server) unsubscribe(ch chan []byte) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
}

func (s *Server) broadcast(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- data:
		default:
			// 订阅者跟不上就丢这一帧，绝不能反过来拖慢主循环
		}
	}
}

// ---------------------------------------------------------------------------
// 传给浏览器的 JSON
// ---------------------------------------------------------------------------

type cellJSON struct {
	// N 是棋子汉字名，空位为 ""。
	N string `json:"n"`
	// R 表示是否红方。汉字名不足以区分红黑——"马"、"车"、"炮" 两边写法相同。
	R bool `json:"r"`
}

type candidateJSON struct {
	Notation string   `json:"notation"`
	From     []int    `json:"from"`
	To       []int    `json:"to"`
	Score    string   `json:"score"`
	Neg      bool     `json:"neg"`
	IsMate   bool     `json:"isMate"`
	PV       []string `json:"pv"`
}

type analysisJSON struct {
	BestMove  string          `json:"bestMove"`
	From      []int           `json:"from,omitempty"`
	To        []int           `json:"to,omitempty"`
	Score     string          `json:"score"`
	Neg       bool            `json:"neg"`
	IsMate    bool            `json:"isMate"`
	Depth     int             `json:"depth"`
	ElapsedMS int64           `json:"elapsedMs"`
	Nodes     string          `json:"nodes"`
	Cands     []candidateJSON `json:"candidates"`
	Final     bool            `json:"final"`
}

type statusJSON struct {
	State      string        `json:"state"`
	Board      [][]cellJSON  `json:"board"`
	FEN        string        `json:"fen"`
	SideToMove string        `json:"sideToMove"`
	SideRed    bool          `json:"sideRed"`
	Result     string        `json:"result"`
	Moves      []string      `json:"moves"`
	LastFrom   []int         `json:"lastFrom,omitempty"`
	LastTo     []int         `json:"lastTo,omitempty"`
	Detected   string        `json:"detected"`
	Desynced   bool          `json:"desynced"`
	Paused     bool          `json:"paused"`
	Engine     string        `json:"engine"`
	Notice     string        `json:"notice"`
	Analysis   *analysisJSON `json:"analysis,omitempty"`
	FrameVer   int64         `json:"frameVer"`
	Calibrated bool          `json:"calibrated"`
}

func (s *Server) toJSON(st *app.Status) *statusJSON {
	board := make([][]cellJSON, game.Rows)
	for r := 0; r < game.Rows; r++ {
		row := make([]cellJSON, game.Cols)
		for c := 0; c < game.Cols; c++ {
			p := st.Board[r][c]
			if p.IsEmpty() {
				continue
			}
			row[c] = cellJSON{N: p.Name(), R: p.Color == game.Red}
		}
		board[r] = row
	}

	out := &statusJSON{
		State:      st.State.String(),
		Board:      board,
		FEN:        st.FEN,
		SideToMove: st.SideToMove.String(),
		SideRed:    st.SideToMove == game.Red,
		Result:     st.Result.String(),
		Moves:      st.Moves,
		Detected:   st.Detected,
		Desynced:   st.Desynced,
		Paused:     st.Paused,
		Engine:     st.Engine,
		Notice:     st.Notice,
		Calibrated: true,
	}
	if out.Moves == nil {
		out.Moves = []string{}
	}
	if st.HasLast {
		out.LastFrom = []int{st.LastMove.From.Row, st.LastMove.From.Col}
		out.LastTo = []int{st.LastMove.To.Row, st.LastMove.To.Col}
	}
	if a := st.Analysis; a != nil {
		out.Analysis = toAnalysisJSON(a, st.AnalysisFinal)
	}
	_, out.FrameVer = s.tap.Thumbnail()
	return out
}

func toAnalysisJSON(a *analyzer.Result, final bool) *analysisJSON {
	out := &analysisJSON{
		BestMove:  a.BestNotation,
		Score:     a.Score.String(),
		Neg:       a.Score.CP < 0 || (a.Score.IsMate && a.Score.Mate < 0),
		IsMate:    a.Score.IsMate,
		Depth:     a.Depth,
		ElapsedMS: a.Elapsed.Milliseconds(),
		Nodes:     humanNodes(a.Nodes),
		Final:     final,
	}
	if !a.BestMove.IsZero() {
		out.From = []int{a.BestMove.From.Row, a.BestMove.From.Col}
		out.To = []int{a.BestMove.To.Row, a.BestMove.To.Col}
	}
	for _, c := range a.Candidates {
		out.Cands = append(out.Cands, candidateJSON{
			Notation: c.Notation,
			From:     []int{c.Move.From.Row, c.Move.From.Col},
			To:       []int{c.Move.To.Row, c.Move.To.Col},
			Score:    c.Score.String(),
			Neg:      c.Score.CP < 0 || (c.Score.IsMate && c.Score.Mate < 0),
			IsMate:   c.Score.IsMate,
			PV:       c.PV,
		})
	}
	return out
}

func humanNodes(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprint(n)
	}
}
