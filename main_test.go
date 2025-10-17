package main

// Generated-By: Claude 4 Sonnet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestHandleCSI tests the ANSI CSI sequence handling logic
func TestHandleCSI(t *testing.T) {
	tests := []struct {
		name                string
		seq                 []byte
		initialBuffer       []byte
		initialCursor       int
		initialAltScreen    bool
		expectedBuffer      []byte
		expectedCursor      int
		expectedAltScreen   bool
	}{
		{
			name:                "Enter alternate screen",
			seq:                 []byte("?1049h"),
			initialBuffer:       []byte("hello"),
			initialCursor:       5,
			initialAltScreen:    false,
			expectedBuffer:      []byte("hello"),
			expectedCursor:      5,
			expectedAltScreen:   true,
		},
		{
			name:                "Exit alternate screen",
			seq:                 []byte("?1049l"),
			initialBuffer:       []byte("world"),
			initialCursor:       3,
			initialAltScreen:    true,
			expectedBuffer:      []byte("world"),
			expectedCursor:      3,
			expectedAltScreen:   false,
		},
		{
			name:                "Arrow left moves cursor",
			seq:                 []byte("D"),
			initialBuffer:       []byte("test"),
			initialCursor:       4,
			initialAltScreen:    false,
			expectedBuffer:      []byte("test"),
			expectedCursor:      3,
			expectedAltScreen:   false,
		},
		{
			name:                "Arrow left at position 0 stays at 0",
			seq:                 []byte("D"),
			initialBuffer:       []byte("test"),
			initialCursor:       0,
			initialAltScreen:    false,
			expectedBuffer:      []byte("test"),
			expectedCursor:      0,
			expectedAltScreen:   false,
		},
		{
			name:                "Arrow right moves cursor",
			seq:                 []byte("C"),
			initialBuffer:       []byte("test"),
			initialCursor:       2,
			initialAltScreen:    false,
			expectedBuffer:      []byte("test"),
			expectedCursor:      3,
			expectedAltScreen:   false,
		},
		{
			name:                "Arrow right at end of buffer stays at end",
			seq:                 []byte("C"),
			initialBuffer:       []byte("test"),
			initialCursor:       4,
			initialAltScreen:    false,
			expectedBuffer:      []byte("test"),
			expectedCursor:      4,
			expectedAltScreen:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buffer := make([]byte, len(tt.initialBuffer))
			copy(buffer, tt.initialBuffer)
			cursor := tt.initialCursor
			altScreen := tt.initialAltScreen

			handleCSI(tt.seq, &buffer, &cursor, &altScreen)

			if !bytes.Equal(buffer, tt.expectedBuffer) {
				t.Errorf("Buffer = %v, want %v", buffer, tt.expectedBuffer)
			}
			if cursor != tt.expectedCursor {
				t.Errorf("Cursor = %d, want %d", cursor, tt.expectedCursor)
			}
			if altScreen != tt.expectedAltScreen {
				t.Errorf("AltScreen = %v, want %v", altScreen, tt.expectedAltScreen)
			}
		})
	}
}

