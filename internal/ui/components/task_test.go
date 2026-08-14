package components

import (
	"errors"
	"testing"
	"time"
)

func TestNewTask(t *testing.T) {
	before := time.Now()
	task := NewTask("sync repos")
	after := time.Now()

	if task.Name != "sync repos" {
		t.Errorf("Name = %q, want sync repos", task.Name)
	}
	if task.StartTime.Before(before) || task.StartTime.After(after) {
		t.Errorf("StartTime = %v, want between %v and %v", task.StartTime, before, after)
	}
	if task.EndTime != nil {
		t.Fatal("EndTime should be nil for a new task")
	}
}

func TestComplete_SetsEndTime(t *testing.T) {
	start := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	task := Task{Name: "build", StartTime: start}

	before := time.Now()
	task.Complete()
	after := time.Now()

	if task.EndTime == nil {
		t.Fatal("EndTime is nil after Complete")
	}
	if task.EndTime.Before(before) || task.EndTime.After(after) {
		t.Errorf("EndTime = %v, want between %v and %v", task.EndTime, before, after)
	}
}

func TestFail_RecordsErrorAndCompletes(t *testing.T) {
	start := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	task := Task{Name: "deploy", StartTime: start}
	err := errors.New("timeout")

	task.Fail(err)

	if task.Error != err {
		t.Fatalf("Error = %v, want %v", task.Error, err)
	}
	if task.EndTime == nil {
		t.Fatal("EndTime is nil after Fail")
	}
}

func TestDuration_Completed(t *testing.T) {
	start := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	end := start.Add(1500 * time.Millisecond)
	task := Task{
		Name:      "lint",
		StartTime: start,
		EndTime:   &end,
	}

	if got := task.Duration(); got != 1500*time.Millisecond {
		t.Errorf("Duration() = %v, want 1500ms", got)
	}
}

func TestDurationString_SubSecond(t *testing.T) {
	start := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	end := start.Add(250 * time.Millisecond)
	task := Task{
		Name:      "fast",
		StartTime: start,
		EndTime:   &end,
	}

	if got := task.DurationString(); got != "250ms" {
		t.Errorf("DurationString() = %q, want 250ms", got)
	}
}

func TestDurationString_AtLeastOneSecond(t *testing.T) {
	start := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	end := start.Add(2500 * time.Millisecond)
	task := Task{
		Name:      "slow",
		StartTime: start,
		EndTime:   &end,
	}

	if got := task.DurationString(); got != "2.5s" {
		t.Errorf("DurationString() = %q, want 2.5s", got)
	}
}

func TestDurationString_ExactlyOneSecond(t *testing.T) {
	start := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	task := Task{
		Name:      "one",
		StartTime: start,
		EndTime:   &end,
	}

	if got := task.DurationString(); got != "1.0s" {
		t.Errorf("DurationString() = %q, want 1.0s", got)
	}
}
