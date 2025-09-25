package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	EOF         = 0x04
	ESC         = 0x1B
	BACKSPACE   = 0x08
	DEL         = 0x7F
	CSI         = '['
	ARROW_LEFT  = 'D'
	ARROW_RIGHT = 'C'
)

var reading atomic.Bool

func main() {
	fifoPath := flag.String("fifo", "/tmp/script.fifo", "Path to the FIFO to read from")
	debug := flag.Bool("debug", false, "Enable debug logging of the line editor buffer")
	flag.Parse()

	log.Printf("Starting script2json. FIFO path: %s", *fifoPath)

	if err := createFifo(*fifoPath); err != nil {
		log.Fatalf("Error creating FIFO: %v", err)
	}

	// fifoByteChan streams bytes from the FIFO reader to the line editor.
	fifoByteChan := make(chan byte, 1024)
	// processedLineChan sends the final, processed string from the line editor
	// to the stdout writer.
	processedLineChan := make(chan string, 1)

	// Start the concurrent processing pipeline.
	go fifoReader(*fifoPath, fifoByteChan)
	go lineEditor(fifoByteChan, processedLineChan, *debug)
	go stdoutWriter(processedLineChan)

	setupSignalHandling(fifoByteChan)

	select {}
}

// createFifo checks if the FIFO at the given path exists, and creates it if it does not.
// Returns an error if the FIFO cannot be created or stat-ed.
func createFifo(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("FIFO does not exist, creating at %s", path)
		if err := syscall.Mkfifo(path, 0666); err != nil {
			return fmt.Errorf("could not create fifo: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("could not stat fifo: %w", err)
	}
	return nil
}

// setupSignalHandling sets up signal handlers for SIGUSR1 and SIGUSR2.
// SIGUSR1 starts data processing by setting the reading flag to true.
// SIGUSR2 stops data processing by setting the reading flag to false and sends EOF to fifoByteChan.
func setupSignalHandling(fifoByteChan chan<- byte) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2)

	go func() {
		for sig := range sigs {
			switch sig {
			case syscall.SIGUSR1:
				log.Println("Received SIGUSR1. Starting to process data.")
				reading.Store(true)
			case syscall.SIGUSR2:
				log.Println("Received SIGUSR2. Stopping data processing.")
				reading.Store(false)
				fifoByteChan <- EOF
			}
		}
	}()
}

// fifoReader opens the FIFO at the specified path, reads it byte-by-byte,
// and sends each byte to the fifoByteChan when reading is enabled.
func fifoReader(fifoPath string, fifoByteChan chan<- byte) {
	defer close(fifoByteChan)

	f, err := os.OpenFile(fifoPath, os.O_RDONLY, 0666)
	if err != nil {
		log.Fatalf("Error opening FIFO: %v", err)
	}
	defer f.Close()

	log.Println("FIFO opened for reading.")

	buf := make([]byte, 1)
	for {
		_, err := f.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from FIFO: %v", err)
			}
			break
		}
		if reading.Load() {
			fifoByteChan <- buf[0]
		}
	}
}

// lineEditor reads bytes from fifoByteChan and processes them into a clean
// buffer, handling ANSI control sequences for cursor movement, backspace, and
// alternate screen mode. When it receives an EOF, it sends the cleaned buffer
// as a string to the processedLineChan.
func lineEditor(fifoByteChan <-chan byte, processedLineChan chan<- string, debug bool) {
	var buffer []byte
	var mu sync.Mutex
	var csiBuffer []byte
	cursor := 0
	inCSI := false
	inAlternateScreen := false

	if debug {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				mu.Lock()
				bufCopy := make([]byte, len(buffer))
				copy(bufCopy, buffer)
				mu.Unlock()
				log.Printf("[DEBUG] lineEditor buffer: %q", string(bufCopy))
			}
		}()
	}

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

	for b := range fifoByteChan {
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
			processedLineChan <- string(buffer)
			buffer = nil
			cursor = 0
			mu.Unlock()
		case ESC:
			b2, ok := <-fifoByteChan
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
	close(processedLineChan)
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

// stdoutWriter waits for a processed line to appear on the processedLineChan
// and prints it directly to standard output.
func stdoutWriter(processedLineChan <-chan string) {
	for line := range processedLineChan {
		fmt.Println(line)
	}
}
