package tuilib

import "testing"

func TestPickerGeom_ListRowOffset(t *testing.T) {
	geom := PickerGeom{
		Valid:       true,
		HasList:     true,
		TopY:        10,
		BottomY:     20,
		ListTopY:    12,
		ListBottomY: 18,
	}

	// In range
	offset, ok := geom.ListRowOffset(5, 12, 80)
	if !ok || offset != 0 {
		t.Errorf("ListRowOffset(5, 12, 80) = (%d, %v), want (0, true)", offset, ok)
	}

	offset, ok = geom.ListRowOffset(5, 15, 80)
	if !ok || offset != 3 {
		t.Errorf("ListRowOffset(5, 15, 80) = (%d, %v), want (3, true)", offset, ok)
	}

	// Out of X range
	if _, ok := geom.ListRowOffset(-1, 15, 80); ok {
		t.Errorf("ListRowOffset with negative X should fail")
	}
	if _, ok := geom.ListRowOffset(80, 15, 80); ok {
		t.Errorf("ListRowOffset with X >= width should fail")
	}

	// Out of Y range
	if _, ok := geom.ListRowOffset(5, 11, 80); ok {
		t.Errorf("ListRowOffset with Y < ListTopY should fail")
	}
	if _, ok := geom.ListRowOffset(5, 19, 80); ok {
		t.Errorf("ListRowOffset with Y > ListBottomY should fail")
	}

	// Invalid / No list
	geomInvalid := geom
	geomInvalid.Valid = false
	if _, ok := geomInvalid.ListRowOffset(5, 15, 80); ok {
		t.Errorf("ListRowOffset with Valid=false should fail")
	}

	geomNoList := geom
	geomNoList.HasList = false
	if _, ok := geomNoList.ListRowOffset(5, 15, 80); ok {
		t.Errorf("ListRowOffset with HasList=false should fail")
	}
}

func TestPickerGeom_ContainsWheel(t *testing.T) {
	geom := PickerGeom{
		Valid:   true,
		HasList: true,
		TopY:    10,
		BottomY: 20,
	}

	if !geom.ContainsWheel(10) {
		t.Errorf("ContainsWheel(10) should be true")
	}
	if !geom.ContainsWheel(15) {
		t.Errorf("ContainsWheel(15) should be true")
	}
	if !geom.ContainsWheel(20) {
		t.Errorf("ContainsWheel(20) should be true")
	}
	if geom.ContainsWheel(9) {
		t.Errorf("ContainsWheel(9) should be false")
	}
	if geom.ContainsWheel(21) {
		t.Errorf("ContainsWheel(21) should be false")
	}
}
