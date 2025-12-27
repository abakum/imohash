package imohash

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var tempDir string

func TestMain(m *testing.M) {
	flag.Parse()

	// Make a temp area for test files
	tempDir, _ = os.MkdirTemp(os.TempDir(), "imohash_test_data")
	ret := m.Run()
	os.RemoveAll(tempDir)
	os.Exit(ret)
}

func TestCustom(t *testing.T) {
	const sampleFile = "sample"
	var hash [Size]byte
	var err error

	sampleSize := 3
	sampleThreshold := 45
	imo := NewCustom(sampleSize, sampleThreshold)

	// empty file
	os.WriteFile(sampleFile, []byte{}, 0666)
	hash, err = imo.SumFile(sampleFile)
	ok(t, err)
	equal(t, hash, [Size]byte{})

	// small file
	os.WriteFile(sampleFile, []byte("hello"), 0666)
	hash, err = imo.SumFile(sampleFile)
	ok(t, err)

	hashStr := fmt.Sprintf("%x", hash)
	equal(t, hashStr, "05d8a7b341bd9b025b1e906a48ae1d19")

	/* boundary tests using the custom sample size */
	size := sampleThreshold

	// test that changing the gaps between sample zones does not affect the hash
	data := bytes.Repeat([]byte{'A'}, size)
	os.WriteFile(sampleFile, data, 0666)
	h1, _ := imo.SumFile(sampleFile)

	data[sampleSize] = 'B'
	data[size-sampleSize-1] = 'B'
	os.WriteFile(sampleFile, data, 0666)
	h2, _ := imo.SumFile(sampleFile)
	equal(t, h1, h2)

	// test that changing a byte on the edge (but within) a sample zone
	// does change the hash
	data = bytes.Repeat([]byte{'A'}, size)
	data[sampleSize-1] = 'B'
	os.WriteFile(sampleFile, data, 0666)
	h3, _ := imo.SumFile(sampleFile)
	notEqual(t, h1, h3)

	data = bytes.Repeat([]byte{'A'}, size)
	data[size/2] = 'B'
	os.WriteFile(sampleFile, data, 0666)
	h4, _ := imo.SumFile(sampleFile)
	notEqual(t, h1, h4)
	notEqual(t, h3, h4)

	data = bytes.Repeat([]byte{'A'}, size)
	data[size/2+sampleSize-1] = 'B'
	os.WriteFile(sampleFile, data, 0666)
	h5, _ := imo.SumFile(sampleFile)
	notEqual(t, h1, h5)
	notEqual(t, h3, h5)
	notEqual(t, h4, h5)

	data = bytes.Repeat([]byte{'A'}, size)
	data[size-sampleSize] = 'B'
	os.WriteFile(sampleFile, data, 0666)
	h6, _ := imo.SumFile(sampleFile)
	notEqual(t, h1, h6)
	notEqual(t, h3, h6)
	notEqual(t, h4, h6)
	notEqual(t, h5, h6)

	// test that changing the size changes the hash
	data = bytes.Repeat([]byte{'A'}, size+1)
	os.WriteFile(sampleFile, data, 0666)
	h7, _ := imo.SumFile(sampleFile)
	notEqual(t, h1, h7)
	notEqual(t, h3, h7)
	notEqual(t, h4, h7)
	notEqual(t, h5, h7)
	notEqual(t, h6, h7)

	// test sampleSize < 1
	imo = NewCustom(0, size)
	data = bytes.Repeat([]byte{'A'}, size)
	os.WriteFile(sampleFile, data, 0666)
	hash, _ = imo.SumFile(sampleFile)
	hashStr = fmt.Sprintf("%x", hash)
	equal(t, hashStr, "2d9123b54d37e9b8f94ab37a7eca6f40")

	os.Remove(sampleFile)
}

