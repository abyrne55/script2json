package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// CommandOutput represents a single command and its output in JSONL format
type CommandOutput struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Output  string `json:"output"`
}

// ANSI escape sequence patterns
var (
	// Remove ANSI escape sequences (colors, cursor movements, etc.)
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	// Remove [K sequences (erase line)
	eraseRegex = regexp.MustCompile(`\[K`)
	// Remove backspace sequences
	backspaceRegex = regexp.MustCompile(`.\x08`)
	// Remove control sequences like ]0; (window title changes)
	controlRegex = regexp.MustCompile(`\][0-9]+;[^\x07\x1b]*[\x07\x1b]`)
	// Remove other control characters including carriage returns
	otherControlRegex = regexp.MustCompile(`[\x00-\x08\x0b-\x1f\x7f]`)
	// Pattern to match shell prompt with ANSI sequences
	promptRegex = regexp.MustCompile(`\[[^]]*\][^$]*\$ `)
	// Pattern for control sequences that start with [?
	bracketControlRegex = regexp.MustCompile(`\[\?[0-9]+[hl]`)
)

// cleanText removes ANSI escape sequences and control characters
func cleanText(text string) string {
	// Remove ANSI escape sequences
	text = ansiRegex.ReplaceAllString(text, "")
	// Remove bracket control sequences
	text = bracketControlRegex.ReplaceAllString(text, "")
	// Remove erase sequences
	text = eraseRegex.ReplaceAllString(text, "")
	// Remove control sequences
	text = controlRegex.ReplaceAllString(text, "")
	// Remove other control characters except newlines and tabs
	text = otherControlRegex.ReplaceAllString(text, "")
	// Handle backspace sequences (remove character + backspace)
	for backspaceRegex.MatchString(text) {
		text = backspaceRegex.ReplaceAllString(text, "")
	}
	return text
}

// extractCommand extracts the command from a shell prompt line
func extractCommand(line string) string {
	// First, find the $ prompt marker
	dollarIndex := strings.LastIndex(line, "$ ")
	if dollarIndex == -1 {
		return ""
	}

	// Extract everything after the $ prompt
	commandPart := line[dollarIndex+2:]

	// Now simulate the command line editing by processing control sequences
	result := ""
	i := 0
	for i < len(commandPart) {
		if i < len(commandPart) && commandPart[i] == '\x1b' {
			// Handle ANSI escape sequences
			j := i + 1
			if j < len(commandPart) {
				if commandPart[j] == '[' {
					// CSI sequence: ESC [
					j++
					// Skip parameters (digits, semicolons, question marks)
					for j < len(commandPart) && (commandPart[j] >= '0' && commandPart[j] <= '9' ||
						commandPart[j] == ';' || commandPart[j] == '?' || commandPart[j] == '=' ||
						commandPart[j] == '<' || commandPart[j] == '>') {
						j++
					}
					// Check for specific commands
					if j < len(commandPart) {
						cmd := commandPart[j]
						if cmd == 'K' {
							// Erase to end of line - clear result
							result = ""
						} else if cmd == 'P' {
							// Delete character - remove last character
							if len(result) > 0 {
								result = result[:len(result)-1]
							}
						} else if cmd == 'D' {
							// Cursor left - could be used for editing, treat as backspace
							if len(result) > 0 {
								result = result[:len(result)-1]
							}
						}
						j++ // Skip the command letter
					}
				} else if commandPart[j] == ']' {
					// OSC sequence: ESC ]
					j++
					// Skip until BEL (0x07) or ESC \
					for j < len(commandPart) && commandPart[j] != '\x07' {
						if commandPart[j] == '\x1b' && j+1 < len(commandPart) && commandPart[j+1] == '\\' {
							j += 2
							break
						}
						j++
					}
					if j < len(commandPart) && commandPart[j] == '\x07' {
						j++ // Skip BEL
					}
				} else {
					// Other escape sequences, skip next character
					j++
				}
			}
			i = j
		} else if i+1 < len(commandPart) && (commandPart[i] == '\x08' || commandPart[i] == '\x7f') {
			// Backspace or DEL - remove last character
			if len(result) > 0 {
				result = result[:len(result)-1]
			}
			i++
		} else if commandPart[i] >= 32 && commandPart[i] <= 126 {
			// Regular printable character
			result += string(commandPart[i])
			i++
		} else {
			// Skip all other control characters (0x00-0x1F except handled ones)
			i++
		}
	}

	return strings.TrimSpace(result)
}

