package device

import (
	"testing"
)

func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		v        int
		min, max int
		want     int
	}{
		{"within range", 50, 0, 100, 50},
		{"below min", -5, 0, 100, 0},
		{"above max", 150, 0, 100, 100},
		{"at min", 0, 0, 100, 0},
		{"at max", 100, 0, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clamp(tt.v, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("clamp(%d, %d, %d) = %d, want %d", tt.v, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

func TestFanStatusParsing(t *testing.T) {
	tests := []struct {
		name       string
		regValue   byte
		wantManual bool
		wantDuty   int
	}{
		{"auto mode", 0x00, false, 0},
		{"manual 50%", 0xB2, true, 50},
		{"manual 100%", 0xE4, true, 100},
		{"manual 0%", 0x80, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manual := tt.regValue&0x80 != 0
			duty := int(tt.regValue & 0x7F)

			if manual != tt.wantManual {
				t.Errorf("manual = %v, want %v", manual, tt.wantManual)
			}
			if duty != tt.wantDuty {
				t.Errorf("duty = %d, want %d", duty, tt.wantDuty)
			}
		})
	}
}
