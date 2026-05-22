package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"

	internalauth "github.com/akam1o/arca-router/internal/auth"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestParseTLSClientRoles(t *testing.T) {
	roles, err := ParseTLSClientRoles("spiffe://arca-router/nms=read-only,router-operator=operator")
	if err != nil {
		t.Fatalf("ParseTLSClientRoles() error = %v", err)
	}
	if roles["spiffe://arca-router/nms"] != internalauth.RoleReadOnly || roles["router-operator"] != internalauth.RoleOperator {
		t.Fatalf("roles = %#v, want parsed role mappings", roles)
	}
}

func TestParseTLSClientRolesRejectsInvalidRole(t *testing.T) {
	_, err := ParseTLSClientRoles("router-operator=superuser")
	if err == nil {
		t.Fatal("ParseTLSClientRoles() error = nil, want invalid role error")
	}
}

func TestParseTLSClientRolesRejectsDuplicateIdentity(t *testing.T) {
	_, err := ParseTLSClientRoles("router-operator=operator,router-operator=admin")
	if err == nil {
		t.Fatal("ParseTLSClientRoles() error = nil, want duplicate identity error")
	}
}

func TestTLSClientRoleUnaryInterceptorAllowsReadOnlyRead(t *testing.T) {
	roles := map[string]string{"monitor": internalauth.RoleReadOnly}
	interceptor := NewTLSClientRoleUnaryInterceptor(roles)
	called := false

	resp, err := interceptor(
		grpcAuthTestContext(t, grpcAuthTestCert{CommonName: "monitor"}),
		nil,
		&googlegrpc.UnaryServerInfo{FullMethod: "/arca.router.v1.ConfigService/GetRunning"},
		func(context.Context, any) (any, error) {
			called = true
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("interceptor() error = %v", err)
	}
	if !called || resp != "ok" {
		t.Fatalf("handler called = %v, resp = %#v; want called with ok", called, resp)
	}
}

func TestTLSClientRoleUnaryInterceptorRejectsReadOnlyWrite(t *testing.T) {
	roles := map[string]string{"monitor": internalauth.RoleReadOnly}
	interceptor := NewTLSClientRoleUnaryInterceptor(roles)
	called := false

	_, err := interceptor(
		grpcAuthTestContext(t, grpcAuthTestCert{CommonName: "monitor"}),
		nil,
		&googlegrpc.UnaryServerInfo{FullMethod: "/arca.router.v1.ConfigService/Commit"},
		func(context.Context, any) (any, error) {
			called = true
			return nil, nil
		},
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("interceptor() status = %v, want PermissionDenied (err=%v)", status.Code(err), err)
	}
	if called {
		t.Fatal("handler was called for denied request")
	}
}

func TestTLSClientRoleUnaryInterceptorAllowsOperatorWrite(t *testing.T) {
	roles := map[string]string{"router-operator": internalauth.RoleOperator}
	interceptor := NewTLSClientRoleUnaryInterceptor(roles)

	_, err := interceptor(
		grpcAuthTestContext(t, grpcAuthTestCert{CommonName: "router-operator"}),
		nil,
		&googlegrpc.UnaryServerInfo{FullMethod: "/arca.router.v1.ConfigService/Commit"},
		func(context.Context, any) (any, error) {
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("interceptor() error = %v", err)
	}
}

func TestTLSClientRoleUnaryInterceptorMatchesAnyCertificateIdentity(t *testing.T) {
	roles := map[string]string{"spiffe://arca-router/nms": internalauth.RoleReadOnly}
	interceptor := NewTLSClientRoleUnaryInterceptor(roles)

	_, err := interceptor(
		grpcAuthTestContext(t, grpcAuthTestCert{
			CommonName: "fallback",
			URI:        "spiffe://arca-router/nms",
		}),
		nil,
		&googlegrpc.UnaryServerInfo{FullMethod: "/arca.router.v1.StateService/GetSystemInfo"},
		func(context.Context, any) (any, error) {
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("interceptor() error = %v", err)
	}
}

func TestTLSClientRoleUnaryInterceptorRejectsUnmappedIdentity(t *testing.T) {
	roles := map[string]string{"monitor": internalauth.RoleReadOnly}
	interceptor := NewTLSClientRoleUnaryInterceptor(roles)

	_, err := interceptor(
		grpcAuthTestContext(t, grpcAuthTestCert{CommonName: "unmapped"}),
		nil,
		&googlegrpc.UnaryServerInfo{FullMethod: "/arca.router.v1.ConfigService/GetRunning"},
		func(context.Context, any) (any, error) {
			return nil, nil
		},
	)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("interceptor() status = %v, want Unauthenticated (err=%v)", status.Code(err), err)
	}
}

func TestTLSClientRoleStreamInterceptorRejectsReadOnlyWrite(t *testing.T) {
	roles := map[string]string{"monitor": internalauth.RoleReadOnly}
	interceptor := NewTLSClientRoleStreamInterceptor(roles)
	called := false

	err := interceptor(
		nil,
		grpcAuthTestServerStream{ctx: grpcAuthTestContext(t, grpcAuthTestCert{CommonName: "monitor"})},
		&googlegrpc.StreamServerInfo{FullMethod: "/arca.router.v1.SessionService/AcquireLock"},
		func(any, googlegrpc.ServerStream) error {
			called = true
			return nil
		},
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("interceptor() status = %v, want PermissionDenied (err=%v)", status.Code(err), err)
	}
	if called {
		t.Fatal("handler was called for denied stream")
	}
}

type grpcAuthTestCert struct {
	CommonName string
	URI        string
	DNSName    string
	Email      string
}

func grpcAuthTestContext(t *testing.T, cert grpcAuthTestCert) context.Context {
	t.Helper()

	x509Cert := &x509.Certificate{
		Subject:        pkix.Name{CommonName: cert.CommonName},
		DNSNames:       []string{},
		EmailAddresses: []string{},
	}
	if cert.URI != "" {
		parsed, err := url.Parse(cert.URI)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", cert.URI, err)
		}
		x509Cert.URIs = []*url.URL{parsed}
	}
	if cert.DNSName != "" {
		x509Cert.DNSNames = []string{cert.DNSName}
	}
	if cert.Email != "" {
		x509Cert.EmailAddresses = []string{cert.Email}
	}
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{x509Cert}}},
		},
	})
}

type grpcAuthTestServerStream struct {
	googlegrpc.ServerStream
	ctx context.Context
}

func (s grpcAuthTestServerStream) Context() context.Context {
	return s.ctx
}