// Test that the top level functions are the same as custom
// functions using the spec defaults.
func TestDefault(t *testing.T) {
	const sampleFile = "sample"
	var h1, h2 [Size]byte
	var testData []byte

	for _, size := range []int{100, 131071, 131072, 50000} {
		imo := NewCustom(16384, 131072)
		testData = M(size)
		equal(t, Sum(testData), imo.Sum(testData))
		os.WriteFile(sampleFile, []byte{}, 0666)
		h1, _ = SumFile(sampleFile)
		h2, _ = imo.SumFile(sampleFile)
		equal(t, h1, h2)
	}
	os.Remove(sampleFile)
}

// Testing helpers from: https://github.com/benbjohnson/testing

// ok fails the test if an err is not nil.
func ok(tb testing.TB, err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d: unexpected error: %s\033[39m\n\n", filepath.Base(file), line, err.Error())
		tb.FailNow()
	}
}

// equal fails the test if exp is not equal to act.
func equal(tb testing.TB, exp, act interface{}) {
	if !reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d:\n\n\texp: %#v\n\n\tgot: %#v\033[39m\n\n", filepath.Base(file), line, exp, act)
		tb.FailNow()
	}
}

// equal fails the test if exp is equal to act.
func notEqual(tb testing.TB, exp, act interface{}) {
	if reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d:\n\n\texpected mismatch, got matching\n\n\tgot: %#v\033[39m\n\n", filepath.Base(file), line, act)
		tb.FailNow()
	}
}

// TestContextCancellation tests that hashing can be cancelled via context
func TestContextCancellation(t *testing.T) {
	const sampleFile = "cancellation_test"
	const fileSize = 10 * 1024 * 1024 // 10MB file to ensure enough time to cancel

	// Create a large test file
	data := make([]byte, fileSize)
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(sampleFile, data, 0666)
	defer os.Remove(sampleFile)

	t.Run("CancelDuringFullHash", func(t *testing.T) {
		imo := NewCustom(SampleSize, SampleThreshold*100) // Set high threshold to force full hashing
		ctx, cancel := context.WithCancel(context.Background())
		imo.SetCtx(ctx)

		// Cancel immediately - should return almost instantly
		cancel()

		start := time.Now()
		_, err := imo.SumFile(sampleFile)
		elapsed := time.Since(start)

		// Should return quickly (not process entire file)
		if elapsed > 100*time.Millisecond {
			t.Errorf("Hashing should have been cancelled quickly, took %v", elapsed)
		}

		// No error should be returned for cancellation
		if err != nil {
			t.Errorf("Expected nil error on cancellation, got %v", err)
		}
	})

	t.Run("CancelDuringSampledHash", func(t *testing.T) {
		imo := New() // Use default settings (will sample 10MB file)
		ctx, cancel := context.WithCancel(context.Background())
		imo.SetCtx(ctx)

		// Cancel after a short delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		_, err := imo.SumFile(sampleFile)
		elapsed := time.Since(start)

		// Should return quickly
		if elapsed > 50*time.Millisecond {
			t.Errorf("Sampled hashing should have been cancelled quickly, took %v", elapsed)
		}

		if err != nil {
			t.Errorf("Expected nil error on cancellation, got %v", err)
		}
	})

	t.Run("NoCancelCompletesNormally", func(t *testing.T) {
		imo := New()
		ctx := context.Background()
		imo.SetCtx(ctx)

		hash, err := imo.SumFile(sampleFile)
		ok(t, err)

		// Should return a valid hash (not empty)
		if hash == emptyArray {
			t.Error("Expected non-empty hash when not cancelled")
		}

		// Compare with hash without context to ensure same result
		imo2 := New()
		hash2, err := imo2.SumFile(sampleFile)
		ok(t, err)

		equal(t, hash, hash2)
	})

	t.Run("CancelMidOperation", func(t *testing.T) {
		// Use a simpler approach - cancel after starting
		imo := New()
		ctx, cancel := context.WithCancel(context.Background())
		imo.SetCtx(ctx)

		// Cancel shortly after starting
		time.AfterFunc(5*time.Millisecond, cancel)

		start := time.Now()
		_, err := imo.SumFile(sampleFile)
		elapsed := time.Since(start)

		// Should return before completing full file
		// For a 10MB file, normal hashing takes much longer
		if elapsed > 100*time.Millisecond {
			t.Errorf("Should have cancelled mid-operation, took %v", elapsed)
		}

		if err != nil {
			t.Errorf("Expected nil error on mid-operation cancellation, got %v", err)
		}
	})

	t.Run("NilContextWorks", func(t *testing.T) {
		imo := New()

		// SetCtx with nil should not panic and should work normally
		imo.SetCtx(nil)

		hash, err := imo.SumFile(sampleFile)
		ok(t, err)

		// Should still produce valid hash
		if hash == emptyArray {
			t.Error("Expected valid hash even with nil context")
		}
	})

	t.Run("AlreadyCancelledContext", func(t *testing.T) {
		imo := New()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
		imo.SetCtx(ctx)

		start := time.Now()
		_, err := imo.SumFile(sampleFile)
		elapsed := time.Since(start)

		// Should return almost instantly
		if elapsed > 10*time.Millisecond {
			t.Errorf("Already cancelled context should return instantly, took %v", elapsed)
		}

		if err != nil {
			t.Errorf("Expected nil error with already cancelled context, got %v", err)
		}
	})
}

