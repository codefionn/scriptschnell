# Fetch Function Example

The `go_sandbox` tool now includes a built-in `Fetch` function for making HTTP requests in the WebAssembly sandbox environment.

## Function Signature

```go
func Fetch(method, url, body string) (responseBody string, statusCode int)
```

## Parameters

- `method` - HTTP method (GET, POST, PUT, DELETE, etc.)
- `url` - Full URL to request
- `body` - Request body (empty string for GET requests)

## Returns

- `responseBody` - The response body as a string
- `statusCode` - HTTP status code (200, 404, 500, etc.)

## Examples

### Simple GET Request

```go
package main

import "fmt"

func main() {
	response, status := Fetch("GET", "https://api.github.com/users/octocat", "")
	fmt.Printf("Status: %d\n", status)
	fmt.Printf("Response: %s\n", response)
}
```

### POST Request with JSON Body

```go
package main

import "fmt"

func main() {
	jsonBody := `{"title":"Test","body":"This is a test"}`
	response, status := Fetch("POST", "https://jsonplaceholder.typicode.com/posts", jsonBody)

	if status == 201 {
		fmt.Printf("Created successfully!\n")
		fmt.Printf("Response: %s\n", response)
	} else {
		fmt.Printf("Error: Status %d\n", status)
	}
}
```

### Checking Multiple Endpoints

```go
package main

import "fmt"

func main() {
	urls := []string{
		"https://httpbin.org/status/200",
		"https://httpbin.org/status/404",
		"https://httpbin.org/status/500",
	}

	for _, url := range urls {
		_, status := Fetch("GET", url, "")
		fmt.Printf("%s -> Status: %d\n", url, status)
	}
}
```

### Error Handling

```go
package main

import "fmt"

func main() {
	response, status := Fetch("GET", "https://api.example.com/data", "")

	switch {
	case status == 200:
		fmt.Printf("Success: %s\n", response)
	case status == 403:
		fmt.Println("Error: Domain not authorized")
	case status == 404:
		fmt.Println("Error: Not found")
	case status >= 500:
		fmt.Println("Error: Server error")
	default:
		fmt.Printf("Error: Status %d\n", status)
	}
}
```

## Authorization

All HTTP requests are subject to domain authorization. When you use `Fetch`, the system will:

1. Extract the domain from the URL
2. Check if the domain is authorized
3. Request user authorization if needed
4. Allow or deny the request

Safe domains (like github.com, googleapis.com, etc.) are typically auto-approved.

## Limitations

- Maximum response size: 1MB
- Request timeout: 30 seconds
- No support for custom headers (use standard HTTP methods)
- Response is returned as a string (you'll need to parse JSON manually)

## Note

The `Fetch` function is automatically available in your code - no imports needed!
