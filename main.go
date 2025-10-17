package main

// Generated-By: Gemini 2.5 Pro and Claude 4 Sonnet

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// CommandRecord is a record of a single command and its output.
type CommandRecord struct {
	ID              string    `json:"id"`
	Command         string    `json:"command"`
	Output          string    `json:"output"`
	ReturnTimestamp time.Time `json:"return_timestamp"`
}

const (
	EOF         = 0x04
	ESC         = 0x1B
	BACKSPACE   = 0x08
	DEL         = 0x7F
	CSI         = '['
	ARROW_LEFT  = 'D'
	ARROW_RIGHT = 'C'
)

// reading is an atomic boolean flag used to indicate whether the program is currently reading from the script FIFO.
// It provides safe concurrent access for goroutines that need to check or update the reading state.
var reading atomic.Bool

// recordID is a monotonically increasing counter for CommandRecord IDs
var recordID atomic.Uint64

// resetChan is used to signal a reset of the lineEditor state
var resetChan = make(chan struct{}, 1)

// recordCreatorResetChan is used to signal a reset of the recordCreator state
var recordCreatorResetChan = make(chan struct{}, 1)

func main() {
	scriptFifoPath := flag.String("script-fifo", "/tmp/script.fifo", "Path to the script FIFO to read from")
	commandFifoPath := flag.String("command-fifo", "/tmp/command.fifo", "Path to the command FIFO to read from")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	pidFile := flag.String("pid-file", "", "Path to write PID file (optional)")
	flag.Parse()

	// Configure structured logging
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		log.Fatalf("Invalid log level: %s. Must be debug, info, warn, or error", *logLevel)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	logger.Debug("Starting script2json", "script_fifo_path", *scriptFifoPath)

	if err := createScriptFifo(*scriptFifoPath, logger); err != nil {
		logger.Error("Error creating script FIFO", "error", err)
		os.Exit(1)
	}

	if err := createCommandFifo(*commandFifoPath, logger); err != nil {
		logger.Error("Error creating command FIFO", "error", err)
		os.Exit(1)
	}

	// Write PID file if specified
	if *pidFile != "" {
		if err := writePidFile(*pidFile, logger); err != nil {
			logger.Error("Error writing PID file", "error", err)
			os.Exit(1)
		}
	}

	// scriptFifoByteChan streams bytes from the script FIFO reader to the line editor.
	scriptFifoByteChan := make(chan byte, 1024)
	// commandOutputChan sends the final, processed string from the line editor
	// to the record creator.
	commandOutputChan := make(chan string, 1)
	// commandChan streams command strings from the command FIFO reader to the record creator.
	commandChan := make(chan string, 1)

	// Start the concurrent processing pipeline.
	go scriptFifoReader(*scriptFifoPath, scriptFifoByteChan, logger)
	go commandFifoReader(*commandFifoPath, commandChan, logger)
	go lineEditor(scriptFifoByteChan, commandOutputChan, logger)
	go recordCreator(commandOutputChan, commandChan)

	setupSignalHandling(scriptFifoByteChan, *pidFile, logger)

	select {}
}

// createScriptFifo checks if the script FIFO at the given path exists, and creates it if it does not.
// Returns an error if the script FIFO cannot be created or stat-ed.
func createScriptFifo(path string, logger *slog.Logger) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Warn("Script FIFO does not exist, creating", "path", path)
		if err := syscall.Mkfifo(path, 0666); err != nil {
			return fmt.Errorf("could not create script fifo: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("could not stat script fifo: %w", err)
	}
	return nil
}

// createCommandFifo checks if the command FIFO at the given path exists, and creates it if it does not.
// Returns an error if the command FIFO cannot be created or stat-ed.
func createCommandFifo(path string, logger *slog.Logger) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Warn("Command FIFO does not exist, creating", "path", path)
		if err := syscall.Mkfifo(path, 0666); err != nil {
			return fmt.Errorf("could not create command fifo: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("could not stat command fifo: %w", err)
	}
	return nil
}

// writePidFile writes the current process ID to the specified file.
// Returns an error if the file cannot be created or written to.
func writePidFile(path string, logger *slog.Logger) error {
	pid := os.Getpid()
	pidStr := fmt.Sprintf("%d\n", pid)

	if err := os.WriteFile(path, []byte(pidStr), 0644); err != nil {
		return fmt.Errorf("could not write PID file: %w", err)
	}

	logger.Debug("PID file written", "path", path, "pid", pid)
	return nil
}

// removePidFile removes the PID file at the specified path.
// Logs a warning if the file cannot be removed, but does not return an error.
func removePidFile(path string, logger *slog.Logger) {
	if err := os.Remove(path); err != nil {
		logger.Warn("Could not remove PID file", "path", path, "error", err)
	} else {
		logger.Debug("PID file removed", "path", path)
	}
}

