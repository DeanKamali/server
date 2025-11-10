package cache

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// GetSystemMemory returns the total system memory in bytes
// On Linux, reads from /proc/meminfo
// On other systems, uses a reasonable default
func GetSystemMemory() int64 {
	// Try to read from /proc/meminfo on Linux
	if memTotal, err := readMemInfo(); err == nil {
		return memTotal
	}
	
	// Fallback: Use Go's runtime memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Use a conservative estimate (8GB default)
	// Go's m.Sys is memory allocated by Go, not total system memory
	defaultRAM := int64(8 * 1024 * 1024 * 1024) // 8GB
	
	// If Go has allocated significant memory, use that as a hint
	if m.Sys > 1024*1024*1024 { // More than 1GB
		// Estimate system RAM as 4x what Go has allocated (conservative)
		estimatedRAM := int64(m.Sys) * 4
		if estimatedRAM < defaultRAM {
			return defaultRAM
		}
		return estimatedRAM
	}
	
	return defaultRAM
}

// readMemInfo reads total memory from /proc/meminfo (Linux)
func readMemInfo() (int64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			// Format: "MemTotal:       16384000 kB"
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				value, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					return 0, err
				}
				// Convert from KB to bytes
				return value * 1024, nil
			}
		}
	}
	
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

