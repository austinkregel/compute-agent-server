package main

import (
	"testing"
)

func TestHandleVersionFlag_True(t *testing.T) {
	if !handleVersionFlag(true) {
		t.Error("handleVersionFlag(true) = false, want true")
	}
}

func TestHandleVersionFlag_False(t *testing.T) {
	if handleVersionFlag(false) {
		t.Error("handleVersionFlag(false) = true, want false")
	}
}
