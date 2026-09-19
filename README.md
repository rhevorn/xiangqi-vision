# Android 中国象棋 AI 辅助系统

通过安卓手机画面自动识别对方走棋，维护完整棋盘状态，并用本地 Pikafish 给出最佳走法。
手机端无需安装任何定制 App。

```text
Android 手机 → ADB 截图 → 棋盘差分 → 推断走法 → 规则校验 → FEN → Pikafish → 最佳走法
```

## 核心设计原则

这套系统的稳定性来自一条硬性纪律：

> **视觉只负责发现变化，象棋规则维护真实状态，Pikafish 专门负责搜索。**

不要做成「AI 看图 → AI 猜棋 → AI 下棋」。具体来说：

- **不识别棋子**。程序启动时内部棋盘就是标准初始局面（红方先行），之后每帧只比较
  90 个交叉点的灰度差异，找出「哪两个格子变了」，而不是重新识别 32 枚棋子。
- **视觉只提供候选**。两个变化的格子之间有正反两个方向，两个候选都交给规则层裁决。
  象棋规则会否掉绝大多数错误猜测——例如「炮二平五」的反向走法在初始局面根本
  不合法（e2 上没有棋子）。
- **识别失败绝不污染棋盘**。推断不出合法走法时，内部棋盘保持原样，只标记失步并
  提示重新同步。宁可漏掉一手，也不能让错误的识别写进状态。

规则实现的正确性由 Pikafish 的 `perft` 交叉验证：初始局面深度 1~4 的节点数
（44 / 1920 / 79666 / 3290240）与本项目 `internal/game` 的输出逐节点一致。

## 环境要求

| 组件 | 说明 |
|---|---|
| Go 1.21+ | 主程序 |
| Pikafish | 象棋引擎，需自行构建（见下） |
| adb | Android platform-tools，用于抓屏 |
| 安卓手机 | 开启 USB 调试 |

OpenCV **不需要**。图像处理只用 Go 标准库，二进制可以直接分发。

## 构建

```bash
# 1. 克隆并编译 Pikafish（会一并下载 NNUE 权重）
make pikafish

# 2. 编译主程序
make build

# 3. 确认可用
./bin/assistant board
./bin/assistant analyze 炮二平五
```

`make pikafish` 做的事：克隆 `official-pikafish/Pikafish`，用 `make build ARCH=apple-silicon`
编译，再把可执行文件与 `pikafish.nnue` 一起复制到 `bin/`。

## 快速开始

```bash
# 1. 确认手机已连上
./bin/assistant devices

# 2. 抓一张截图（确认画面正常）
./bin/assistant capture -o shot.png

# 3. 校准棋盘：浏览器里点两个角点
./bin/assistant calibrate

# 4. 手机上开局，然后跑完整链路
./bin/assistant watch
```

### 没有手机也能验证

内置的合成画面生成器可以把整条链路跑通，不需要真机：

```bash
./bin/assistant simulate                       # 生成一段 10 手开局的画面序列
./bin/assistant watch -frames testdata/game    # 回放它
```

回放时会打印每一步的识别结果与引擎建议，可以用来确认校准、差分、规则、引擎都正常。

## 子命令

| 命令 | 说明 |
|---|---|
| `board` | 打印棋盘（可指定 `-fen`） |
| `analyze` | 从命令行给的走法走到某个局面，然后让引擎给建议 |
| `capture` | 抓一张截图存成 PNG |
| `devices` | 列出已连接的安卓设备 |
| `calibrate` | 浏览器点击式棋盘校准 |
| `watch` | 完整链路：自动识别对方走子并给出建议 |
| `simulate` | 合成画面序列，用于无手机验证 |

各子命令的完整选项用 `assistant <子命令> -h` 查看。

### analyze

不走手机，直接从命令行构造局面并分析：

```bash
# 从初始局面走两步后分析
./bin/assistant analyze 炮二平五 马8进7

# 直接指定 FEN
./bin/assistant analyze -fen "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

# 调参
./bin/assistant analyze 炮二平五 -movetime 1000 -multipv 5
```

走法支持中文记谱（`炮二平五`、`马8进7`、`前马进七`）与 UCI 坐标（`h2e2`）两种写法。

### watch

```bash
./bin/assistant watch                          # 连真机
./bin/assistant watch -frames testdata/game    # 回放画面序列
./bin/assistant watch -image shot.png          # 固定图片（调试用）
./bin/assistant watch -mock                    # 用内置假引擎，验证视觉链路
./bin/assistant watch -board                   # 每步打印棋盘
```

运行中可用的按键：

| 键 | 作用 |
|---|---|
| `r` | 重新同步（回到标准初始局面） |
| `b` | 打印当前棋盘 |
| `f` | 打印最近一步与当前 FEN |
| `q` | 退出 |

### calibrate

校准是整个系统的地基。`assistant calibrate` 会抓一张截图并用浏览器打开一个本地页面：

1. 依次点击棋盘的**左上角交叉点**与**右下角交叉点**（最外侧两条线的交点，
   不是图片边缘）；
2. 页面会实时把 90 个取样点画出来，确认它们都落在交叉点上；
3. 点「保存」，程序把棋盘区域写进配置，并另外输出一张 `debug/calibration.png`
   供事后复核。

页面还会提示「格子不是正方形」这类明显的点选错误。

## 配置文件

默认路径 `configs/config.yaml`，不存在时使用内置默认值（可用 `-config` 指定其他路径）。

