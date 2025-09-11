package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/acarl005/stripansi"
)

type Record struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Output  string `json:"output"`
}

type State int

const (
	// State to accumulate output until we find the next prompt.
	AccumulatingOutput State = iota
	// State when we have found a prompt and are waiting for the command to be terminated by a newline.
	ParsingCommand
)

var promptPatterns = [][]byte{[]byte("$ "), []byte("# "), []byte("> ")}

func main() {
	reader := bufio.NewReader(os.Stdin)
	id := 1
	state := AccumulatingOutput // Start by accumulating any initial output/headers.
	var currentCommand RawCommand
	var currentOutput bytes.Buffer
	var buffer bytes.Buffer

	p := make([]byte, 4096)

	for {
		n, err := reader.Read(p)
		if n > 0 {
			buffer.Write(p[:n])
		}

		// Process the buffer as many times as possible.
		for processBuffer(&buffer, &state, &currentCommand, &currentOutput, &id) {
		}

		if err == io.EOF {
			if currentCommand.raw != "" {
				currentOutput.Write(buffer.Bytes())
				emitRecord(id, currentCommand, currentOutput.Bytes())
			}
			break
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading from stdin: %v\n", err)
			break
		}
	}
}

// processBuffer attempts to process a chunk of data from the buffer.
// It returns true if it made progress, false if it needs more data.
func processBuffer(buffer *bytes.Buffer, state *State, cmd *RawCommand, output *bytes.Buffer, id *int) bool {
	if buffer.Len() == 0 {
		return false
	}

	if *state == AccumulatingOutput {
		// Find the next prompt in the buffer.
		promptPos, _ := findPrompt(buffer.Bytes())
		if promptPos == -1 {
			// No prompt found, so everything in the buffer is output.
			output.Write(buffer.Bytes())
			buffer.Reset()
			return true // We processed the whole buffer.
		}

		// A prompt was found. Everything before it belongs to the previous command's output.
		output.Write(buffer.Bytes()[:promptPos])

		// Now, emit the completed record for the *previous* command.
		if cmd.raw != "" {
			emitRecord(*id, *cmd, output.Bytes())
			*id++
		}

		// The new state is to parse the command that starts with the prompt we just found.
		buffer.Next(promptPos) // Consume the output part from the buffer.
		*state = ParsingCommand
		output.Reset()
		return true // State changed, so we made progress.
	}

	if *state == ParsingCommand {
		// The buffer now starts with a prompt. Find the end of the command line.
		newlinePos := bytes.IndexAny(buffer.Bytes(), "\r\n")
		if newlinePos == -1 {
			return false // Need more data to find the end of the command.
		}

		// We have a full command line.
		promptAndCmdBytes := buffer.Bytes()[:newlinePos]

		// Extract the raw command text after the prompt.
		promptPos, promptLen := findPrompt(promptAndCmdBytes)
		if promptPos != -1 {
			commandStart := promptPos + promptLen
			if commandStart <= len(promptAndCmdBytes) {
				*cmd = NewRawCommand(string(promptAndCmdBytes[commandStart:]))
			} else {
				*cmd = NewRawCommand("")
			}
		} else {
			// Fallback: This should not happen if the logic is correct.
			*cmd = NewRawCommand(string(promptAndCmdBytes))
		}

		// Consume the prompt and command from the buffer.
		buffer.Next(newlinePos)
		if bytes.HasPrefix(buffer.Bytes(), []byte("\r\n")) {
			buffer.Next(2)
		} else {
			buffer.Next(1)
		}

		// The new state is to accumulate output for this new command.
		*state = AccumulatingOutput
		return true // State changed, made progress.
	}

	return false
}

// findPrompt searches for a prompt in a byte slice. It's complex because of ANSI codes.
// It returns the starting position of the prompt and the length of the prompt marker itself.
func findPrompt(data []byte) (pos int, length int) {
	for i := len(data) - 1; i >= 0; i-- {
		for _, p := range promptPatterns {
			if bytes.HasPrefix(data[i:], p) {
				return i, len(p)
			}
		}
	}
	return -1, 0
}

func emitRecord(id int, command RawCommand, output []byte) {
	renderedCommand := command.Render()
	if renderedCommand == "" || renderedCommand == "exit" {
		return
	}

	record := Record{
		ID:      fmt.Sprintf("%d", id),
		Command: renderedCommand,
		Output:  strings.TrimSpace(stripansi.Strip(string(output))),
	}

	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(record); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
	}
}

// RawCommand holds the raw command string and can render it
type RawCommand struct {
	raw string
}

func NewRawCommand(raw string) RawCommand {
	return RawCommand{raw: raw}
}

func (rc RawCommand) Render() string {
	var runes []rune
	cursor := 0
	rawRunes := []rune(rc.raw)

	i := 0
	for i < len(rawRunes) {
		r := rawRunes[i]
		switch r {
		case '\b', '\x7f': // backspace or delete
			if cursor > 0 {
				if cursor <= len(runes) {
					runes = append(runes[:cursor-1], runes[cursor:]...)
				}
				cursor--
			}
		case '\x1b': // ANSI escape sequence
			i++
			if i < len(rawRunes) && rawRunes[i] == '[' {
				i++
				start := i
				for i < len(rawRunes) && !((rawRunes[i] >= 'a' && rawRunes[i] <= 'z') || (rawRunes[i] >= 'A' && rawRunes[i] <= 'Z')) {
					i++
				}
				if i < len(rawRunes) {
					cmd := rawRunes[i]
					switch cmd {
					case 'C':
						if cursor < len(runes) {
							cursor++
						}
					case 'D':
						if cursor > 0 {
							cursor--
						}
					case 'K':
						if cursor < len(runes) {
							runes = runes[:cursor]
						}
					}
				} else {
					i = start
				}
			}
		default:
			if r >= 32 { // Printable characters
				if cursor == len(runes) {
					runes = append(runes, r)
				} else {
					runes = append(runes[:cursor], append([]rune{r}, runes[cursor:]...)...)
				}
				cursor++
			}
		}
		i++
	}

	return strings.TrimSpace(string(runes))
}
