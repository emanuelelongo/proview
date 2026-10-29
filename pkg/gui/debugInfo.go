package gui

import (
	"fmt"
	"runtime/metrics"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const debugPreferenceKey = "debug.enabled"
const debugSampleInterval = 5 * time.Second
const debugInfoTooltip = "Go CPU (estimated capacity usage) · Go RAM · Goroutines"

type debugCPUSample struct{ total, idle, percent float64 }

func (s *debugCPUSample) update(total, idle float64) float64 {
	// Runtime CPU counters may remain unchanged between reads. Keep the last
	// estimate until new counters arrive; the first estimate uses process totals.
	if delta := total - s.total; delta > 0 {
		s.percent = min(1.0, max(0.0, 1-(idle-s.idle)/delta)) * 100
		s.total, s.idle = total, idle
	}
	return s.percent
}

func (g *Gui) setDebugEnabled(enabled bool) {
	g.stopDebugInfo()
	g.debugEnabled = enabled
	g.app.Preferences().SetBool(debugPreferenceKey, enabled)
	if g.debugItem != nil {
		g.debugItem.Checked = enabled
		refreshMenuState(g.tipsMenu)
	}
	if enabled {
		g.setTipsEnabled(false)
		g.startDebugInfo()
	} else {
		g.hideTip()
	}
}

func (g *Gui) stopDebugInfo() {
	g.debugGeneration++
	if g.tipSurface != nil {
		g.tipSurface.MouseOut()
	}
	if g.debugTimer != nil {
		g.debugTimer.Stop()
		g.debugTimer = nil
	}
}

func (g *Gui) startDebugInfo() {
	g.stopDebugInfo()
	g.debugCPU = debugCPUSample{}
	g.updateDebugInfo()
	g.scheduleDebugInfo(g.debugGeneration)
}

func (g *Gui) scheduleDebugInfo(generation uint64) {
	g.debugTimer = time.AfterFunc(debugSampleInterval, func() {
		fyne.Do(func() {
			if g.closed || !g.debugEnabled || generation != g.debugGeneration {
				return
			}
			g.debugTimer = nil
			g.updateDebugInfo()
			g.scheduleDebugInfo(generation)
		})
	})
}

func (g *Gui) updateDebugInfo() {
	if g.closed || !g.debugEnabled || g.tipText == nil {
		return
	}
	samples := []metrics.Sample{
		{Name: "/cpu/classes/total:cpu-seconds"},
		{Name: "/cpu/classes/idle:cpu-seconds"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
		{Name: "/sched/goroutines:goroutines"},
	}
	metrics.Read(samples)
	// Compare runtime CPU classes only, not OS process CPU time.
	cpu := g.debugCPU.update(samples[0].Value.Float64(), samples[1].Value.Float64())
	if g.selectionStatusActive {
		return
	}
	memory := samples[2].Value.Uint64() - samples[3].Value.Uint64()
	g.tipText.Color = theme.Color(theme.ColorNameForeground)
	g.tipText.Text = fmt.Sprintf("%.1f%% · %.1f MiB · %d", cpu, float64(memory)/(1024*1024), samples[4].Value.Uint64())
	g.refreshTipText()
}
