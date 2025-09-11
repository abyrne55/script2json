# script2json

A minimal Golang tool that parses output files from the "script" command into JSONL format.

## Features

- Converts script command output to JSONL format with fields: "id", "command", and "output"
- Removes ANSI escape sequences and control characters
- Properly handles command line editing (backspaces, erasures)
- Groups multi-line output together for each command
- Supports streaming input and piped commands
- Handles commands with no output
- Filters out shell prompts and ignores "exit" commands

## Building

```bash
go build -o script2json main.go
```

## Usage

### Basic usage with existing script file
```bash
./script2json < sample_input.typescript
```

### Streaming usage with tail -f
```bash
tail -f sample_input.typescript | ./script2json > sample_output.jsonl
```

### Real-time streaming with named pipe (FIFO)
```bash
# Terminal 1: Create pipe and start script with flush-on-write
mkfifo /tmp/session.pipe
script -f /tmp/session.pipe

# Terminal 2: Process the stream  
./script2json < /tmp/session.pipe
```

**Understanding the `-f` flag**: The `-f` (flush) option makes `script` flush output "after each write", but this still means commands typically appear after their output is complete and the next shell prompt appears. This is by design for proper command/output grouping.

**Force immediate flush** (if needed): You can send `SIGUSR1` to the script process to force immediate flushing:
```bash
# Find the script process ID and send signal
pkill -USR1 script
```

**Note**: Some delay between command execution and JSON output is normal and expected behavior, not a bug. This ensures complete command/output pairs rather than partial data.

### Live script session to JSONL
```bash
script typescript_file.out
# ... run your commands ...
# exit
cat typescript_file.out | ./script2json
```

## Output Format

Each line in the output is a JSON object with:
- `id`: Sequential command identifier (string)
- `command`: The command as it was typed (cleaned of control characters)
- `output`: All output lines from the command joined with newlines

Example output:
```json
{"id":"1","command":"echo hello","output":"hello"}
{"id":"2","command":"ls -la","output":"total 16\ndrwxr-xr-x 3 user user 4096 Sep 11 14:00 .\ndrwxr-xr-x 5 user user 4096 Sep 11 13:59 .."}
{"id":"3","command":"true","output":""}
```

## Notes

- The tool ignores commands that "take over the TTY" like `man` (though it will still parse their output if they exit quickly)
- Commands like `exit` are filtered out
- ANSI color codes and terminal control sequences are stripped from both commands and output
- The tool works with streaming input, processing commands as they complete
