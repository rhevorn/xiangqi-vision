package webui

import (
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"strings"

	"xiangqi-vision/internal/vision"
)

// CalibratePage 返回棋盘校准页面的 HTML。
//
// imageURL 是页面拉取截图的地址——命令行版直接内嵌 data URI，网页版指向
// /api/screenshot，两者共用同一套模板。afterSaveURL 非空时，保存成功后页面
// 会跳转到它。
func CalibratePage(imageURL, afterSaveURL string) string {
	page := strings.Replace(calibrateHTML, "__IMAGE_URL__", imageURL, 1)
	return strings.Replace(page, "__AFTER_SAVE__", afterSaveURL, 1)
}

// ParseCalibrationRect 校验并解析浏览器提交的棋盘区域。
func ParseCalibrationRect(r *http.Request, img image.Image) (vision.Rect, error) {
	var rect vision.Rect
	if err := json.NewDecoder(r.Body).Decode(&rect); err != nil {
		return rect, fmt.Errorf("请求格式错误: %w", err)
	}

	// 规范化：保证从左上到右下（用户可能先点了右下角）
	if rect.Width < 0 {
		rect.X += rect.Width
		rect.Width = -rect.Width
	}
	if rect.Height < 0 {
		rect.Y += rect.Height
		rect.Height = -rect.Height
	}
	if !rect.Valid() {
		return rect, fmt.Errorf("棋盘尺寸无效: %dx%d", rect.Width, rect.Height)
	}
	if _, err := vision.NewCalibration(rect, 0); err != nil {
		return rect, err
	}

	b := img.Bounds()
	if rect.X < b.Min.X || rect.Y < b.Min.Y ||
		rect.X+rect.Width > b.Max.X || rect.Y+rect.Height > b.Max.Y {
		return rect, fmt.Errorf("棋盘区域超出画面范围（画面 %dx%d）", b.Dx(), b.Dy())
	}
	return rect, nil
}

