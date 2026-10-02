package transfer

import (
	"fmt"
	"time"
)

// FormatBytes renders a byte count like "48.2M" or "512B".
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v >= 100 {
		return fmt.Sprintf("%.0f%c", v, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f%c", v, "KMGTPE"[exp])
}

// FormatRate renders bytes per second.
func FormatRate(bps float64) string { return FormatBytes(int64(bps)) + "/s" }

// ETA estimates the remaining time, or "" when unknown.
func (p Progress) ETA() string {
	if p.Rate <= 0 || p.Done >= p.Total {
		return ""
	}
	d := time.Duration(float64(p.Total-p.Done)/p.Rate) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// Fraction is the completed share of bytes in [0,1].
func (p Progress) Fraction() float64 {
	if p.Total <= 0 {
		if p.Files > 0 && p.Settled == p.Files {
			return 1
		}
		return 0
	}
	return min(1, float64(p.Done)/float64(p.Total))
}
