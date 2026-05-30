package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/peer"
)

func TestGRPCRequestUserUsesUnixPeerCredential(t *testing.T) {
	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: peerCredentialAddr{
			identity: peerIdentity{
				UID:      501,
				GID:      20,
				PID:      1234,
				Username: "local-admin",
			},
		},
	})

	if got := grpcRequestUser(ctx, "mallory"); got != "local-admin" {
		t.Fatalf("grpcRequestUser() = %q, want local-admin", got)
	}
}

func TestGRPCRequestUserFallsBackToTrimmedRequestUser(t *testing.T) {
	if got := grpcRequestUser(context.Background(), " alice "); got != "alice" {
		t.Fatalf("grpcRequestUser() = %q, want alice", got)
	}
}
