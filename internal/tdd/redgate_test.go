package tdd

import (
	"testing"

	"github.com/tu/tu-agent/internal/testresult"
)

func TestNewTestsRed(t *testing.T) {
	newFiles := []string{"core/src/test/java/com/acme/FooTest.java"}

	t.Run("suite green overall is never red", func(t *testing.T) {
		rep := testresult.Report{Cases: []testresult.Case{
			{Class: "com.acme.FooTest", Name: "x", Status: testresult.Pass},
		}}
		got := NewTestsRed(true, rep, newFiles)
		if got.OK {
			t.Fatal("green suite reported as red")
		}
	})

	t.Run("new test failing is red", func(t *testing.T) {
		rep := testresult.Report{Cases: []testresult.Case{
			{Class: "com.acme.FooTest", Name: "x", Status: testresult.Fail},
		}}
		got := NewTestsRed(false, rep, newFiles)
		if !got.OK {
			t.Fatalf("failing new test not red: %+v", got)
		}
	})

	t.Run("build failed, no report, is red", func(t *testing.T) {
		got := NewTestsRed(false, testresult.Report{}, newFiles)
		if !got.OK {
			t.Fatalf("build-fail not treated as red: %+v", got)
		}
	})

	t.Run("green-on-arrival new test is not red and is flagged", func(t *testing.T) {
		rep := testresult.Report{Cases: []testresult.Case{
			{Class: "com.acme.FooTest", Name: "x", Status: testresult.Pass},
			{Class: "com.acme.OtherTest", Name: "y", Status: testresult.Fail}, // suite red overall
		}}
		got := NewTestsRed(false, rep, newFiles)
		if got.OK {
			t.Fatal("green-on-arrival new test wrongly accepted as red")
		}
		if len(got.GreenFiles) != 1 || got.GreenFiles[0] != newFiles[0] {
			t.Fatalf("GreenFiles = %v, want %v", got.GreenFiles, newFiles)
		}
	})
}

func TestNewTestsRedClassNameCollision(t *testing.T) {
	// Regression: class-name suffix collision. File AFooTest.java (class com.acme.AFooTest)
	// should not match an unrelated passing case for com.acme.FooTest.
	// Before fix: AFooTest wrongly matched FooTest (HasSuffix without boundary),
	// causing AFooTest to be flagged green-on-arrival.
	// After fix: AFooTest has no case, treated as red (build failed → expected).
	newFiles := []string{"src/test/java/com/acme/AFooTest.java"}
	rep := testresult.Report{Cases: []testresult.Case{
		{Class: "com.acme.FooTest", Name: "x", Status: testresult.Pass},
	}}
	got := NewTestsRed(false, rep, newFiles)
	if !got.OK {
		t.Fatalf("AFooTest with no case wrongly flagged green: %+v", got)
	}
	if len(got.GreenFiles) != 0 {
		t.Fatalf("GreenFiles = %v, want empty", got.GreenFiles)
	}
}
