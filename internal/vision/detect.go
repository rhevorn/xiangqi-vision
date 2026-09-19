package vision

import (
	"fmt"
	"image"
	"sort"

	"xiangqi-vision/internal/game"
)

// MoveCandidate 是视觉层推断出的候选走法。
//
// 视觉只负责"猜"，规则层负责"确认"：这里两个方向都会给出，由控制器交给
// 象棋规则裁决哪一个合法。
type MoveCandidate struct {
	Move  game.Move
	Score float64 // 起止两格差异之和，越大越像真实走法
}

func (c MoveCandidate) String() string {
	return fmt.Sprintf("%s(%.1f)", c.Move, c.Score)
}

// DetectMoveCandidates 从格点差异中推断候选走法。
//
// 取差异最大的若干个格点（通常正好是"起点"和"终点"两个），两两组合成
// 有序对；因为无法从单帧差分判断棋子是移出还是移入，所以两个方向都会
// 作为候选给出。
func DetectMoveCandidates(diffs []CellDiff, minScore float64, maxCells int) []MoveCandidate {
	changed := make([]CellDiff, 0, len(diffs))
	for _, d := range diffs {
		if d.Score >= minScore {
			changed = append(changed, d)
		}
	}
	if len(changed) < 2 {
		return nil
	}

	sort.SliceStable(changed, func(i, j int) bool { return changed[i].Score > changed[j].Score })
	if maxCells > 0 && len(changed) > maxCells {
		changed = changed[:maxCells]
	}

	out := make([]MoveCandidate, 0, len(changed)*(len(changed)-1))
	for _, from := range changed {
		for _, to := range changed {
			if from.Row == to.Row && from.Col == to.Col {
				continue
			}
			out = append(out, MoveCandidate{
				Move:  game.Move{From: from.Square(), To: to.Square()},
				Score: from.Score + to.Score,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// StabilityTracker 判断画面是否已经静止。
//
// 象棋 App 普遍有走子动画、落子高亮与"最后一步"标记，看到变化就分析必然
// 会读到中间的动画帧。因此必须等到连续若干帧不再变化，才认为棋局落定。
type StabilityTracker struct {
	// Threshold 为"认为画面没变"的单格点最大平均绝对差阈值。
	Threshold float64
	// Frames 为判定稳定所需的连续静止帧数。
	Frames int

	cal     *Calibration
	prev    *image.Gray
	stable  int
	lastMax float64
}

// NewStabilityTracker 创建稳定判定器。
func NewStabilityTracker(cal *Calibration, threshold float64, frames int) *StabilityTracker {
	if frames < 1 {
		frames = 1
	}
	return &StabilityTracker{cal: cal, Threshold: threshold, Frames: frames}
}

// Observe 观测一帧画面。
//
// 只在"刚好达到连续静止帧数"的那一帧返回 true（边沿触发），避免调用方
// 对同一个静止画面反复触发分析。
func (t *StabilityTracker) Observe(cur *image.Gray) (settled bool, maxDiff float64) {
	if t.prev == nil {
		t.prev = cur
		return false, 0
	}

	maxDiff = MaxCellDiff(DiffCells(t.prev, cur, t.cal))
	t.prev = cur
	t.lastMax = maxDiff

	if maxDiff >= t.Threshold {
		t.stable = 0
		return false, maxDiff
	}

	t.stable++
	if t.stable == t.Frames {
		return true, maxDiff
	}
	return false, maxDiff
}

// Reset 用给定帧重新开始跟踪。换设备、重新校准或重新同步时应调用。
func (t *StabilityTracker) Reset(frame *image.Gray) {
	t.prev = frame
	t.stable = 0
	t.lastMax = 0
}

// Describe 返回追踪器配置说明。
func (t *StabilityTracker) Describe() string {
	return fmt.Sprintf("阈值 %.1f / 连续 %d 帧", t.Threshold, t.Frames)
}