// calibrateHTML 是校准页面。用 Go 原始字符串保存，因此内部不能出现反引号，
// JS 里也一律用单引号拼字符串。
const calibrateHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>棋盘校准</title>
<style>
  :root {
    --bg: #14161a; --panel: #1d2026; --line: #2e333c;
    --fg: #e8eaed; --muted: #9aa0a6; --accent: #4c9aff; --warn: #f5a623; --ok: #3ddc84;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 24px; background: var(--bg); color: var(--fg);
    font: 14px/1.6 -apple-system, "PingFang SC", "Helvetica Neue", sans-serif;
  }
  h1 { font-size: 18px; margin: 0 0 8px; font-weight: 600; }
  .hint { color: var(--muted); margin: 0 0 4px; }
  .hint b { color: var(--fg); }
  .bar {
    display: flex; gap: 12px; align-items: center; flex-wrap: wrap;
    margin: 16px 0; padding: 12px 16px; background: var(--panel);
    border: 1px solid var(--line); border-radius: 8px;
  }
  #stage { position: relative; display: inline-block; line-height: 0;
           border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
  #shot { display: block; max-width: min(100%, 520px); height: auto; cursor: crosshair; }
  #grid { position: absolute; left: 0; top: 0; width: 100%; height: 100%; pointer-events: none; }
  button {
    font: inherit; padding: 7px 18px; border-radius: 6px; cursor: pointer;
    border: 1px solid var(--line); background: #262a31; color: var(--fg);
  }
  button:hover:not(:disabled) { background: #30353e; }
  button.primary { background: var(--accent); border-color: var(--accent); color: #06121f; font-weight: 600; }
  button.primary:hover:not(:disabled) { background: #6aabff; }
  button:disabled { opacity: .4; cursor: not-allowed; }
  a { color: var(--accent); }
  .stat { color: var(--muted); }
  .stat b { color: var(--fg); font-variant-numeric: tabular-nums; }
  #status { font-weight: 600; }
  .ok { color: var(--ok); } .warn { color: var(--warn); }
</style>
</head>
<body>
  <h1>棋盘校准</h1>
  <p class="hint">
    请依次点击棋盘的<b>左上角交叉点</b>与<b>右下角交叉点</b>——
    也就是最外侧两条线相交的那两个点，<b>不是</b>棋盘图片的边缘。
  </p>
  <p class="hint">点击后页面会画出 90 个红色取样点，确认它们都落在交叉点上再保存。</p>

  <div class="bar">
    <span class="stat" id="stat">已选 <b>0</b> / 2 个角点</span>
    <span class="stat" id="geom"></span>
    <span id="status"></span>
    <span style="flex:1"></span>
    <button id="refresh" title="重新抓一张手机画面">重新抓屏</button>
    <button id="reset">重选</button>
    <button id="save" class="primary" disabled>保存</button>
  </div>

  <div id="stage">
    <img id="shot" src="__IMAGE_URL__" alt="手机画面">
    <canvas id="grid"></canvas>
  </div>

<script>
(function () {
  var AFTER_SAVE = '__AFTER_SAVE__';
  var img = document.getElementById('shot');
  var canvas = document.getElementById('grid');
  var ctx = canvas.getContext('2d');
  var stat = document.getElementById('stat');
  var geom = document.getElementById('geom');
  var status = document.getElementById('status');
  var saveBtn = document.getElementById('save');

  var ROWS = 10, COLS = 9;
  var pts = [];
  var saving = false;

  function ready() {
    canvas.width = img.naturalWidth;
    canvas.height = img.naturalHeight;
    draw();
  }
  if (img.complete && img.naturalWidth) { ready(); } else { img.addEventListener('load', ready); }

  // 把鼠标位置换算成图片原始像素坐标（页面上图片可能被缩放过）
  function toImage(e) {
    var r = img.getBoundingClientRect();
    return {
      x: Math.round((e.clientX - r.left) * (img.naturalWidth / r.width)),
      y: Math.round((e.clientY - r.top) * (img.naturalHeight / r.height))
    };
  }

  function line(x0, y0, x1, y1) {
    ctx.beginPath(); ctx.moveTo(x0, y0); ctx.lineTo(x1, y1); ctx.stroke();
  }

  function marker(p, color, label) {
    var s = Math.max(8, canvas.width / 60);
    ctx.strokeStyle = color; ctx.lineWidth = Math.max(2, canvas.width / 400);
    line(p.x - s, p.y, p.x + s, p.y);
    line(p.x, p.y - s, p.x, p.y + s);
    if (label) {
      ctx.fillStyle = color;
      ctx.font = 'bold ' + Math.round(canvas.width / 34) + 'px sans-serif';
      ctx.fillText(label, p.x + s, p.y - s);
    }
  }

  function draw() {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    if (pts.length === 0) return;
    if (pts.length === 1) { marker(pts[0], '#ff3b30', '左上'); return; }

    var a = pts[0], b = pts[1];
    var dx = (b.x - a.x) / (COLS - 1);
    var dy = (b.y - a.y) / (ROWS - 1);
    var wide = Math.max(1, canvas.width / 900);

    ctx.strokeStyle = 'rgba(76,154,255,0.85)'; ctx.lineWidth = wide;
    for (var r = 0; r < ROWS; r++) { line(a.x, a.y + r * dy, a.x + (COLS - 1) * dx, a.y + r * dy); }
    for (var c = 0; c < COLS; c++) { line(a.x + c * dx, a.y, a.x + c * dx, a.y + (ROWS - 1) * dy); }

    var roi = Math.max(4, Math.round(Math.min(dx, dy) * 0.45));
    ctx.strokeStyle = 'rgba(61,220,132,0.9)';
    ctx.fillStyle = '#ff3b30';
    var dot = Math.max(1.5, canvas.width / 700);
    for (var rr = 0; rr < ROWS; rr++) {
      for (var cc = 0; cc < COLS; cc++) {
        var x = a.x + cc * dx, y = a.y + rr * dy;
        ctx.strokeRect(x - roi / 2, y - roi / 2, roi, roi);
        ctx.beginPath(); ctx.arc(x, y, dot, 0, Math.PI * 2); ctx.fill();
      }
    }
    marker(a, '#ff3b30', '左上');
    marker(b, '#ff3b30', '右下');
  }

  function normalize() {
    if (pts.length < 2) return null;
    var a = pts[0], b = pts[1];
    return {
      x: Math.min(a.x, b.x), y: Math.min(a.y, b.y),
      width: Math.abs(b.x - a.x), height: Math.abs(b.y - a.y)
    };
  }

  function update() {
    stat.innerHTML = '已选 <b>' + pts.length + '</b> / 2 个角点';
    var rect = normalize();
    if (!rect) { geom.textContent = ''; saveBtn.disabled = true; return; }

    var dx = rect.width / (COLS - 1), dy = rect.height / (ROWS - 1);
    var ratio = Math.min(dx, dy) / Math.max(dx, dy);
    geom.innerHTML = '棋盘 <b>' + rect.width + '×' + rect.height +
      '</b>　格距 <b>' + dx.toFixed(1) + '×' + dy.toFixed(1) + '</b>';

    // 象棋棋盘的格点应当接近正方形：长宽比偏离太多，通常意味着角点点错了
    if (ratio < 0.9) {
      status.className = 'warn';
      status.textContent = '⚠ 格子不是正方形（' + (ratio * 100).toFixed(0) + '%），请检查是否点准了角点';
    } else {
      status.className = '';
      status.textContent = '';
    }
    saveBtn.disabled = false;
  }

  document.getElementById('stage').addEventListener('click', function (e) {
    if (saving) return;
    if (pts.length >= 2) { pts = []; }
    pts.push(toImage(e));
    draw(); update();
  });

  document.getElementById('reset').addEventListener('click', function () {
    pts = []; status.textContent = ''; status.className = '';
    draw(); update();
  });

  document.getElementById('refresh').addEventListener('click', function () {
    pts = []; draw(); update();
    status.className = ''; status.textContent = '正在重新抓屏…';
    img.src = '__IMAGE_URL__' + (('__IMAGE_URL__'.indexOf('?') >= 0) ? '&' : '?') + 'refresh=1&t=' + Date.now();
    img.onload = function () { status.textContent = ''; ready(); };
  });

  saveBtn.addEventListener('click', function () {
    var rect = normalize();
    if (!rect) return;
    saving = true;
    saveBtn.disabled = true;
    status.className = '';
    status.textContent = '保存中…';

    fetch('/api/calibrate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(rect)
    }).then(function (r) {
      if (!r.ok) { return r.text().then(function (t) { throw new Error(t); }); }
      status.className = 'ok';
      status.textContent = '✓ 已保存';
      if (AFTER_SAVE) {
        setTimeout(function () { location.href = AFTER_SAVE; }, 600);
      }
    }).catch(function (err) {
      saving = false;
      saveBtn.disabled = false;
      status.className = 'warn';
      status.textContent = '保存失败: ' + err.message;
    });
  });

  update();
})();
</script>
</body>
</html>
`
