package fancontrol

import (
	"testing"
)

func TestInterpolate(t *testing.T) {
	curve := []CurvePoint{
		{TempC: 0, DutyPercent: 0},
		{TempC: 40, DutyPercent: 0},
		{TempC: 50, DutyPercent: 20},
		{TempC: 60, DutyPercent: 40},
		{TempC: 70, DutyPercent: 60},
		{TempC: 80, DutyPercent: 80},
		{TempC: 90, DutyPercent: 100},
	}

	tests := []struct {
		name string
		temp float64
		want int
	}{
		{"below curve", -10.0, 0},
		{"at first point", 0.0, 0},
		{"in flat region", 30.0, 0},
		{"at ramp start", 40.0, 0},
		{"mid first ramp", 45.0, 10},
		{"at 50C", 50.0, 20},
		{"at 55C", 55.0, 30},
		{"at 60C", 60.0, 40},
		{"at 75C", 75.0, 70},
		{"at last point", 90.0, 100},
		{"above curve", 100.0, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Interpolate(tt.temp, curve)
			if got != tt.want {
				t.Errorf("Interpolate(%v) = %d, want %d", tt.temp, got, tt.want)
			}
		})
	}
}

func TestInterpolateEmptyCurve(t *testing.T) {
	got := Interpolate(50.0, nil)
	if got != 0 {
		t.Errorf("Interpolate with nil curve = %d, want 0", got)
	}
}

func TestInterpolateSinglePoint(t *testing.T) {
	curve := []CurvePoint{{TempC: 50, DutyPercent: 50}}

	tests := []struct {
		name string
		temp float64
		want int
	}{
		{"below", 30.0, 50},
		{"at", 50.0, 50},
		{"above", 70.0, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Interpolate(tt.temp, curve)
			if got != tt.want {
				t.Errorf("Interpolate(%v) = %d, want %d", tt.temp, got, tt.want)
			}
		})
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{5, 5},
		{-5, 5},
		{0, 0},
	}
	for _, tt := range tests {
		if got := abs(tt.input); got != tt.want {
			t.Errorf("abs(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if len(cfg.Fans) == 0 {
		t.Fatal("DefaultConfig has no fans")
	}

	if cfg.Interval <= 0 {
		t.Errorf("interval = %v, want > 0", cfg.Interval)
	}

	for fi, fc := range cfg.Fans {
		if len(fc.Curve) == 0 {
			t.Fatalf("fan %d has empty curve", fi)
		}

		// Verify curve is sorted by temperature.
		for i := 1; i < len(fc.Curve); i++ {
			if fc.Curve[i].TempC <= fc.Curve[i-1].TempC {
				t.Errorf("fan %d curve not sorted: point %d (TempC=%d) <= point %d (TempC=%d)",
					fi, i, fc.Curve[i].TempC, i-1, fc.Curve[i-1].TempC)
			}
		}

		// Verify duty values are in valid range.
		for i, p := range fc.Curve {
			if p.DutyPercent < 0 || p.DutyPercent > 100 {
				t.Errorf("fan %d curve point %d: duty %d out of range [0, 100]", fi, i, p.DutyPercent)
			}
		}

		if fc.SmoothingFactor < 0 || fc.SmoothingFactor > 1 {
			t.Errorf("fan %d smoothing factor = %v, want [0, 1]", fi, fc.SmoothingFactor)
		}
	}
}
