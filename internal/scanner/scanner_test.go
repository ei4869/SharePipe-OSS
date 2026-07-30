package scanner

import (
	"bytes"
	"context"
	"runtime"
	"testing"
	"time"
)

func TestCommandClassifiesExitCodes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command fixture")
	}
	scanner, err := NewCommand(CommandOptions{
		Command: []string{"cmd.exe", "/d", "/c", "more >NUL & exit /b 1"},
		Timeout: time.Second, CleanExitCodes: []int{0}, InfectedExitCodes: []int{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), Metadata{Path: "sample.bin", Size: 4}, bytes.NewBufferString("test"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != VerdictInfected {
		t.Fatalf("expected infected verdict, received %q", result.Verdict)
	}
}

func TestCommandRejectsOverlappingExitCodes(t *testing.T) {
	_, err := NewCommand(CommandOptions{
		Command: []string{"scanner"}, Timeout: time.Second,
		CleanExitCodes: []int{0}, InfectedExitCodes: []int{0},
	})
	if err == nil {
		t.Fatal("expected overlapping exit codes to be rejected")
	}
}