```yaml
device:
  adb_path: ""          # 留空则自动探测 PATH 与 Android SDK 常见位置
  adb_serial: ""        # 只有一台设备时可留空

capture:
  fps: 5                # 截图频率。象棋是回合制，2~5 已经足够
  frames_dir: ""        # 用本地画面序列代替真机
  frame_file: ""        # 用固定图片代替真机
  repeat_each: 1        # 序列中每张图重复返回的次数

board:                  # 由 calibrate 自动写入
  x: 120
  y: 310
  width: 870            # 左上角点到右下角点的横向跨度
  height: 970

vision:
  roi_size: 0           # 取样区域边长，0 表示按格距自动推算（约格距的 45%）
  diff_threshold: 20    # 判定某个格点发生变化的平均灰度差阈值
  stable_frames: 3      # 判定棋局落定所需的连续静止帧数
  max_candidate_cells: 4
  retry_frames: 3       # 推断失败时额外抓取的帧数

engine:
  path: ./bin/pikafish
  threads: 4
  hash_mb: 256
  movetime_ms: 500
  multipv: 3
  eval_file: ""         # 留空则用引擎同目录下的 pikafish.nnue

log:
  level: info           # debug / info / warn / error
  file: ""              # 留空则只输出到终端

debug:
  dir: debug
  save_frames: false    # 开启后每一帧都存盘，仅供深度排查
```

## 工作流程

```text
抓屏（fps 可配）
   ↓
灰度化
   ↓
与上一帧比较 90 个交叉点的平均灰度差
   ├─ 还在变（动画/高亮）→ 继续等
   ↓ 连续 N 帧静止
与「上一个稳定帧」比较，找出变化的格点
   ├─ 没有变化 → 继续等
   ↓
两两组合成候选走法（正反两个方向都在内）
   ↓
交给象棋规则裁决
   ├─ 都不合法 → 重抓几帧重试；仍失败则标记失步，**不更新棋盘**
   ↓ 唯一合法
更新棋盘 → 导出 FEN → Pikafish → 最佳走法
```

几个关键细节：

- **采样区域只取格子中央**（约格距的 45%）。象棋 App 普遍会在格子上叠加
  「最后一步」高亮框、选中框或可走点提示，这些装饰都在格子边缘，取样区避开它们
  就不会被误判成走子。
- **稳定判定用最大差异，不用平均差异**。走子只影响一两个格子，取平均会被 90 个
  格子稀释掉。
- **引擎收到完整的重复局面历史**。发送的是 `position fen <起始局面> moves <全部走法>`
  而不是单个 FEN，这样 Pikafish 才能正确处理长将与重复局面。

## 延迟调优

方案书 §23 要求「从对方落子完成到输出最佳走法 < 1 秒」。延迟由两段构成：

```text
落子 → 确认画面稳定       stable_frames / fps
     → 引擎搜索           movetime_ms
```

默认配置（`fps: 5`、`stable_frames: 3`、`movetime_ms: 500`）算下来是
`3/5 + 0.5 ≈ 1.1 秒`，略超目标。要压进 1 秒，按需调其中一项：

| 调整 | 效果 | 代价 |
|---|---|---|
| `fps: 10` | 稳定判定从 600ms 降到 300ms | 抓屏开销翻倍 |
| `stable_frames: 2` | 再省一帧的时间 | 对动画的容忍度下降 |
| `movetime_ms: 350` | 直接省 150ms | 棋力略降（仍有 18 层左右） |

`fps: 10` + `movetime_ms: 350` 大约是 550ms，余量比较舒服。

`watch` 打开 `-progress`（默认开）时会先显示浅层结果再不断刷新，所以用户看到
第一个答案的时间远早于最终结果。

## 项目结构

```text
cmd/assistant/          命令行入口
internal/
  capture/              ADB 截图、本地图片、画面序列回放
  vision/               校准、90 点取样、帧差分、稳定判定、走法推断
  game/                 棋盘、走法规则、FEN、中文记谱
  engine/               UCI 协议客户端（Pikafish）与 Mock 引擎
  analyzer/             把引擎结果翻译成中文记谱与展示结构
  overlay/              终端渲染
  app/                  主循环状态机
  config/               YAML 配置
```

## 测试

```bash
make test          # 全部测试
make test-short    # 跳过耗时的深度 perft

go test ./internal/game/ -run Perft -v    # 与 Pikafish 交叉验证走法生成
```

引擎集成测试会在找不到 `bin/pikafish` 时自动跳过，也可以用
`PIKAFISH_PATH=/path/to/pikafish` 指定。

## 排查

**识别不到走子**
先确认 `debug/calibration.png` 里 90 个红点是否都落在交叉点上。校准偏了是最常见的原因。
然后看 `watch -v` 的日志里「变化格」一行：正常走子应该恰好有两个格子的差异明显
高于阈值，其余接近 0。

**频繁失步**
多半是 `diff_threshold` 偏低，把动画或高亮当成了走子。可以调高阈值，或调大
`stable_frames` 让它多等几帧。识别失败时 `debug/` 下会留下标注了变化格点的截图。

**引擎很慢或无响应**
检查 `bin/pikafish` 旁边是否有 `pikafish.nnue`。引擎按工作目录查找权重文件，
缺失时会退化成很弱的搜索。

## 第一版不包含

按方案书 §22，以下都留到后续版本：OCR / YOLO 棋子识别、自动操作手机、
复杂 GUI 与 Mac 浮层、完整棋盘识别（因此 MVP 只支持从标准初始局面启动）、
棋谱与云端服务。

失步后的恢复手段是 `r` 键重新同步（回到初始局面），而不是重新识别整个棋盘。
