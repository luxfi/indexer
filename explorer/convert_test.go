package explorer

import "testing"

func TestToBool(t *testing.T) {
	if !toBool(true) {
		t.Error("bool true")
	}
	if toBool(false) {
		t.Error("bool false")
	}
	if !toBool(int64(1)) {
		t.Error("int64 1")
	}
	if toBool(int64(0)) {
		t.Error("int64 0")
	}
	if !toBool(float64(1)) {
		t.Error("float64 1")
	}
	if toBool("") {
		t.Error("string empty")
	}
}

func TestToInt(t *testing.T) {
	if toInt(42) != 42 {
		t.Error("int")
	}
	if toInt(int64(99)) != 99 {
		t.Error("int64")
	}
	if toInt(float64(7.9)) != 7 {
		t.Error("float64")
	}
	if toInt("123") != 123 {
		t.Error("string")
	}
	if toInt(nil) != 0 {
		t.Error("nil")
	}
}
