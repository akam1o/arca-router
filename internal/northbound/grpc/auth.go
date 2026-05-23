package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	internalauth "github.com/akam1o/arca-router/internal/auth"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	grpcRolePairSeparator = "="
)

var grpcMethodOperations = map[string]string{
	"/arca.router.v1.ConfigService/GetRunning":        "get-config",
	"/arca.router.v1.ConfigService/GetCandidate":      "get-config",
	"/arca.router.v1.ConfigService/EditCandidate":     "edit-config",
	"/arca.router.v1.ConfigService/ReplaceCandidate":  "edit-config",
	"/arca.router.v1.ConfigService/Commit":            "commit",
	"/arca.router.v1.ConfigService/ValidateCandidate": "validate",
	"/arca.router.v1.ConfigService/Discard":           "discard-changes",
	"/arca.router.v1.ConfigService/Rollback":          "commit",
	"/arca.router.v1.ConfigService/Diff":              "get-config",
	"/arca.router.v1.ConfigService/ListHistory":       "get-config",

	"/arca.router.v1.SessionService/CreateSession": "get",
	"/arca.router.v1.SessionService/CloseSession":  "close-session",
	"/arca.router.v1.SessionService/AcquireLock":   "lock",
	"/arca.router.v1.SessionService/ReleaseLock":   "unlock",

	"/arca.router.v1.StateService/GetInterfaces":           "get",
	"/arca.router.v1.StateService/GetRoutes":               "get",
	"/arca.router.v1.StateService/GetBGPNeighbors":         "get",
	"/arca.router.v1.StateService/GetOSPFNeighbors":        "get",
	"/arca.router.v1.StateService/GetRouteText":            "get",
	"/arca.router.v1.StateService/GetBGPSummaryText":       "get",
	"/arca.router.v1.StateService/GetBGPNeighborText":      "get",
	"/arca.router.v1.StateService/GetOSPFNeighborsText":    "get",
	"/arca.router.v1.StateService/GetVRRPText":             "get",
	"/arca.router.v1.StateService/GetBFDText":              "get",
	"/arca.router.v1.StateService/GetBFDStatus":            "get",
	"/arca.router.v1.StateService/GetLCPReconciliation":    "get",
	"/arca.router.v1.StateService/GetHAStatus":             "get",
	"/arca.router.v1.StateService/GetRoutingInstances":     "get",
	"/arca.router.v1.StateService/GetClassOfService":       "get",
	"/arca.router.v1.StateService/GetSystemInfo":           "get",
	"/arca.router.v1.TelemetryService/GetTelemetryCatalog": "get",
	"/arca.router.v1.TelemetryService/SubscribeTelemetry":  "get",
}

// ParseTLSClientRoles parses identity=role pairs used by the daemon's
// --grpc-client-role flag.
func ParseTLSClientRoles(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	roles := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		pair := strings.TrimSpace(part)
		if pair == "" {
			return nil, fmt.Errorf("invalid gRPC client role mapping: empty mapping")
		}
		identity, role, ok := strings.Cut(pair, grpcRolePairSeparator)
		if !ok {
			return nil, fmt.Errorf("invalid gRPC client role mapping %q: expected identity=role", pair)
		}
		identity = strings.TrimSpace(identity)
		role = strings.TrimSpace(role)
		if identity == "" {
			return nil, fmt.Errorf("invalid gRPC client role mapping %q: identity is required", pair)
		}
		if !isValidGRPCRole(role) {
			return nil, fmt.Errorf("invalid gRPC client role mapping %q: invalid role %q", identity, role)
		}
		if _, exists := roles[identity]; exists {
			return nil, fmt.Errorf("duplicate gRPC client role mapping for identity %q", identity)
		}
		roles[identity] = role
	}
	return roles, nil
}

func isValidGRPCRole(role string) bool {
	switch role {
	case internalauth.RoleReadOnly, internalauth.RoleOperator, internalauth.RoleAdmin:
		return true
	default:
		return false
	}
}

// NewTLSClientRoleUnaryInterceptor enforces method-level RBAC for TLS-authenticated
// gRPC clients.
func NewTLSClientRoleUnaryInterceptor(roles map[string]string) googlegrpc.UnaryServerInterceptor {
	authorizer := internalauth.NewAuthorizer()
	return func(ctx context.Context, req any, info *googlegrpc.UnaryServerInfo, handler googlegrpc.UnaryHandler) (any, error) {
		if err := authorizeGRPCMethod(ctx, info.FullMethod, roles, authorizer); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// NewTLSClientRoleStreamInterceptor enforces method-level RBAC for streaming
// gRPC methods.
func NewTLSClientRoleStreamInterceptor(roles map[string]string) googlegrpc.StreamServerInterceptor {
	authorizer := internalauth.NewAuthorizer()
	return func(srv any, stream googlegrpc.ServerStream, info *googlegrpc.StreamServerInfo, handler googlegrpc.StreamHandler) error {
		if err := authorizeGRPCMethod(stream.Context(), info.FullMethod, roles, authorizer); err != nil {
			return err
		}
		return handler(srv, stream)
	}
}

func authorizeGRPCMethod(ctx context.Context, method string, roles map[string]string, authorizer *internalauth.Authorizer) error {
	if len(roles) == 0 {
		return status.Error(codes.Unauthenticated, "gRPC client certificate role mapping is not configured")
	}
	operation, ok := grpcMethodOperations[method]
	if !ok {
		return status.Errorf(codes.PermissionDenied, "gRPC method %s is not authorized", method)
	}
	role, ok := grpcTLSClientRole(ctx, roles)
	if !ok {
		return status.Error(codes.Unauthenticated, "gRPC client certificate identity is not mapped to a role")
	}
	if !authorizer.IsPermitted(role, operation) {
		return status.Errorf(codes.PermissionDenied, "gRPC role %s is not permitted to perform %s", role, operation)
	}
	return nil
}

func grpcTLSClientRole(ctx context.Context, roles map[string]string) (string, bool) {
	state, ok := grpcTLSConnectionState(ctx)
	if !ok {
		return "", false
	}
	for _, identity := range grpcTLSIdentities(state) {
		if role, ok := roles[identity]; ok {
			return role, true
		}
	}
	return "", false
}

func grpcTLSConnectionState(ctx context.Context) (tls.ConnectionState, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return tls.ConnectionState{}, false
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return tls.ConnectionState{}, false
	}
	return tlsInfo.State, true
}
