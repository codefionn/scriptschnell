package actor

import (
	"fmt"
	"time"
)

// DebugHealthScenario shows what's happening in the test
func DebugHealthScenario() {
	fmt.Println("=== Testing Health Scenario ===")

	// Create a health checkable
	health := NewHealthCheckable("test", make(chan Message, 10), nil)

	fmt.Println("1. Initial state:")
	report := health.GenerateHealthReport()
	fmt.Printf("   Status: %s\n", report.Status)
	fmt.Printf("   IsHealthy: %v\n", health.IsHealthy())
	fmt.Printf("   Error Count: %d\n", report.Metrics.ErrorCount)

	fmt.Println("\n2. After recording error:")
	health.RecordError(fmt.Errorf("test error"))
	report = health.GenerateHealthReport()
	fmt.Printf("   Status: %s\n", report.Status)
	fmt.Printf("   IsHealthy: %v\n", health.IsHealthy())
	fmt.Printf("   Error Count: %d\n", report.Metrics.ErrorCount)
	fmt.Printf("   Time Since Last Error: %v\n", time.Since(report.Metrics.LastError))

	fmt.Println("\n3. After recording activity (simulating Send()):")
	health.RecordActivity()
	report = health.GenerateHealthReport()
	fmt.Printf("   Status: %s\n", report.Status)
	fmt.Printf("   IsHealthy: %v\n", health.IsHealthy())
	fmt.Printf("   Error Count: %d\n", report.Metrics.ErrorCount)
	fmt.Printf("   Time Since Last Error: %v\n", time.Since(report.Metrics.LastError))

	fmt.Println("\n4. Raw Health Check:")
	fmt.Printf("   Mailbox Usage: %.1f%%\n", report.Metrics.MailboxUsage)
	fmt.Printf("   Recent Error Check: %v (error count %d, last error %v)\n",
		!report.Metrics.LastError.IsZero() && time.Since(report.Metrics.LastError) < 5*time.Minute && report.Metrics.ErrorCount > 0,
		report.Metrics.ErrorCount, report.Metrics.LastError)
	fmt.Printf("   Activity Check: %v (uptime %v, since last activity %v)\n",
		report.Metrics.Uptime > 30*time.Minute && time.Since(report.Metrics.LastActivityTime) > time.Hour,
		report.Metrics.Uptime, time.Since(report.Metrics.LastActivityTime))
}