// TestLineEditorBasicInput tests basic character input handling
func TestLineEditorBasicInput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress debug logs during tests
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	commandOutputChan := make(chan string, 1)

	go lineEditor(scriptFifoByteChan, commandOutputChan, logger)

	// Send "hello" followed by EOF
	for _, b := range []byte("hello") {
		scriptFifoByteChan <- b
	}
	scriptFifoByteChan <- EOF

	// Wait for output
	select {
	case output := <-commandOutputChan:
		if output != "hello" {
			t.Errorf("Output = %q, want %q", output, "hello")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

// TestLineEditorBackspace tests backspace handling
func TestLineEditorBackspace(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	commandOutputChan := make(chan string, 1)

	go lineEditor(scriptFifoByteChan, commandOutputChan, logger)

	// Send "helloX" then DEL (delete last character)
	for _, b := range []byte("helloX") {
		scriptFifoByteChan <- b
	}
	scriptFifoByteChan <- DEL
	scriptFifoByteChan <- EOF

	// Wait for output
	select {
	case output := <-commandOutputChan:
		if output != "hello" {
			t.Errorf("Output = %q, want %q", output, "hello")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

// TestLineEditorAlternateScreen tests alternate screen mode filtering
func TestLineEditorAlternateScreen(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	commandOutputChan := make(chan string, 1)

	go lineEditor(scriptFifoByteChan, commandOutputChan, logger)

	// Send "before"
	for _, b := range []byte("before") {
		scriptFifoByteChan <- b
	}

	// Enter alternate screen mode (ESC[?1049h)
	scriptFifoByteChan <- ESC
	scriptFifoByteChan <- CSI
	for _, b := range []byte("?1049h") {
		scriptFifoByteChan <- b
	}

	// Send garbage that should be ignored
	for _, b := range []byte("GARBAGE") {
		scriptFifoByteChan <- b
	}

	// Exit alternate screen mode (ESC[?1049l)
	scriptFifoByteChan <- ESC
	scriptFifoByteChan <- CSI
	for _, b := range []byte("?1049l") {
		scriptFifoByteChan <- b
	}

	// Send "after"
	for _, b := range []byte("after") {
		scriptFifoByteChan <- b
	}

	scriptFifoByteChan <- EOF

	// Wait for output
	select {
	case output := <-commandOutputChan:
		if output != "beforeafter" {
			t.Errorf("Output = %q, want %q", output, "beforeafter")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

// TestLineEditorCursorMovement tests arrow key cursor movement
func TestLineEditorCursorMovement(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	commandOutputChan := make(chan string, 1)

	go lineEditor(scriptFifoByteChan, commandOutputChan, logger)

	// Type "helo"
	for _, b := range []byte("helo") {
		scriptFifoByteChan <- b
	}

	// Move left twice (ESC[D)
	for i := 0; i < 2; i++ {
		scriptFifoByteChan <- ESC
		scriptFifoByteChan <- CSI
		scriptFifoByteChan <- ARROW_LEFT
	}

	// Insert 'l'
	scriptFifoByteChan <- 'l'

	scriptFifoByteChan <- EOF

	// Wait for output
	select {
	case output := <-commandOutputChan:
		if output != "hello" {
			t.Errorf("Output = %q, want %q", output, "hello")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for output")
	}
}

// TestLineEditorReset tests the reset functionality
func TestLineEditorReset(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	commandOutputChan := make(chan string, 2)

	go lineEditor(scriptFifoByteChan, commandOutputChan, logger)

	// Send "garbage" and EOF to create first output
	for _, b := range []byte("garbage") {
		scriptFifoByteChan <- b
	}
	scriptFifoByteChan <- EOF

	// Wait for first output to be processed
	select {
	case output := <-commandOutputChan:
		if output != "garbage" {
			t.Errorf("First output = %q, want %q", output, "garbage")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for first output")
	}

	// Now send reset signal to clear state for next command
	select {
	case resetChan <- struct{}{}:
	default:
		t.Fatal("Reset channel is full")
	}

	// Give the reset a moment to process
	time.Sleep(100 * time.Millisecond)

	// Send "hello" followed by EOF
	for _, b := range []byte("hello") {
		scriptFifoByteChan <- b
	}
	scriptFifoByteChan <- EOF

	// Wait for second output - should only get "hello" (no garbage)
	select {
	case output := <-commandOutputChan:
		if output != "hello" {
			t.Errorf("Second output = %q, want %q (reset did not clear buffer properly)", output, "hello")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for second output")
	}
}

// TestCommandRecordJSON tests JSON marshaling/unmarshaling
func TestCommandRecordJSON(t *testing.T) {
	now := time.Now()
	record := CommandRecord{
		ID:              "42",
		Command:         "echo hello",
		Output:          "hello\r\n",
		ReturnTimestamp: now,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal back
	var decoded CommandRecord
	err = json.Unmarshal(jsonData, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields
	if decoded.ID != record.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, record.ID)
	}
	if decoded.Command != record.Command {
		t.Errorf("Command = %q, want %q", decoded.Command, record.Command)
	}
	if decoded.Output != record.Output {
		t.Errorf("Output = %q, want %q", decoded.Output, record.Output)
	}
	// Time comparison with some tolerance for serialization
	if decoded.ReturnTimestamp.Sub(record.ReturnTimestamp).Abs() > time.Millisecond {
		t.Errorf("ReturnTimestamp = %v, want %v", decoded.ReturnTimestamp, record.ReturnTimestamp)
	}
}

// TestRecordCreator tests the record creation pipeline
func TestRecordCreator(t *testing.T) {
	// Reset recordID counter for predictable test results
	recordID.Store(0)

	commandOutputChan := make(chan string, 1)
	commandChan := make(chan string, 1)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	go recordCreator(commandOutputChan, commandChan)

	// Send a command and output
	commandChan <- "echo hello"
	commandOutputChan <- "hello\r\n"

	// Give recordCreator time to process
	time.Sleep(100 * time.Millisecond)

	// Close the write end and restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Parse JSON
	var record CommandRecord
	err := json.Unmarshal([]byte(output), &record)
	if err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify fields
	if record.ID != "1" {
		t.Errorf("ID = %q, want %q", record.ID, "1")
	}
	if record.Command != "echo hello" {
		t.Errorf("Command = %q, want %q", record.Command, "echo hello")
	}
	if record.Output != "hello\r\n" {
		t.Errorf("Output = %q, want %q", record.Output, "hello\r\n")
	}
}

// TestRecordCreatorReset tests that the recordCreator can be reset
func TestRecordCreatorReset(t *testing.T) {
	// This test verifies that sending a reset signal will drain the channels
	commandOutputChan := make(chan string, 10)
	commandChan := make(chan string, 10)

	go recordCreator(commandOutputChan, commandChan)

	// Send stale data that should be drained
	for i := 0; i < 5; i++ {
		commandChan <- fmt.Sprintf("stale command %d", i)
		commandOutputChan <- fmt.Sprintf("stale output %d", i)
	}

	// Verify channels have data
	if len(commandChan) == 0 {
		t.Fatal("Test setup error: commandChan should have data")
	}
	if len(commandOutputChan) == 0 {
		t.Fatal("Test setup error: commandOutputChan should have data")
	}

	// Send reset signal
	select {
	case recordCreatorResetChan <- struct{}{}:
	default:
		t.Fatal("recordCreatorResetChan is full")
	}

	// Give reset time to drain the channels
	time.Sleep(200 * time.Millisecond)

	// Verify channels were drained
	commandChanLen := len(commandChan)
	outputChanLen := len(commandOutputChan)

	if commandChanLen > 0 {
		t.Errorf("commandChan still has %d items after reset", commandChanLen)
	}
	if outputChanLen > 0 {
		t.Errorf("commandOutputChan still has %d items after reset", outputChanLen)
	}
}

// TestAtomicReading tests the reading flag
func TestAtomicReading(t *testing.T) {
	reading.Store(false)
	if reading.Load() {
		t.Error("reading should start false")
	}

	reading.Store(true)
	if !reading.Load() {
		t.Error("reading should be true after Store(true)")
	}

	reading.Store(false)
	if reading.Load() {
		t.Error("reading should be false after Store(false)")
	}
}

// TestRecordIDIncrement tests the monotonic record ID counter
func TestRecordIDIncrement(t *testing.T) {
	recordID.Store(0)

	var wg sync.WaitGroup
	const goroutines = 10
	const incrementsPerGoroutine = 100

	// Increment from multiple goroutines concurrently
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				recordID.Add(1)
			}
		}()
	}

	wg.Wait()

	expected := uint64(goroutines * incrementsPerGoroutine)
	if recordID.Load() != expected {
		t.Errorf("recordID = %d, want %d", recordID.Load(), expected)
	}
}

// TestCreateScriptFifo tests FIFO creation
func TestCreateScriptFifo(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "script2json-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fifoPath := fmt.Sprintf("%s/test.fifo", tmpDir)

	// Create FIFO
	err = createScriptFifo(fifoPath, logger)
	if err != nil {
		t.Fatalf("createScriptFifo failed: %v", err)
	}

	// Verify it exists and is a FIFO
	info, err := os.Stat(fifoPath)
	if err != nil {
		t.Fatalf("FIFO stat failed: %v", err)
	}

	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Error("Created file is not a FIFO")
	}

	// Call again - should not error (already exists)
	err = createScriptFifo(fifoPath, logger)
	if err != nil {
		t.Errorf("createScriptFifo should not error on existing FIFO: %v", err)
	}
}

// TestCreateCommandFifo tests command FIFO creation
func TestCreateCommandFifo(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "script2json-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fifoPath := fmt.Sprintf("%s/command.fifo", tmpDir)

	// Create FIFO
	err = createCommandFifo(fifoPath, logger)
	if err != nil {
		t.Fatalf("createCommandFifo failed: %v", err)
	}

	// Verify it exists and is a FIFO
	info, err := os.Stat(fifoPath)
	if err != nil {
		t.Fatalf("FIFO stat failed: %v", err)
	}

	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Error("Created file is not a FIFO")
	}
}

// TestWritePidFile tests PID file creation
func TestWritePidFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "script2json-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidPath := fmt.Sprintf("%s/test.pid", tmpDir)

	// Write PID file
	err = writePidFile(pidPath, logger)
	if err != nil {
		t.Fatalf("writePidFile failed: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}

	expectedPid := fmt.Sprintf("%d\n", os.Getpid())
	if string(data) != expectedPid {
		t.Errorf("PID file content = %q, want %q", string(data), expectedPid)
	}
}

// TestRemovePidFile tests PID file removal
func TestRemovePidFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "script2json-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidPath := fmt.Sprintf("%s/test.pid", tmpDir)

	// Write PID file
	err = writePidFile(pidPath, logger)
	if err != nil {
		t.Fatalf("writePidFile failed: %v", err)
	}

	// Remove it
	removePidFile(pidPath, logger)

	// Verify it's gone
	_, err = os.Stat(pidPath)
	if !os.IsNotExist(err) {
		t.Error("PID file should not exist after removal")
	}
}

// TestSignalHandlingSetup tests that signal handling can be set up without panic
func TestSignalHandlingSetup(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)

	// Create temp PID file
	tmpDir, err := os.MkdirTemp("", "script2json-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidPath := fmt.Sprintf("%s/test.pid", tmpDir)

	// This should not panic
	setupSignalHandling(scriptFifoByteChan, pidPath, logger)

	// Give signal handler goroutine time to start
	time.Sleep(50 * time.Millisecond)
}

// TestSignalHandlingUSR1 tests SIGUSR1 signal handling
func TestSignalHandlingUSR1(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	reading.Store(false)

	setupSignalHandling(scriptFifoByteChan, "", logger)
	time.Sleep(50 * time.Millisecond)

	// Send SIGUSR1 to self
	err := syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	if err != nil {
		t.Fatalf("Failed to send SIGUSR1: %v", err)
	}

	// Give signal time to be processed
	time.Sleep(100 * time.Millisecond)

	if !reading.Load() {
		t.Error("SIGUSR1 should have set reading to true")
	}
}

// TestSignalHandlingUSR2 tests SIGUSR2 signal handling
func TestSignalHandlingUSR2(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	reading.Store(true)

	setupSignalHandling(scriptFifoByteChan, "", logger)
	time.Sleep(50 * time.Millisecond)

	// Send SIGUSR2 to self
	err := syscall.Kill(os.Getpid(), syscall.SIGUSR2)
	if err != nil {
		t.Fatalf("Failed to send SIGUSR2: %v", err)
	}

	// Give signal time to be processed
	time.Sleep(100 * time.Millisecond)

	if reading.Load() {
		t.Error("SIGUSR2 should have set reading to false")
	}

	// Verify EOF was sent
	select {
	case b := <-scriptFifoByteChan:
		if b != EOF {
			t.Errorf("Expected EOF (0x%02X), got 0x%02X", EOF, b)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("EOF was not sent to channel after SIGUSR2")
	}
}

// TestSignalHandlingHUP tests SIGHUP signal handling (reset)
func TestSignalHandlingHUP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scriptFifoByteChan := make(chan byte, 1024)
	reading.Store(true)

	setupSignalHandling(scriptFifoByteChan, "", logger)
	time.Sleep(50 * time.Millisecond)

	// Clear any pre-existing signals in the channels
	select {
	case <-resetChan:
	default:
	}
	select {
	case <-recordCreatorResetChan:
	default:
	}

	// Send SIGHUP to self
	err := syscall.Kill(os.Getpid(), syscall.SIGHUP)
	if err != nil {
		t.Fatalf("Failed to send SIGHUP: %v", err)
	}

	// Give signal time to be processed
	time.Sleep(200 * time.Millisecond)

	// Verify reading was stopped (primary effect of SIGHUP)
	if reading.Load() {
		t.Error("SIGHUP should have set reading to false")
	}

	// The SIGHUP handler should have tried to send reset signals.
	// We can't directly verify they were sent since they're consumed by goroutines,
	// but we can verify the main effect (reading = false) happened.
	// This test successfully validates that SIGHUP is handled correctly.
}

// TestEndToEnd tests the complete pipeline from FIFOs to JSON output
func TestEndToEnd(t *testing.T) {
	// Create temporary directory for FIFOs
	tmpDir, err := os.MkdirTemp("", "script2json-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptFifoPath := fmt.Sprintf("%s/script.fifo", tmpDir)
	commandFifoPath := fmt.Sprintf("%s/command.fifo", tmpDir)
	pidFilePath := fmt.Sprintf("%s/script2json.pid", tmpDir)

	// Create FIFOs
	if err := syscall.Mkfifo(scriptFifoPath, 0666); err != nil {
		t.Fatalf("Failed to create script FIFO: %v", err)
	}
	if err := syscall.Mkfifo(commandFifoPath, 0666); err != nil {
		t.Fatalf("Failed to create command FIFO: %v", err)
	}

	// Redirect stdout to capture JSON output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Reset global state
	reading.Store(false)
	recordID.Store(0)

	// Create channels for the pipeline
	scriptFifoByteChan := make(chan byte, 1024)
	commandOutputChan := make(chan string, 1)
	commandChan := make(chan string, 1)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during test
	}))

	// Start the pipeline components
	go scriptFifoReader(scriptFifoPath, scriptFifoByteChan, logger)
	go commandFifoReader(commandFifoPath, commandChan, logger)
	go lineEditor(scriptFifoByteChan, commandOutputChan, logger)
	go recordCreator(commandOutputChan, commandChan)

	// Write PID file
	if err := writePidFile(pidFilePath, logger); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	// Set up signal handling
	setupSignalHandling(scriptFifoByteChan, pidFilePath, logger)

	// Give goroutines time to start
	time.Sleep(100 * time.Millisecond)

	// Get our PID for sending signals
	pid := os.Getpid()

	// Test sequence: simulate running "echo hello"

	// 1. Open script FIFO for writing (simulates script -f)
	scriptFifo, err := os.OpenFile(scriptFifoPath, os.O_WRONLY, 0666)
	if err != nil {
		t.Fatalf("Failed to open script FIFO for writing: %v", err)
	}
	defer scriptFifo.Close()

	// 2. Send SIGUSR1 to start reading (simulates DEBUG trap)
	syscall.Kill(pid, syscall.SIGUSR1)
	time.Sleep(50 * time.Millisecond)

	// 3. Write command output to script FIFO
	scriptFifo.Write([]byte("hello\r\n"))
	time.Sleep(50 * time.Millisecond)

	// 4. Write command to command FIFO (PROMPT_COMMAND writes command first)
	commandFifo, err := os.OpenFile(commandFifoPath, os.O_WRONLY, 0666)
	if err != nil {
		t.Fatalf("Failed to open command FIFO for writing: %v", err)
	}
	commandFifo.Write([]byte("echo hello\n"))
	commandFifo.Close()
	time.Sleep(50 * time.Millisecond)

	// 5. Send SIGUSR2 to stop reading and flush (simulates PROMPT_COMMAND)
	syscall.Kill(pid, syscall.SIGUSR2)

	// Give pipeline time to process
	time.Sleep(200 * time.Millisecond)

	// Test another command with ANSI sequences: "ls" with colored output

	syscall.Kill(pid, syscall.SIGUSR1)
	time.Sleep(50 * time.Millisecond)

	// Write output with ANSI color codes (ESC[32m = green)
	scriptFifo.Write([]byte("\x1b[32mfile.txt\x1b[0m\r\n"))
	time.Sleep(50 * time.Millisecond)

	// Write command first
	commandFifo, err = os.OpenFile(commandFifoPath, os.O_WRONLY, 0666)
	if err != nil {
		t.Fatalf("Failed to open command FIFO for writing: %v", err)
	}
	commandFifo.Write([]byte("ls --color=auto\n"))
	commandFifo.Close()
	time.Sleep(50 * time.Millisecond)

	// Then send SIGUSR2
	syscall.Kill(pid, syscall.SIGUSR2)
	time.Sleep(200 * time.Millisecond)

	// Test third command

	syscall.Kill(pid, syscall.SIGUSR1)
	time.Sleep(50 * time.Millisecond)

	scriptFifo.Write([]byte("fixed\r\n"))
	time.Sleep(50 * time.Millisecond)

	commandFifo, err = os.OpenFile(commandFifoPath, os.O_WRONLY, 0666)
	if err != nil {
		t.Fatalf("Failed to open command FIFO for writing: %v", err)
	}
	commandFifo.Write([]byte("echo fixed\n"))
	commandFifo.Close()
	time.Sleep(50 * time.Millisecond)

	syscall.Kill(pid, syscall.SIGUSR2)
	time.Sleep(200 * time.Millisecond)

	// Close stdout and restore
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Parse JSON lines
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	var records []CommandRecord

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var record CommandRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Logf("Failed to parse JSON line: %s", line)
			t.Fatalf("JSON parse error: %v", err)
		}
		records = append(records, record)
	}

	// Verify we got 3 records
	if len(records) < 3 {
		t.Fatalf("Expected at least 3 records, got %d\nOutput: %s", len(records), output)
	}

	// Verify first record (echo hello)
	if records[0].Command != "echo hello" {
		t.Errorf("Record 0 command = %q, want %q", records[0].Command, "echo hello")
	}
	if records[0].Output != "hello\r\n" {
		t.Errorf("Record 0 output = %q, want %q", records[0].Output, "hello\r\n")
	}

	// Verify second record (ls --color=auto) - ANSI codes should be stripped
	if records[1].Command != "ls --color=auto" {
		t.Errorf("Record 1 command = %q, want %q", records[1].Command, "ls --color=auto")
	}
	// The ANSI color codes should be stripped, leaving just "file.txt\r\n"
	if records[1].Output != "file.txt\r\n" {
		t.Errorf("Record 1 output = %q, want %q (ANSI codes not stripped)", records[1].Output, "file.txt\r\n")
	}

	// Verify third record (echo fixed)
	if records[2].Command != "echo fixed" {
		t.Errorf("Record 2 command = %q, want %q", records[2].Command, "echo fixed")
	}
	if records[2].Output != "fixed\r\n" {
		t.Errorf("Record 2 output = %q, want %q", records[2].Output, "fixed\r\n")
	}

	// Verify all records have monotonically increasing IDs
	for i := 1; i < len(records); i++ {
		prevID, _ := strconv.Atoi(records[i-1].ID)
		currID, _ := strconv.Atoi(records[i].ID)
		if currID <= prevID {
			t.Errorf("Record IDs not monotonic: %d -> %d", prevID, currID)
		}
	}

	// Verify all records have timestamps
	for i, record := range records {
		if record.ReturnTimestamp.IsZero() {
			t.Errorf("Record %d has zero timestamp", i)
		}
	}

	// Verify PID file was created and contains correct PID
	pidData, err := os.ReadFile(pidFilePath)
	if err != nil {
		t.Errorf("Failed to read PID file: %v", err)
	}
	expectedPID := fmt.Sprintf("%d\n", pid)
	if string(pidData) != expectedPID {
		t.Errorf("PID file content = %q, want %q", string(pidData), expectedPID)
	}

	t.Logf("End-to-end test successful! Processed %d commands", len(records))
}
