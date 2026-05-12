package frr

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

const mgmtdSmokeEnv = "ARCA_FRR_MGMTD_SMOKE"

func TestFRRMgmtdSmokeApplyAndCleanup(t *testing.T) {
	if os.Getenv(mgmtdSmokeEnv) != "1" {
		t.Skipf("set %s=1 to run the live FRR mgmtd smoke test", mgmtdSmokeEnv)
	}
	if _, err := exec.LookPath("vtysh"); err != nil {
		t.Fatalf("vtysh not found: %v", err)
	}

	client := NewVtyshMgmtClient()
	name := "ARCA_SMOKE_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	base := "/frr-route-map:lib/route-map" + keyPred("name", name)
	entry := base + "/entry" + keyPred("sequence", "10")
	cleanupOps := []MgmtOperation{deleteOp(base)}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = client.Apply(ctx, cleanupOps)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	applyOps := []MgmtOperation{
		setOp(base+"/name", name),
		setOp(entry+"/sequence", "10"),
		setOp(entry+"/action", "permit"),
	}
	if err := client.Apply(ctx, applyOps); err != nil {
		t.Fatalf("apply smoke route-map through mgmtd: %v", err)
	}
	if err := client.Apply(ctx, cleanupOps); err != nil {
		t.Fatalf("cleanup smoke route-map through mgmtd: %v", err)
	}
}