// setupSignalHandling sets up signal handlers for SIGUSR1, SIGUSR2, SIGHUP, and termination signals.
// SIGUSR1 starts data processing by setting the reading flag to true.
// SIGUSR2 stops data processing by setting the reading flag to false and sends EOF to scriptFifoByteChan.
// SIGHUP resets the lineEditor state to recover from desync conditions.
// Termination signals (SIGINT, SIGTERM) clean up the PID file and exit gracefully.
func setupSignalHandling(scriptFifoByteChan chan<- byte, pidFilePath string, logger *slog.Logger) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range sigs {
			switch sig {
			case syscall.SIGUSR1:
				logger.Debug("Received SIGUSR1, starting to process data")
				reading.Store(true)
			case syscall.SIGUSR2:
				logger.Debug("Received SIGUSR2, stopping data processing")
				reading.Store(false)
				scriptFifoByteChan <- EOF
			case syscall.SIGHUP:
				logger.Info("Received SIGHUP, resetting all pipeline state")
				// Stop reading to prevent corrupted data
				wasReading := reading.Load()
				reading.Store(false)

				// Send reset signal to lineEditor (non-blocking)
				select {
				case resetChan <- struct{}{}:
				default:
					// Reset already pending
				}

				// Send reset signal to recordCreator (non-blocking)
				select {
				case recordCreatorResetChan <- struct{}{}:
				default:
					// Reset already pending
				}

				// If we were reading, send EOF to flush current buffer
				if wasReading {
					scriptFifoByteChan <- EOF
				}

				logger.Info("Reset signals sent, all pipeline state will be cleared")
			case syscall.SIGINT, syscall.SIGTERM:
				logger.Debug("Received termination signal, cleaning up", "signal", sig)
				if pidFilePath != "" {
					removePidFile(pidFilePath, logger)
				}
				os.Exit(0)
			}
		}
	}()
}

// scriptFifoReader opens the script FIFO at the specified path, reads it byte-by-byte,
// and sends each byte to the scriptFifoByteChan when reading is enabled.
func scriptFifoReader(scriptFifoPath string, scriptFifoByteChan chan<- byte, logger *slog.Logger) {
	defer close(scriptFifoByteChan)

	f, err := os.OpenFile(scriptFifoPath, os.O_RDONLY, 0666)
	if err != nil {
		log.Fatalf("Error opening script FIFO: %v", err)
	}
	defer f.Close()

	logger.Debug("Script FIFO opened for reading")

	buf := make([]byte, 1)
	for {
		_, err := f.Read(buf)
		if err != nil {
			if err != io.EOF {
				logger.Error("Error reading from script FIFO", "error", err)
			}
			break
		}
		if reading.Load() {
			scriptFifoByteChan <- buf[0]
		}
	}
}

// commandFifoReader opens the command FIFO at the specified path, reads it line-by-line,
// and sends each line to the commandChan.
func commandFifoReader(commandFifoPath string, commandChan chan<- string, logger *slog.Logger) {
	defer close(commandChan)

	logger.Debug("Command FIFO reader starting")

	buf := make([]byte, 1024)
	var commandBuffer []byte

	for {
		// Re-open the FIFO for each read session
		f, err := os.OpenFile(commandFifoPath, os.O_RDONLY, 0666)
		if err != nil {
			logger.Error("Error opening command FIFO", "error", err)
			break
		}

		logger.Debug("Command FIFO opened for reading")

		// Read until EOF (writer closes)
		for {
			n, err := f.Read(buf)
			if err != nil {
				if err == io.EOF {
					logger.Debug("Command FIFO writer closed, will reopen")
					break // Break inner loop to reopen FIFO
				}
				logger.Error("Error reading from command FIFO", "error", err)
				f.Close()
				return
			}

			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					// Send complete command
					if len(commandBuffer) > 0 {
						commandChan <- string(commandBuffer)
						logger.Debug("Sent command to commandChan", "command", string(commandBuffer))
						commandBuffer = nil
					}
				} else {
					//logger.Debug("Appended byte to commandBuffer", "byte", string(buf[i]))
					commandBuffer = append(commandBuffer, buf[i])
				}
			}
		}

		f.Close()
		// Continue outer loop to reopen FIFO
	}
}

