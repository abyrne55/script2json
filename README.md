# script2json
Generated-By: Gemini 2.5 Pro

A minimal Golang tool that parses the output from the `script` command into a clean, machine-readable JSONL format.

## Description

The `script` command is a great utility for recording terminal sessions. However, the resulting `typescript` file is a raw dump of the terminal stream, including ANSI escape codes for colors, cursor movements, and shell prompts.

This tool, `script2json`, reads that raw stream and transforms it into a structured JSONL format, where each line is a JSON object containing a unique ID, the command that was executed, and the corresponding clean text output.

It is designed to work with streams and FIFOs, allowing for real-time conversion of a live terminal session.

## Features

-   Parses `script` typescript files into JSONL.
-   Strips ANSI escape codes from command output.
-   Correctly renders commands by interpreting backspaces and other control characters.
-   Supports streaming from pipes and FIFOs for live conversion.
-   Groups multi-line output with the correct command.

## Output Format

Each line in the output is a JSON object with the following fields:

-   `id`: A unique, incrementing ID for each command.
-   `command`: The command that was executed, rendered as a clean string.
-   `output`: The complete, clean text output of the command.

### Example

```json
{"id":"1","command":"echo hello world","output":"hello world"}
{"id":"2","command":"ls -l","output":"total 8\n-rw-r--r-- 1 user user 1234 Sep 11 16:20 main.go"}
```

## Build Instructions

To build the tool, you need to have Go installed.

1.  Clone the repository or download the source code.
2.  Fetch the single dependency:
    ```bash
    go get github.com/acarl005/stripansi
    ```
3.  Build the executable:
    ```bash
    go build
    ```

## Usage

The tool reads from standard input and writes to standard output, making it easy to use with pipes.

### Converting a Saved File

```bash
./script2json < typescript.log > output.jsonl
```

### Live Conversion with a FIFO

This is the most powerful feature. You can "watch" a terminal session and get structured JSONL in real-time.

1.  In one terminal, create a FIFO and start `script`:
    ```bash
    mkfifo /tmp/mysession
    script -f /tmp/mysession
    ```
    Now, anything you type in this terminal will be written to the FIFO.

2.  In a second terminal, run `script2json` to process the stream from the FIFO:
    ```bash
    ./script2json < /tmp/mysession
    ```
    As you execute commands in the first terminal, you will see JSONL records appear in the second terminal immediately, with no lag.
