package main

import (
	"context"
	"fmt"
)

func cmdVersion(ctx context.Context, f *flags) int {
	fmt.Printf("arca-router CLI\n")
	fmt.Printf("  Version:    %s\n", Version)
	fmt.Printf("  Commit:     %s\n", Commit)
	fmt.Printf("  Build Date: %s\n", BuildDate)
	fmt.Printf("\n")

	// TODO: Add VPP version (Phase 2 - Task 5.6)
	fmt.Printf("VPP:  (not yet implemented)\n")

	// TODO: Add FRR version (Phase 2 - Task 5.6)
	fmt.Printf("FRR:  (not yet implemented)\n")

	return ExitSuccess
}
