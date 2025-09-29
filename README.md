
# script2json
Generated-By: Gemini 2.5 Pro

A Go application that reads from a FIFO, processes terminal control sequences, and outputs cleaned strings in a JSON format.

 ## Usage

  1. Build and install the application
```bash
go build -o script2json .
go install
```

  2. Run the application, specifying some pre-created FIFOs (or allowing it to create FIFOs for you) and piping the output somewhere useful
```bash
script2json -log-level error -script-fifo /tmp/script.fifo -command-fifo /tmp/command.fifo > /tmp/json.fifo
```

  3. Start a `script` session that writes to the script FIFO in real time
```bash
script -f /tmp/script.fifo
```
  4. Configure the resulting recorded shell with a `DEBUG` trap that sends SIGUSR1 to the script2json process just before the command executes and a `PROMPT_COMMAND` that writes the last command to the command FIFO and then sends SIGUSR2 to the script2json process
```bash
PROMPT_COMMAND='echo "$(fc -ln -1 2>/dev/null | sed "s/^[[:space:]]*//")" > /tmp/command.fifo 2>/dev/null; pkill -USR2 script2json 2>/dev/null; ' ; trap '[[ ! "$BASH_COMMAND" =~ pkill\ -USR[1-2]+\ script2json ]] && { pkill -USR1 script2json 2>/dev/null; }' DEBUG
```

   5. Use the terminal as you normally would, observing the JSON-formatted output flowing into the pipe you set up
```json
{"id":"9","command":"echo foo | rev","output":"oof\r\n","return_timestamp":"2025-09-29T13:24:41.027649619-04:00"}
```

Don't forget to clean up all the FIFOs once you're done