// isScriptHeader checks if a line is the script start/end header
func isScriptHeader(line string) bool {
	cleaned := cleanText(line)
	return strings.Contains(cleaned, "Script started on") ||
		strings.Contains(cleaned, "Script done on")
}

// isPromptLine checks if a line contains a shell prompt
func isPromptLine(line string) bool {
	cleaned := cleanText(line)
	// Look for typical shell prompt patterns
	return strings.Contains(cleaned, "$ ") && len(strings.TrimSpace(cleaned)) > 2
}

// isControlSequenceLine checks if a line is just control sequences
func isControlSequenceLine(line string) bool {
	cleaned := cleanText(line)
	return strings.TrimSpace(cleaned) == ""
}

// isExitCommand checks if the line contains just "exit"
func isExitCommand(line string) bool {
	cleaned := strings.TrimSpace(cleanText(line))
	return cleaned == "exit"
}

func processLine(line string, commandID *int, currentCommand *string, outputLines *[]string, inCommand *bool) {
	// Skip script headers
	if isScriptHeader(line) {
		return
	}

	// Skip exit command
	if isExitCommand(line) {
		return
	}

	// Skip pure control sequence lines
	if isControlSequenceLine(line) {
		return
	}

	// Check if this is a prompt line with a command
	if isPromptLine(line) {
		// If we were processing a previous command, output it
		if *inCommand && *currentCommand != "" {
			output := strings.TrimSpace(strings.Join(*outputLines, "\n"))

			cmdOutput := CommandOutput{
				ID:      fmt.Sprintf("%d", *commandID),
				Command: *currentCommand,
				Output:  output,
			}

			jsonBytes, _ := json.Marshal(cmdOutput)
			fmt.Println(string(jsonBytes))
			os.Stdout.Sync() // Force flush
			(*commandID)++
		}

		// Start new command
		command := extractCommand(line)
		if command != "" && command != "exit" {
			*currentCommand = command
			*outputLines = []string{}
			*inCommand = true
		} else {
			*inCommand = false
		}
		return
	}

	// If we're in a command and this isn't a control sequence line, collect output
	if *inCommand {
		cleaned := cleanText(line)
		// Skip lines that are just control sequences
		if strings.TrimSpace(cleaned) != "" {
			*outputLines = append(*outputLines, strings.TrimSpace(cleaned))
		}
	}
}

func main() {
	commandID := 1
	var currentCommand string
	var outputLines []string
	inCommand := false

	buffer := make([]byte, 1)
	lineBuffer := ""

	for {
		n, err := os.Stdin.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		if n > 0 {
			char := buffer[0]

			if char == '\n' || char == '\r' {
				// Process complete line
				if len(lineBuffer) > 0 {
					processLine(lineBuffer, &commandID, &currentCommand, &outputLines, &inCommand)
					lineBuffer = ""
				}
			} else {
				// Add character to buffer
				lineBuffer += string(char)
			}
		}
	}

	// Process any remaining line
	if len(lineBuffer) > 0 {
		processLine(lineBuffer, &commandID, &currentCommand, &outputLines, &inCommand)
	}

	// Process any remaining command
	if inCommand && currentCommand != "" {
		output := strings.TrimSpace(strings.Join(outputLines, "\n"))

		cmdOutput := CommandOutput{
			ID:      fmt.Sprintf("%d", commandID),
			Command: currentCommand,
			Output:  output,
		}

		jsonBytes, _ := json.Marshal(cmdOutput)
		fmt.Println(string(jsonBytes))
		os.Stdout.Sync() // Force flush
	}
}
