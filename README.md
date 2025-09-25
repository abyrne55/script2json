name: script2json
description: "A Go application that reads from a FIFO, processes terminal control sequences, and outputs cleaned strings."
author: "AI Assistant"
version: "1.0.0"

instructions: |
  1. Build the application:
     go build -o script2json .

  2. Create the FIFO (if it doesn't exist):
     mkfifo /tmp/script.fifo

  3. Run the application in one terminal:
     ./script2json -fifo /tmp/script.fifo

  4. In another terminal, find the PID of the script2json process:
     pgrep script2json

  5. Send SIGUSR1 to start capturing data:
     kill -SIGUSR1 <PID>

  6. Write to the FIFO. You can use `script` to capture a terminal session:
     script -f /tmp/script.fifo
     # Type something, use backspace, arrow keys, etc.
     # Exit the script session with `exit`.

  7. Send SIGUSR2 to stop capturing and print the result:
     kill -SIGUSR2 <PID>

  The cleaned output will be printed to the stdout of the script2json application.
