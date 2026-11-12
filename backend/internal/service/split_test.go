package service

import "testing"

func TestBuildClipRanges(t *testing.T) {
	ranges := buildClipRanges(10_000, []float64{2.0, 5.5, 8.0}, 500)
	if len(ranges) != 4 {
		t.Fatalf("got %d ranges, want 4: %+v", len(ranges), ranges)
	}
	want := []clipRange{{0, 2000}, {2000, 5500}, {5500, 8000}, {8000, 10000}}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("range %d = %+v, want %+v", i, ranges[i], want[i])
		}
	}
}

func TestBuildClipRangesNoCuts(t *testing.T) {
	ranges := buildClipRanges(10_000, nil, 500)
	if len(ranges) != 0 {
		t.Fatalf("got %d ranges, want 0", len(ranges))
	}
}

func TestBuildClipRangesFoldsShort(t *testing.T) {
	// 0.2s after start and a flash 0.3s after a real cut should fold.
	ranges := buildClipRanges(10_000, []float64{0.2, 3.0, 3.2}, 400)
	if len(ranges) != 2 {
		t.Fatalf("got %d ranges, want 2: %+v", len(ranges), ranges)
	}
	if ranges[0].startMs != 0 || ranges[0].endMs != 3000 {
		t.Fatalf("first = %+v, want 0-3000", ranges[0])
	}
}
