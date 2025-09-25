package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
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
	flag.Parse()

	log.Printf("Starting script2json. FIFO path: %s", *fifoPath)

	if err := createFifo(*fifoPath); err != nil {
		log.Fatalf("Error creating FIFO: %v", err)
	}

	x := make(chan byte, 1024)
	y := make(chan string, 1)

	go fifoReader(*fifoPath, x)
	go lineEditor(x, y)
	go stdoutWriter(y)

	setupSignalHandling(x)

	select {}
}

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

func setupSignalHandling(x chan<- byte) {
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
				x <- EOF
			}
		}
	}()
}

func fifoReader(fifoPath string, x chan<- byte) {
	defer close(x)

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
			x <- buf[0]
		}
	}
}

func lineEditor(x <-chan byte, y chan<- string) {
	var buffer []byte
	var csiBuffer []byte
	cursor := 0
	inCSI := false
	inAlternateScreen := false

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

	for b := range x {
		if inCSI {
			csiBuffer = append(csiBuffer, b)
			if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '~' {
				inCSI = false
				handleCSI(csiBuffer, &buffer, &cursor, &inAlternateScreen)
				csiBuffer = nil
			}
			continue
		}

		if inAlternateScreen {
			continue
		}

		switch b {
		case EOF:
			y <- string(buffer)
			buffer = nil
			cursor = 0
			inAlternateScreen = false
		case ESC:
			b2, ok := <-x
			if !ok {
				continue
			}
			if b2 == CSI {
				inCSI = true
				csiBuffer = []byte{}
			}
		case BACKSPACE, DEL:
			if cursor > 0 {
				buffer = append(buffer[:cursor-1], buffer[cursor:]...)
				cursor--
			}
		case '\n', '\r':
			insertByte(b)
		default:
			if b >= 32 && b < 127 { // Printable characters
				insertByte(b)
			}
		}
	}
	close(y)
}

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

func stdoutWriter(y <-chan string) {
	for line := range y {
		fmt.Println(line)
	}
}
