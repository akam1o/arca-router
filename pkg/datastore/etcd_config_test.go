package datastore

import (
	"errors"
	"testing"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestEtcdGetResponseRevisionUsesHeaderRevision(t *testing.T) {
	resp := &clientv3.GetResponse{
		Header: &etcdserverpb.ResponseHeader{Revision: 42},
		Kvs: []*mvccpb.KeyValue{
			{ModRevision: 7},
		},
	}

	if got := etcdGetResponseRevision(resp); got != 42 {
		t.Fatalf("etcdGetResponseRevision() = %d, want header revision 42", got)
	}
}

func TestRunningConfigTextFromEtcdResponse(t *testing.T) {
	resp := &clientv3.GetResponse{
		Kvs: []*mvccpb.KeyValue{
			{Value: []byte("set system host-name edge01\n")},
		},
	}

	got, err := runningConfigTextFromEtcdResponse(resp)
	if err != nil {
		t.Fatalf("runningConfigTextFromEtcdResponse() error = %v", err)
	}
	if got != "set system host-name edge01\n" {
		t.Fatalf("runningConfigTextFromEtcdResponse() = %q", got)
	}
}

func TestRunningConfigTextFromEtcdResponseRequiresConfigKey(t *testing.T) {
	_, err := runningConfigTextFromEtcdResponse(&clientv3.GetResponse{})
	if err == nil {
		t.Fatal("runningConfigTextFromEtcdResponse() error = nil, want inconsistent state error")
	}
	var dsErr *Error
	if !errors.As(err, &dsErr) || dsErr.Code != ErrCodeInternal {
		t.Fatalf("runningConfigTextFromEtcdResponse() error = %v, want ErrCodeInternal", err)
	}
}
