// Package securemem provides memory-protected storage for sensitive data
// using memguard to prevent data from being read via debugger, memory dump, or swap.
//
// This package should be imported by your main package to ensure memguard is initialized.
//
// Example:
//
//	import _ "github.com/yourproject/internal/securemem"
//
//	func main() {
//	    // Your application code here
//	}
package securemem

// init initializes memguard when the package is imported.
func init() {
	Init()
}
