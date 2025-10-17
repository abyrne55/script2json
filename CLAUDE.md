# script2json - Project Overview for Claude

## What is script2json?

script2json is a **Go application** that bridges the gap between traditional terminal recording and structured data processing. It reads output from the Unix `script` utility (which captures terminal sessions), processes ANSI terminal control sequences, and outputs cleaned command/output pairs in JSON format.

This tool is a critical component of the [Shaidow](https://github.com/abyrne55/shaidow) project, where it enables real-time AI analysis of shell sessions by converting messy terminal output into structured data.

## Core Purpose

The Unix `script` command records everything that appears on a terminal, including:
- ANSI escape sequences (colors, cursor movements, etc.)
- User input with backspaces and corrections
- Alternate screen mode sequences (from programs like vim, less, etc.)
- Carriage returns and control characters

script2json solves the problem of extracting **clean, structured data** from this noisy output by:
1. Stripping ANSI control sequences
2. Simulating cursor movements and backspaces
3. Ignoring alternate screen mode content
4. Associating commands with their outputs
5. Outputting structured JSON records

## Architecture

### Concurrent Pipeline Design

script2json uses a **four-stage concurrent pipeline** with goroutines communicating via channels:

```
┌─────────────────┐     ┌──────────────┐     ┌──────────────────┐     ┌────────────────┐
│ scriptFifoReader│────>│ lineEditor   │────>│ recordCreator    │────>│ stdout (JSON)  │
└─────────────────┘     └──────────────┘     └──────────────────┘     └────────────────┘
                              ^                      ^
                              │                      │
                        (byte stream)          (command string)
                              │                      │
                        scriptFifoByteChan     commandChan
                                                     │
                                              ┌──────────────────┐
                                              │ commandFifoReader│
                                              └──────────────────┘
```

### Components

1. **scriptFifoReader** (goroutine)
   - Reads bytes from the script FIFO
   - Only sends bytes when `reading` flag is true (controlled by SIGUSR1/SIGUSR2)
   - Feeds raw terminal bytes into `scriptFifoByteChan`

2. **commandFifoReader** (goroutine)
   - Reads command strings from a separate command FIFO
   - Handles FIFO reopening when writer closes
   - Sends complete commands (newline-delimited) to `commandChan`

3. **lineEditor** (goroutine)
   - Processes bytes from `scriptFifoByteChan`
   - Maintains an internal buffer with cursor position
   - Handles ANSI escape sequences (CSI, cursor movements, backspace)
   - Detects and ignores alternate screen mode content
   - On EOF signal, sends cleaned buffer to `commandOutputChan`

4. **recordCreator** (goroutine)
   - Receives cleaned output from `commandOutputChan`
   - Matches with corresponding command from `commandChan`
   - Creates `CommandRecord` with monotonic ID and timestamp
   - Marshals to JSON and writes to stdout

### Signal Handling

script2json uses Unix signals to synchronize with the shell:

- **SIGUSR1**: Start reading (sets `reading` flag to true)
  - Sent by shell's DEBUG trap just before command execution

- **SIGUSR2**: Stop reading (sets `reading` flag to false, sends EOF)
  - Sent by shell's PROMPT_COMMAND after command completes

- **SIGHUP**: Reset lineEditor state (NEW - desync recovery)
  - Clears internal buffer, cursor position, CSI state, and alternate screen flag
  - Stops reading temporarily
  - Flushes current buffer if reading was active
  - Allows recovery from race conditions without full restart
  - Non-blocking implementation prevents multiple concurrent resets

- **SIGINT/SIGTERM**: Clean shutdown
  - Removes PID file if specified
  - Graceful exit

### Data Structures

#### CommandRecord
```go
type CommandRecord struct {
    ID              string    `json:"id"`               // Monotonically increasing
    Command         string    `json:"command"`           // The shell command
    Output          string    `json:"output"`            // Cleaned command output
    ReturnTimestamp time.Time `json:"return_timestamp"`  // When command completed
}
```

## Key Technical Details

### ANSI Escape Sequence Processing

The `lineEditor` goroutine handles several types of terminal control sequences:

1. **CSI (Control Sequence Introducer)**: `ESC [` sequences
   - Cursor movements (left/right arrows)
   - Alternate screen mode (`?1049h` to enter, `?1049l` to exit)

2. **Basic control characters**
   - Backspace (0x08) and DEL (0x7F)
   - Newline and carriage return

3. **Buffer simulation**
   - Maintains cursor position within buffer
   - Inserts characters at cursor position (not just appending)
   - Deletes characters on backspace
   - Ignores all content when in alternate screen mode

### Alternate Screen Mode

Many terminal programs (vim, less, top, etc.) use alternate screen mode to:
- Save current screen content
- Display their own interface
- Restore original content on exit

script2json detects these sequences (`ESC[?1049h` and `ESC[?1049l`) and:
- Sets `inAlternateScreen` flag when entering
- Ignores all bytes except ESC while in alternate screen
- Clears flag when exiting alternate screen

This prevents tool UIs from polluting command output.

### FIFO Mechanics

script2json uses **two FIFOs**:

1. **script FIFO**: Raw terminal bytes from `script -f`
   - Single reader (scriptFifoReader)
   - Continuous stream

2. **command FIFO**: Command strings from shell
   - commandFifoReader must reopen after each writer close
   - Written by shell's PROMPT_COMMAND
   - Newline-delimited strings

FIFOs are created automatically if they don't exist (mode 0666).

### Race Condition Handling

The synchronization model relies on precise signal timing:
1. Shell's DEBUG trap sends SIGUSR1 (start reading)
2. User command executes, output goes to script FIFO
3. Shell's PROMPT_COMMAND writes command to command FIFO
4. Shell's PROMPT_COMMAND sends SIGUSR2 (stop reading, send EOF)

If signals arrive out of order or are delayed (heavy system load), commands/outputs may be:
- Missed entirely
- Corrupted (partial output)
- Desynchronized (wrong command paired with output)

## Usage Workflow

### 1. Build and Install
```bash
go build -o script2json .
go install
```

### 2. Start script2json
```bash
script2json -log-level error \
  -script-fifo /tmp/script.fifo \
  -command-fifo /tmp/command.fifo \
  > /tmp/json.fifo
```

### 3. Start script session
```bash
script -f /tmp/script.fifo
```

### 4. Configure shell with traps
```bash
PROMPT_COMMAND='echo "$(fc -ln -1 2>/dev/null | sed "s/^[[:space:]]*//")" > /tmp/command.fifo 2>/dev/null; pkill -USR2 script2json 2>/dev/null; '

trap '[[ ! "$BASH_COMMAND" =~ pkill\ -USR[1-2]+\ script2json ]] && { pkill -USR1 script2json 2>/dev/null; }' DEBUG
```

### 5. Use shell normally
```bash
$ echo hello
hello
```

### 6. Observe JSON output
```json
{"id":"1","command":"echo hello","output":"hello\r\n","return_timestamp":"2025-09-29T13:24:41.027649619-04:00"}
```

## Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--script-fifo` | `/tmp/script.fifo` | Path to script FIFO (input) |
| `--command-fifo` | `/tmp/command.fifo` | Path to command FIFO (input) |
| `--log-level` | `info` | Log level: debug, info, warn, error |
| `--pid-file` | (none) | Path to write process ID (optional) |

## Signals Reference

| Signal | Purpose | Effect |
|--------|---------|--------|
| `SIGUSR1` | Start reading | Sets `reading` flag to true |
| `SIGUSR2` | Stop reading & flush | Sets `reading` to false, sends EOF |
| `SIGHUP` | Reset state | Clears lineEditor buffers and flags |
| `SIGINT` | Graceful shutdown | Cleanup and exit |
| `SIGTERM` | Graceful shutdown | Cleanup and exit |

## File Structure

```
script2json/
├── main.go                      # Entire application (single file)
├── main_test.go                 # Comprehensive test suite (72.8% coverage)
├── go.mod                       # Go module definition
├── README.md                    # User documentation
├── CLAUDE.md                    # This file - project overview for Claude
├── LICENSE                      # Apache 2.0 license
└── examples/
    ├── sample_input.typescript  # Raw script output example
    └── sample_output.jsonl      # Cleaned JSON output example
```

## State Management

### Atomic Variables

- **`reading` (atomic.Bool)**: Controls whether scriptFifoReader sends bytes
  - Set true by SIGUSR1
  - Set false by SIGUSR2
  - Prevents output from appearing in wrong command records

- **`recordID` (atomic.Uint64)**: Monotonic counter for CommandRecord IDs
  - Incremented for each record
  - Provides ordering guarantee

### Per-Goroutine State

- **lineEditor**: `buffer`, `cursor`, `inCSI`, `inAlternateScreen`, `csiBuffer`
- **commandFifoReader**: `commandBuffer` (accumulates bytes until newline)

## Error Handling

- **FIFO Creation**: Fails fast if FIFO can't be created/stat'd
- **FIFO Reading**: Logs errors but continues (allows recovery)
- **JSON Marshaling**: Logs error and skips record
- **Signal Handling**: Always succeeds (signals are best-effort)
- **PID File**: Warns on cleanup failure but doesn't error

## Integration with Shaidow

Shaidow's `start.sh` script:
1. Creates FIFOs in a temporary directory
2. Launches script2json with appropriate FIFO paths
3. Starts `script -f` in a tmux pane
4. Configures shell with DEBUG trap and PROMPT_COMMAND
5. Pipes script2json output to `shaidow.py`

The integration chain:
```
User Shell → script → script FIFO → script2json → JSON FIFO → shaidow.py → LLM
              ↑                       ↑
              │                       │
         (captures)            (signals via SIGUSR1/2)
```

## Debugging Tips

### Enable Debug Logging
```bash
script2json -log-level debug -script-fifo /tmp/script.fifo -command-fifo /tmp/command.fifo
```

Debug logs include:
- FIFO open/close events
- Signal reception
- Buffer state every 5 seconds
- Command/output matching

### Common Issues

1. **No output**: Check that SIGUSR1/SIGUSR2 are being sent
   ```bash
   ps aux | grep script2json  # Get PID
   kill -USR1 <pid>           # Manually start reading
   kill -USR2 <pid>           # Manually stop reading
   ```

2. **Corrupted output or desync**: System may be under heavy load, signals arriving late
   - **Quick fix**: Send SIGHUP to reset state
     ```bash
     pkill -HUP script2json
     # Or: kill -HUP $(cat /path/to/pid.file)
     ```
   - Consider `renice`ing script2json process
   - Reduce concurrent shell activity
   - Monitor logs at debug level to see buffer state

3. **Missing commands**: commandFifoReader may not be receiving data
   - Check PROMPT_COMMAND is writing to correct FIFO
   - Verify FIFO permissions

4. **Alternate screen content leaking**: Check CSI handling logic in `handleCSI()`
   - Can also try SIGHUP reset to clear stuck `inAlternateScreen` flag

5. **Stuck in reading state**: Buffer not flushing
   - Send SIGHUP to reset
   - Verify SIGUSR2 is being sent by shell's PROMPT_COMMAND

## Performance Characteristics

- **Memory**: Minimal (one buffer per goroutine, small channels)
- **CPU**: Low (mostly I/O bound, byte-by-byte processing)
- **Latency**: Sub-millisecond for typical commands
- **Throughput**: Limited by terminal output rate, not processor

## Desync Recovery Feature

### Overview
As of the latest version, script2json includes a **SIGHUP-based reset mechanism** to recover from desynchronization without full restart.

### When to Use
Send SIGHUP when you notice:
- Commands paired with wrong outputs
- Missing outputs for executed commands
- Corrupted/partial output in JSON
- Stuck in alternate screen mode
- Buffer not clearing between commands

### How It Works
1. Signal handler receives SIGHUP
2. Stops reading (sets `reading` = false)
3. Sends reset signal to lineEditor via `resetChan`
4. Flushes current buffer if reading was active (sends EOF)
5. lineEditor goroutine clears all state:
   - Buffer contents
   - Cursor position
   - CSI parsing state (`inCSI`, `csiBuffer`)
   - Alternate screen flag (`inAlternateScreen`)
6. Ready for next command with clean state

### Implementation Details
- **Non-blocking reset**: Uses buffered channel with select to prevent blocking
- **Thread-safe**: Uses mutex to protect buffer during reset
- **Preserves connections**: FIFOs remain open, no reconnection needed
- **Logged**: Reset events logged at INFO level for debugging

### Example Usage
```bash
# Basic reset
pkill -HUP script2json

# With PID file
kill -HUP $(cat /tmp/script2json.pid)

# In Shaidow integration
script2json --pid-file /tmp/script2json.pid &
# Later, if desync detected:
kill -HUP $(cat /tmp/script2json.pid)
```

## Testing

### Test Suite Overview
script2json includes a comprehensive test suite in `main_test.go` with **72.8% code coverage**.

### Test Coverage (17 test functions, 24 subtests)

1. **ANSI/CSI Sequence Handling** (`TestHandleCSI`)
   - Enter/exit alternate screen mode
   - Arrow left/right cursor movements
   - Boundary conditions (cursor at start/end)

2. **Line Editor Functionality**
   - `TestLineEditorBasicInput`: Basic character input
   - `TestLineEditorBackspace`: DEL/backspace handling
   - `TestLineEditorAlternateScreen`: Filtering alternate screen content
   - `TestLineEditorCursorMovement`: Arrow key cursor positioning
   - `TestLineEditorReset`: State reset via resetChan

3. **Record Creation**
   - `TestRecordCreator`: JSON record creation and output
   - `TestRecordCreatorReset`: Channel draining on reset
   - `TestRecordIDIncrement`: Concurrent monotonic counter

4. **FIFO Management**
   - `TestCreateScriptFifo`: Script FIFO creation
   - `TestCreateCommandFifo`: Command FIFO creation

5. **PID File Management**
   - `TestWritePidFile`: PID file creation and content
   - `TestRemovePidFile`: PID file cleanup

6. **Signal Handling**
   - `TestSignalHandlingSetup`: Setup without panic
   - `TestSignalHandlingUSR1`: SIGUSR1 starts reading
   - `TestSignalHandlingUSR2`: SIGUSR2 stops reading and sends EOF
   - `TestSignalHandlingHUP`: SIGHUP resets state

7. **End-to-End Integration** (`TestEndToEnd`)
   - Complete pipeline from FIFOs to JSON output
   - Multiple commands with proper signal timing
   - ANSI sequence stripping verification
   - Monotonic ID verification
   - Timestamp verification
   - PID file verification

### Running Tests

```bash
# Run all tests
go test

# Run with verbose output
go test -v

# Run with coverage
go test -coverprofile=coverage.out

# View coverage report
go tool cover -html=coverage.out

# Run specific test
go test -run TestEndToEnd -v
```

### Test Design Principles

- **Isolation**: Each test uses temporary directories and separate goroutines
- **Realistic**: End-to-end test simulates actual FIFO/signal workflow
- **Coverage**: Tests focus on script2json logic, not Go language features
- **Cleanup**: All tests properly clean up resources (FIFOs, temp dirs, goroutines)
- **Timing**: Tests account for goroutine scheduling with appropriate sleeps

## Potential Enhancements

1. ~~**Better error recovery**: Restart reading on desync~~ ✅ **IMPLEMENTED** (SIGHUP reset)
2. **Configurable buffer limits**: Prevent memory issues with huge outputs
3. **More ANSI sequence support**: Handle cursor position, screen clearing
4. **Metrics/telemetry**: Count processed commands, detect desyncs automatically
5. **Multiple shell support**: Beyond Bash (zsh, fish, etc.)
6. **Streaming output**: Send partial results for long-running commands
7. **Auto-reset on detection**: Automatically detect desync and trigger reset

## Security Considerations

- **FIFO permissions**: 0666 allows any user to read/write
- **No input validation**: Trusts shell to send well-formed data
- **Signal handling**: Any process can send SIGUSR1/SIGUSR2
- **PID file**: Written with 0644 (world-readable)

For production use, consider:
- Restricting FIFO permissions
- Validating command strings
- Using a more secure IPC mechanism