// TestConcurrentCancellation tests multiple concurrent hashing operations with cancellation
func TestConcurrentCancellation(t *testing.T) {
	const sampleFile = "concurrent_test"
	const fileSize = 5 * 1024 * 1024 // 5MB

	// Create test file
	data := make([]byte, fileSize)
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(sampleFile, data, 0666)
	defer os.Remove(sampleFile)

	// Test multiple goroutines cancelling independently
	const numGoroutines = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []string

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			imo := New()
			ctx, cancel := context.WithCancel(context.Background())
			imo.SetCtx(ctx)

			// Cancel at different times
			switch id % 3 {
			case 0:
				cancel() // Cancel immediately
			case 1:
				defer cancel() // Cancel at end
			default:
				// Cancel after short delay
				time.AfterFunc(time.Duration(id)*time.Millisecond, cancel)
			}

			_, err := imo.SumFile(sampleFile)

			mu.Lock()
			defer mu.Unlock()

			if id%3 != 0 {
				// Should complete or be cancelled mid-operation
				// Both outcomes are valid
				if err != nil {
					errors = append(errors, fmt.Sprintf("Goroutine %d: unexpected error: %v", id, err))
				}
			}
		}(i)
	}

	wg.Wait()

	// Report any errors
	if len(errors) > 0 {
		t.Errorf("Concurrent test failures:\n%s", strings.Join(errors, "\n"))
	}
}

// TestSetCtxMultipleTimes tests that SetCtx can be called multiple times
func TestSetCtxMultipleTimes(t *testing.T) {
	const sampleFile = "multictx_test"
	const smallSize = 1024

	// Create small test file
	data := make([]byte, smallSize)
	os.WriteFile(sampleFile, data, 0666)
	defer os.Remove(sampleFile)

	imo := New()

	// First with normal context
	ctx1, cancel1 := context.WithCancel(context.Background())
	imo.SetCtx(ctx1)
	hash1, err1 := imo.SumFile(sampleFile)
	ok(t, err1)

	// Change to cancelled context
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	imo.SetCtx(ctx2)
	hash2, err2 := imo.SumFile(sampleFile)

	// Check both results
	if hash1 == emptyArray {
		t.Error("First hash should be valid")
	}
	if err1 != nil {
		t.Errorf("First hash should not have error: %v", err1)
	}

	if hash2 != emptyArray {
		t.Error("Second hash should be empty (cancelled context)")
	}
	if err2 != nil {
		t.Errorf("Second hash should not have error (cancellation is silent): %v", err2)
	}

	// Cleanup
	cancel1()
}
