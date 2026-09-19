package webui

// dashboardHTML 是仪表盘页面。用 Go 原始字符串保存，因此内部不能出现反引号，
// JS 里一律用单引号或双引号拼字符串。
const dashboardHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>中国象棋 AI 辅助</title>
<style>
  :root {
    --bg: #101215; --panel: #1a1d22; --panel2: #22262c; --line: #2e333c;
    --fg: #e8eaed; --muted: #939aa3; --accent: #4c9aff;
    --ok: #3ddc84; --warn: #f5a623; --bad: #ff5c5c;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; background: var(--bg); color: var(--fg); min-height: 100vh;
    font: 14px/1.6 -apple-system, "PingFang SC", "Helvetica Neue", sans-serif;
  }
  header {
    display: flex; align-items: center; gap: 14px; flex-wrap: wrap;
    padding: 14px 22px; border-bottom: 1px solid var(--line); background: var(--panel);
    position: sticky; top: 0; z-index: 10;
  }
  header h1 { font-size: 16px; margin: 0; font-weight: 600; letter-spacing: .3px; }
  .pill {
    padding: 3px 12px; border-radius: 999px; font-size: 12.5px; font-weight: 600;
    background: var(--panel2); color: var(--muted); border: 1px solid var(--line);
    transition: background .2s, color .2s;
  }
  .pill.wait  { color: #9ecbff; border-color: #2c4a70; background: #16233a; }
  .pill.move  { color: #ffd479; border-color: #6b5320; background: #2e2613; }
  .pill.think { color: #d0a6ff; border-color: #513a70; background: #241a33; }
  .pill.done  { color: var(--ok);  border-color: #235b3c; background: #132a1f; }
  .pill.bad   { color: var(--bad); border-color: #6b2b2b; background: #2e1616; }
  .grow { flex: 1; }
  button {
    font: inherit; padding: 6px 15px; border-radius: 6px; cursor: pointer;
    border: 1px solid var(--line); background: var(--panel2); color: var(--fg);
  }
  button:hover { background: #2c3138; }
  button.primary { background: var(--accent); border-color: var(--accent); color: #06121f; font-weight: 600; }
  button.primary:hover { background: #6aabff; }

  main {
    display: grid; grid-template-columns: minmax(320px, 572px) minmax(300px, 1fr);
    gap: 22px; padding: 22px; align-items: start; max-width: 1500px; margin: 0 auto;
  }
  @media (max-width: 940px) { main { grid-template-columns: 1fr; } }

  .board-card {
    background: var(--panel); border: 1px solid var(--line); border-radius: 12px;
    padding: 14px; position: relative;
  }
  #board { width: 100%; height: auto; display: block; border-radius: 8px; }

  aside { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .card {
    background: var(--panel); border: 1px solid var(--line); border-radius: 12px;
    padding: 14px 16px;
  }
  .label {
    font-size: 11.5px; letter-spacing: .9px; text-transform: uppercase;
    color: var(--muted); margin-bottom: 8px;
  }
  .best-row { display: flex; align-items: baseline; gap: 14px; flex-wrap: wrap; }
  .best-move {
    font-size: 30px; font-weight: 700; letter-spacing: 3px; line-height: 1.25;
  }
  .best-move.empty { font-size: 18px; color: var(--muted); letter-spacing: 0; font-weight: 400; }
  .score { font-size: 21px; font-weight: 700; font-variant-numeric: tabular-nums; }
  .score.pos { color: var(--ok); } .score.neg { color: var(--bad); } .score.mate { color: #ffb84d; }
  .meta { color: var(--muted); font-size: 12.5px; margin-top: 8px; font-variant-numeric: tabular-nums; }
  .meta span + span::before { content: ' · '; }

  ol.cands { list-style: none; margin: 0; padding: 0; }
  ol.cands li {
    padding: 7px 9px; border-radius: 7px; cursor: pointer; border: 1px solid transparent;
    display: grid; grid-template-columns: 20px 1fr auto; gap: 9px; align-items: baseline;
  }
  ol.cands li:hover { background: var(--panel2); }
  ol.cands li.sel { background: #16233a; border-color: #2c4a70; }
  ol.cands .idx { color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
  ol.cands .nt { font-weight: 600; letter-spacing: 1.5px; }
  ol.cands .sc { font-variant-numeric: tabular-nums; font-size: 13px; }
  ol.cands .pv {
    grid-column: 2 / -1; color: var(--muted); font-size: 12px;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  ol.cands:empty::after { content: '—'; color: var(--muted); }

  .moves { display: flex; flex-wrap: wrap; gap: 5px 12px; font-size: 13px; max-height: 148px; overflow-y: auto; }
  .moves .mv { color: var(--muted); }
  .moves .mv b { color: var(--fg); font-weight: 600; letter-spacing: 1px; }
  .moves .mv.r b { color: #ff8a80; }
  .moves .mv.b b { color: #90caf9; }
  .moves:empty::after { content: '还没有走子'; color: var(--muted); }

  .frame-row { display: flex; gap: 12px; align-items: flex-start; }
  #thumb {
    width: 92px; border-radius: 6px; border: 1px solid var(--line); display: block;
    background: #000; flex: none;
  }
  .frame-info { color: var(--muted); font-size: 12px; min-width: 0; }
  .frame-info code { color: var(--fg); font-size: 11.5px; word-break: break-all; }

  .notice {
    margin-top: 12px; padding: 10px 13px; border-radius: 8px; font-size: 13px;
    background: #2e1616; border: 1px solid #6b2b2b; color: #ffb3b3;
  }
  [hidden] { display: none !important; }
</style>
</head>
<body>
<header>
  <h1>中国象棋 AI 辅助</h1>
  <span class="pill" id="state">连接中…</span>
  <span class="grow"></span>
  <button id="pause">暂停</button>
  <button id="resync" class="primary">重新同步</button>
</header>

<main>
  <div class="board-card">
    <canvas id="board" width="572" height="634"></canvas>
    <div class="notice" id="notice" hidden></div>
  </div>

  <aside>
    <div class="card">
      <div class="label">最佳走法</div>
      <div class="best-row">
        <div class="best-move empty" id="best">等待识别</div>
        <div class="score" id="score"></div>
      </div>
      <div class="meta" id="meta"></div>
    </div>

    <div class="card">
      <div class="label">候选走法</div>
      <ol class="cands" id="cands"></ol>
    </div>

    <div class="card">
      <div class="label">走子记录 <span id="moveCount"></span></div>
      <div class="moves" id="moves"></div>
    </div>

    <div class="card">
      <div class="label">手机画面</div>
      <div class="frame-row">
        <img id="thumb" alt="手机画面">
        <div class="frame-info">
          <div id="engine"></div>
          <div><code id="fen"></code></div>
        </div>
      </div>
    </div>
  </aside>
</main>

<script>
(function () {
  // ── 棋盘几何：9 列 10 行交叉点 ────────────────────────────────
  var CW = 62, PAD = 38, R = 26;
  var cv = document.getElementById('board');
  var ctx = cv.getContext('2d');

  function X(c) { return PAD + c * CW; }
  function Y(r) { return PAD + r * CW; }

  var state = null;
  var selected = -1;   // 被点选的候选序号；-1 表示显示最佳走法

  // ── 绘制 ────────────────────────────────────────────────────
  function line(x0, y0, x1, y1) {
    ctx.beginPath(); ctx.moveTo(x0, y0); ctx.lineTo(x1, y1); ctx.stroke();
  }

  function drawBackground() {
    var g = ctx.createLinearGradient(0, 0, 0, cv.height);
    g.addColorStop(0, '#f0dcb4'); g.addColorStop(1, '#e3c894');
    ctx.fillStyle = g; ctx.fillRect(0, 0, cv.width, cv.height);

    ctx.strokeStyle = '#8a6a42'; ctx.lineWidth = 1.4;

    // 横线：10 条，左右贯通
    for (var r = 0; r < 10; r++) line(X(0), Y(r), X(8), Y(r));
    // 竖线：最外两条贯通，中间 7 条在河界处断开
    for (var c = 0; c < 9; c++) {
      if (c === 0 || c === 8) { line(X(c), Y(0), X(c), Y(9)); continue; }
      line(X(c), Y(0), X(c), Y(4));
      line(X(c), Y(5), X(c), Y(9));
    }
    // 九宫斜线
    line(X(3), Y(0), X(5), Y(2)); line(X(5), Y(0), X(3), Y(2));
    line(X(3), Y(7), X(5), Y(9)); line(X(5), Y(7), X(3), Y(9));

    // 外框加粗一圈
    ctx.lineWidth = 2.4; ctx.strokeStyle = '#7a5c38';
    ctx.strokeRect(X(0) - 7, Y(0) - 7, X(8) - X(0) + 14, Y(9) - Y(0) + 14);

    // 兵与炮的定位小折角
    ctx.lineWidth = 1.4; ctx.strokeStyle = '#8a6a42';
    var marks = [[3,0],[3,2],[3,4],[3,6],[3,8],[6,0],[6,2],[6,4],[6,6],[6,8],[2,1],[2,7],[7,1],[7,7]];
    var d = 5, e = 9;
    for (var i = 0; i < marks.length; i++) {
      var mr = marks[i][0], mc = marks[i][1];
      var px = X(mc), py = Y(mr);
      var corners = [[-1,-1],[1,-1],[-1,1],[1,1]];
      for (var k = 0; k < 4; k++) {
        var sx = corners[k][0], sy = corners[k][1];
        if (mc === 0 && sx < 0) continue;      // 贴边的点只画朝内的一半
        if (mc === 8 && sx > 0) continue;
        line(px + sx * e, py + sy * d, px + sx * (e + d), py + sy * d);
        line(px + sx * e, py + sy * d, px + sx * e, py + sy * (d + d));
      }
    }

    // 楚河汉界
    ctx.fillStyle = '#9a7a4e';
    ctx.font = '600 20px "PingFang SC", serif';
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    var my = (Y(4) + Y(5)) / 2;
    ctx.fillText('楚  河', X(2), my);
    ctx.fillText('汉  界', X(6), my);
  }

  function drawPiece(row, col, cell, highlight) {
    var px = X(col), py = Y(row);
    var red = cell.r;

    if (highlight) {
      ctx.beginPath(); ctx.arc(px, py, R + 4, 0, Math.PI * 2);
      ctx.fillStyle = 'rgba(255,214,102,0.55)'; ctx.fill();
    }

    ctx.beginPath(); ctx.arc(px, py, R, 0, Math.PI * 2);
    ctx.fillStyle = '#f7ecd4'; ctx.fill();
    ctx.lineWidth = 2; ctx.strokeStyle = red ? '#b3261e' : '#212121'; ctx.stroke();

    ctx.beginPath(); ctx.arc(px, py, R - 4.5, 0, Math.PI * 2);
    ctx.lineWidth = 1; ctx.strokeStyle = red ? 'rgba(179,38,30,.45)' : 'rgba(33,33,33,.4)'; ctx.stroke();

    ctx.fillStyle = red ? '#b3261e' : '#212121';
    ctx.font = '700 27px "PingFang SC", "STKaiti", serif';
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillText(cell.n, px, py + 1);
  }

  function drawArrow(x0, y0, x1, y1, color) {
    var ang = Math.atan2(y1 - y0, x1 - x0);
    var sx = x0 + Math.cos(ang) * (R + 5), sy = y0 + Math.sin(ang) * (R + 5);
    var ex = x1 - Math.cos(ang) * (R + 9), ey = y1 - Math.sin(ang) * (R + 9);

    // 先描一圈白边，深色棋子上也能看清
    ctx.lineCap = 'round';
    ctx.strokeStyle = 'rgba(255,255,255,0.85)'; ctx.lineWidth = 9;
    line(sx, sy, ex, ey);
    ctx.strokeStyle = color; ctx.lineWidth = 5;
    line(sx, sy, ex, ey);

    var head = 15;
    ctx.beginPath();
    ctx.moveTo(ex, ey);
    ctx.lineTo(ex - Math.cos(ang - 0.42) * head, ey - Math.sin(ang - 0.42) * head);
    ctx.lineTo(ex - Math.cos(ang + 0.42) * head, ey - Math.sin(ang + 0.42) * head);
    ctx.closePath();
    ctx.fillStyle = 'rgba(255,255,255,0.85)';
    ctx.fill();
    ctx.fillStyle = color; ctx.fill();
  }

  function draw() {
    drawBackground();
    if (!state || !state.board) return;

    for (var r = 0; r < 10; r++) {
      for (var c = 0; c < 9; c++) {
        var cell = state.board[r][c];
        if (!cell || !cell.n) continue;
        var hl = false;
        if (state.lastFrom && state.lastFrom[0] === r && state.lastFrom[1] === c) hl = true;
        if (state.lastTo && state.lastTo[0] === r && state.lastTo[1] === c) hl = true;
        drawPiece(r, c, cell, hl);
      }
    }

    var mv = null, color = '#2e7d32';
    var a = state.analysis;
    if (a && a.from && a.to) {
      mv = { from: a.from, to: a.to };
      if (selected >= 0 && a.candidates && a.candidates[selected] &&
          a.candidates[selected].from && a.candidates[selected].to) {
        mv = { from: a.candidates[selected].from, to: a.candidates[selected].to };
        color = '#ef6c00';
      }
    }
    if (mv) drawArrow(X(mv.from[1]), Y(mv.from[0]), X(mv.to[1]), Y(mv.to[0]), color);
  }

  // ── 侧栏渲染 ────────────────────────────────────────────────
  function pillClass(s) {
    if (s.indexOf('等待对方') >= 0) return 'wait';
    if (s.indexOf('稳定') >= 0) return 'move';
    if (s.indexOf('分析') >= 0) return 'think';
    if (s.indexOf('展示') >= 0) return 'done';
    if (s.indexOf('异常') >= 0) return 'bad';
    return '';
  }

  function renderMoves(moves) {
    var box = document.getElementById('moves');
    box.textContent = '';
    for (var i = 0; i < moves.length; i += 2) {
      var d = document.createElement('div');
      d.className = 'mv r';
      var n = document.createElement('span');
      n.style.color = 'var(--muted)';
      n.style.marginRight = '5px';
      n.textContent = (i / 2 + 1) + '.';
      d.appendChild(n);
      var b1 = document.createElement('b'); b1.textContent = moves[i]; d.appendChild(b1);
      if (moves[i + 1]) {
        d.appendChild(document.createTextNode(' '));
        var b2 = document.createElement('b'); b2.textContent = moves[i + 1]; d.appendChild(b2);
        d.className = 'mv b';
      }
      box.appendChild(d);
    }
    document.getElementById('moveCount').textContent =
      moves.length ? '（' + Math.ceil(moves.length / 2) + ' 回合）' : '';
    box.scrollTop = box.scrollHeight;
  }

  function renderCands(a) {
    var box = document.getElementById('cands');
    box.textContent = '';
    if (!a || !a.candidates) return;
    for (var i = 0; i < a.candidates.length; i++) {
      (function (i) {
        var c = a.candidates[i];
        var li = document.createElement('li');
        if (i === selected) li.className = 'sel';

        var idx = document.createElement('span'); idx.className = 'idx'; idx.textContent = (i + 1);
        var nt = document.createElement('span'); nt.className = 'nt'; nt.textContent = c.notation;
        var sc = document.createElement('span'); sc.className = 'sc'; sc.textContent = c.score;
        sc.style.color = c.neg ? 'var(--bad)' : 'var(--ok)';
        if (c.isMate) sc.style.color = '#ffb84d';

        li.appendChild(idx); li.appendChild(nt); li.appendChild(sc);

        if (c.pv && c.pv.length > 1) {
          var pv = document.createElement('span'); pv.className = 'pv';
          var shown = c.pv.slice(1, 9).join(' ');
          pv.textContent = shown + (c.pv.length > 9 ? ' …' : '');
          li.appendChild(pv);
        }

        li.addEventListener('click', function () {
          selected = (selected === i) ? -1 : i;
          renderCands(a); draw();
        });
        box.appendChild(li);
      })(i);
    }
  }

  var lastFrameVer = -1;

  function render(st) {
    state = st;

    var pill = document.getElementById('state');
    pill.textContent = st.state;
    pill.className = 'pill ' + pillClass(st.state);

    var a = st.analysis;
    var best = document.getElementById('best');
    if (a && a.bestMove) {
      best.textContent = a.bestMove;
      best.className = 'best-move';
      var sc = document.getElementById('score');
      sc.textContent = a.score;
      sc.className = 'score ' + (a.isMate ? 'mate' : (a.neg ? 'neg' : 'pos'));
      document.getElementById('meta').textContent = '';
      var m = document.getElementById('meta');
      var parts = ['深度 ' + a.depth, a.elapsedMs + 'ms', a.nodes + ' 节点'];
      if (!a.final) parts.push('搜索中…');
      for (var i = 0; i < parts.length; i++) {
        var s = document.createElement('span'); s.textContent = parts[i]; m.appendChild(s);
      }
    } else {
      best.textContent = '等待识别';
      best.className = 'best-move empty';
      document.getElementById('score').textContent = '';
      document.getElementById('meta').textContent = '';
    }

    renderCands(a);
    renderMoves(st.moves || []);

    var notice = document.getElementById('notice');
    if (st.desynced && st.notice) { notice.hidden = false; notice.textContent = st.notice; }
    else if (st.notice) { notice.hidden = false; notice.textContent = st.notice; }
    else { notice.hidden = true; }

    document.getElementById('pause').textContent = st.paused ? '继续' : '暂停';
    document.getElementById('engine').textContent = st.engine || '';
    document.getElementById('fen').textContent = st.fen || '';

    if (st.frameVer !== lastFrameVer) {
      lastFrameVer = st.frameVer;
      document.getElementById('thumb').src = '/api/frame?v=' + st.frameVer;
    }

    draw();
  }

  // ── 与前端的连接 ────────────────────────────────────────────
  function post(url) { fetch(url, { method: 'POST' }).catch(function () {}); }

  document.getElementById('resync').addEventListener('click', function () {
    if (confirm('确定要重新同步吗？\n\n内部棋盘会回到标准初始局面。\n请先在手机上重新开一局停在开局画面。')) {
      post('/api/resync');
    }
  });
  document.getElementById('pause').addEventListener('click', function () {
    post('/api/pause?paused=' + (state && state.paused ? 'false' : 'true'));
  });

  var src = new EventSource('/events');
  src.addEventListener('status', function (e) {
    try { render(JSON.parse(e.data)); } catch (err) { console.error(err); }
  });
  src.onerror = function () {
    var pill = document.getElementById('state');
    pill.textContent = '连接断开，正在重连…';
    pill.className = 'pill bad';
  };

  draw();
})();
</script>
</body>
</html>
`
