# Shell Function Example

The `go_sandbox` tool now includes a built-in `Shell` function for executing shell commands in the WebAssembly sandbox environment.

## Function Signature

```go
func Shell(command string) (stdout string, stderr string, exitCode int)
```

## Parameters

- `command` - Shell command to execute (passed to `sh -c`)

## Returns

- `stdout` - Standard output from the command
- `stderr` - Standard error from the command
- `exitCode` - Exit code (0 for success, non-zero for failure)

## Examples

### Simple Command

```go
package main

import "fmt"

func main() {
	out, err, code := Shell("echo 'Hello World'")
	fmt.Printf("Output: %s\n", out)
	fmt.Printf("Exit code: %d\n", code)
}
```

### List Files

```go
package main

import "fmt"

func main() {
	out, _, code := Shell("ls -la")
	if code == 0 {
		fmt.Printf("Files:\n%s\n", out)
	} else {
		fmt.Printf("Error: command failed with code %d\n", code)
	}
}
```

### Check Exit Code

```go
package main

import "fmt"

func main() {
	_, _, code := Shell("test -f /etc/passwd")
	if code == 0 {
		fmt.Println("/etc/passwd exists")
	} else {
		fmt.Println("/etc/passwd does not exist")
	}
}
```

### Handle Stderr

```go
package main

import "fmt"

func main() {
	out, errOut, code := Shell("ls /nonexistent")

	if code != 0 {
		fmt.Printf("Command failed with code %d\n", code)
		fmt.Printf("Error: %s\n", errOut)
	} else {
		fmt.Printf("Output: %s\n", out)
	}
}
```

### Pipe Commands

```go
package main

import "fmt"

func main() {
	out, _, _ := Shell("echo 'apple\nbanana\ncherry' | grep 'banana'")
	fmt.Printf("Found: %s\n", out)
}
```

### Complex Shell Scripts

```go
package main

import "fmt"

func main() {
	script := `
		count=0
		for i in 1 2 3 4 5; do
			count=$((count + i))
		done
		echo "Sum: $count"
	`

	out, _, code := Shell(script)
	fmt.Printf("Result: %s (exit code: %d)\n", out, code)
}
```

### Working with Environment

```go
package main

import "fmt"

func main() {
	// Get current directory
	pwd, _, _ := Shell("pwd")
	fmt.Printf("Working directory: %s\n", pwd)

	// Get environment variable
	home, _, _ := Shell("echo $HOME")
	fmt.Printf("Home directory: %s\n", home)
}
```

### Combining Fetch and Shell

```go
package main

import "fmt"

func main() {
	// Fetch some data
	data, status := Fetch("GET", "https://api.github.com/users/octocat", "")

	if status == 200 {
		// Save to file using shell
		Shell("echo '" + data + "' > /tmp/github_data.json")

		// Verify file was created
		out, _, code := Shell("wc -l < /tmp/github_data.json")
		if code == 0 {
			fmt.Printf("Saved GitHub data (%s lines)\n", out)
		}
	}
}
```

## Authorization

All shell commands are subject to command authorization. When you use `Shell`, the system will:

1. Analyze the command for safety
2. Check if the command pattern is authorized
3. Request user authorization if needed
4. Allow or deny the execution

Safe commands (like `ls`, `echo`, `pwd`, etc.) are typically auto-approved. Potentially dangerous commands (like `rm`, `curl`, `git push`, etc.) require user authorization.

## Limitations

- Maximum stdout size: 1MB
- Maximum stderr size: 1MB
- Command timeout: 30 seconds
- Commands run with `/bin/sh -c`
- No interactive commands supported

## Security Notes

- Commands are executed in the host environment, not inside WASM
- Full filesystem access available (subject to permissions)
- Network access available (can make external connections)
- Use caution with user input in commands (potential command injection)

## Note

The `Shell` function is automatically available in your code - no imports needed!