// lineEditor reads bytes from scriptFifoByteChan and processes them into a clean
// buffer, handling ANSI control sequences for cursor movement, backspace, and
// alternate screen mode. When it receives an EOF, it sends the cleaned buffer
// as a string to the commandOutputChan. Can be reset via resetChan to recover from desync.
func lineEditor(scriptFifoByteChan <-chan byte, commandOutputChan chan<- string, logger *slog.Logger) {
	var buffer []byte
	var mu sync.Mutex
	var csiBuffer []byte
	cursor := 0
	inCSI := false
	inAlternateScreen := false

	// drainChannel drains all pending bytes from scriptFifoByteChan
	drainChannel := func() {
		drained := 0
		for {
			select {
			case <-scriptFifoByteChan:
				drained++
			default:
				logger.Debug("lineEditor channel drained", "bytes_discarded", drained)
				return
			}
		}
	}

	// resetState clears all lineEditor state and drains input channel
	resetState := func() {
		mu.Lock()
		defer mu.Unlock()
		buffer = nil
		csiBuffer = nil
		cursor = 0
		inCSI = false
		inAlternateScreen = false
		logger.Debug("lineEditor state cleared")

		// Drain any buffered bytes from the input channel
		drainChannel()
	}

	// Start debug logging goroutine if debug level is enabled
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			bufCopy := make([]byte, len(buffer))
			copy(bufCopy, buffer)
			mu.Unlock()

			logger.Debug("lineEditor buffer state", "buffer", string(bufCopy), "cursor", cursor)
		}
	}()

	// Start goroutine to monitor for reset signals
	go func() {
		for range resetChan {
			resetState()
		}
	}()

	insertByte := func(b byte) {
		if cursor == len(buffer) {
			buffer = append(buffer, b)
		} else {
			buffer = append(buffer, 0)
			copy(buffer[cursor+1:], buffer[cursor:])
			buffer[cursor] = b
		}
		cursor++
	}

	for b := range scriptFifoByteChan {
		if inCSI {
			csiBuffer = append(csiBuffer, b)
			if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '~' {
				inCSI = false
				mu.Lock()
				handleCSI(csiBuffer, &buffer, &cursor, &inAlternateScreen)
				mu.Unlock()
				csiBuffer = nil
			}
			continue
		}

		// If in alternate screen mode, ignore everything except the ESCAPE character
		// which is needed to process the exit sequence.
		if inAlternateScreen && b != ESC {
			continue
		}

		switch b {
		case EOF:
			mu.Lock()
			commandOutputChan <- string(buffer)
			buffer = nil
			cursor = 0
			mu.Unlock()
		case ESC:
			b2, ok := <-scriptFifoByteChan
			if !ok {
				continue
			}
			if b2 == CSI {
				inCSI = true
				csiBuffer = []byte{}
			}
		case BACKSPACE, DEL:
			mu.Lock()
			if cursor > 0 {
				buffer = append(buffer[:cursor-1], buffer[cursor:]...)
				cursor--
			}
			mu.Unlock()
		case '\n', '\r':
			mu.Lock()
			insertByte(b)
			mu.Unlock()
		default:
			if b >= 32 && b < 127 { // Printable characters
				mu.Lock()
				insertByte(b)
				mu.Unlock()
			}
		}
	}
	close(commandOutputChan)
}

// handleCSI processes a Control Sequence Introducer (CSI) escape sequence.
// It updates the buffer, cursor position, and alternate screen mode state as appropriate.
// - seq: the CSI sequence bytes
// - buffer: pointer to the current line buffer
// - cursor: pointer to the current cursor position within the buffer
// - inAlternateScreen: pointer to a bool indicating if alternate screen mode is active
func handleCSI(seq []byte, buffer *[]byte, cursor *int, inAlternateScreen *bool) {
	if bytes.HasSuffix(seq, []byte("h")) && bytes.Contains(seq, []byte("?1049")) {
		*inAlternateScreen = true
	} else if bytes.HasSuffix(seq, []byte("l")) && bytes.Contains(seq, []byte("?1049")) {
		*inAlternateScreen = false
	} else if len(seq) > 0 {
		switch seq[len(seq)-1] {
		case ARROW_LEFT:
			if *cursor > 0 {
				(*cursor)--
			}
		case ARROW_RIGHT:
			if *cursor < len(*buffer) {
				(*cursor)++
			}
		}
	}
}

// recordCreator creates CommandRecord instances from output and command data.
// It sets a monotonically increasing ID, return timestamp, copies data from commandOutputChan
// into the Output field, and reads from commandChan into the Command field.
// Can be reset via recordCreatorResetChan to drain stale data.
func recordCreator(commandOutputChan <-chan string, commandChan <-chan string) {
	// Start goroutine to monitor for reset signals
	go func() {
		for range recordCreatorResetChan {
			// Drain commandOutputChan
			outputDrained := 0
			for {
				select {
				case <-commandOutputChan:
					outputDrained++
				default:
					slog.Debug("recordCreator commandOutputChan drained", "items_discarded", outputDrained)
					goto drainCommands
				}
			}

		drainCommands:
			// Drain commandChan
			commandDrained := 0
			for {
				select {
				case <-commandChan:
					commandDrained++
				default:
					slog.Debug("recordCreator commandChan drained", "items_discarded", commandDrained)
					slog.Info("recordCreator channels drained", "outputs_discarded", outputDrained, "commands_discarded", commandDrained)
					return
				}
			}
		}
	}()

	for output := range commandOutputChan {
		// Read the corresponding command
		var command string
		select {
		case command = <-commandChan:
			// Got a command
		default:
			// No command available, use empty string
			command = ""
		}

		// Create the record
		record := CommandRecord{
			ID:              strconv.FormatUint(recordID.Add(1), 10),
			Command:         command,
			Output:          output,
			ReturnTimestamp: time.Now(),
		}

		// Output as JSON
		jsonData, err := json.Marshal(record)
		if err != nil {
			log.Printf("Error marshaling record to JSON: %v", err)
			continue
		}

		fmt.Println(string(jsonData))
	}
}
