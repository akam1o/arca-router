package grpc

import (
	"context"

	googlegrpc "google.golang.org/grpc"
)

const (
	configServiceName  = "arca.router.v1.ConfigService"
	sessionServiceName = "arca.router.v1.SessionService"
	stateServiceName   = "arca.router.v1.StateService"
)

type configServiceServer interface {
	GetRunning(context.Context) (string, uint64, error)
	GetCandidate(context.Context, string) (string, error)
	EditCandidate(context.Context, string, string) error
	Commit(context.Context, string, string, string) (string, uint64, error)
	Discard(context.Context, string) error
	Diff(context.Context, string) (string, bool, error)
	ListHistory(context.Context, int, int) ([]CommitInfo, error)
}

type sessionServiceServer interface {
	CreateSession(context.Context, string) (string, error)
	CloseSession(context.Context, string) error
	AcquireLock(context.Context, string, string) error
	ReleaseLock(context.Context, string) error
}

type stateServiceServer interface {
	GetInterfaces(context.Context, string) ([]InterfaceInfo, error)
	GetRoutes(context.Context, string, string) ([]RouteInfo, error)
	GetBGPNeighbors(context.Context) ([]BGPNeighborInfo, error)
	GetSystemInfo(context.Context) (*SystemInfo, error)
}

func registerConfigServiceServer(s *googlegrpc.Server, srv configServiceServer) {
	s.RegisterService(&googlegrpc.ServiceDesc{
		ServiceName: configServiceName,
		HandlerType: (*configServiceServer)(nil),
		Methods: []googlegrpc.MethodDesc{
			{MethodName: "GetRunning", Handler: configGetRunningHandler},
			{MethodName: "GetCandidate", Handler: configGetCandidateHandler},
			{MethodName: "EditCandidate", Handler: configEditCandidateHandler},
			{MethodName: "Commit", Handler: configCommitHandler},
			{MethodName: "Discard", Handler: configDiscardHandler},
			{MethodName: "Diff", Handler: configDiffHandler},
			{MethodName: "ListHistory", Handler: configListHistoryHandler},
		},
	}, srv)
}

func registerSessionServiceServer(s *googlegrpc.Server, srv sessionServiceServer) {
	s.RegisterService(&googlegrpc.ServiceDesc{
		ServiceName: sessionServiceName,
		HandlerType: (*sessionServiceServer)(nil),
		Methods: []googlegrpc.MethodDesc{
			{MethodName: "CreateSession", Handler: sessionCreateHandler},
			{MethodName: "CloseSession", Handler: sessionCloseHandler},
			{MethodName: "AcquireLock", Handler: sessionAcquireLockHandler},
			{MethodName: "ReleaseLock", Handler: sessionReleaseLockHandler},
		},
	}, srv)
}

func registerStateServiceServer(s *googlegrpc.Server, srv stateServiceServer) {
	s.RegisterService(&googlegrpc.ServiceDesc{
		ServiceName: stateServiceName,
		HandlerType: (*stateServiceServer)(nil),
		Methods: []googlegrpc.MethodDesc{
			{MethodName: "GetInterfaces", Handler: stateGetInterfacesHandler},
			{MethodName: "GetRoutes", Handler: stateGetRoutesHandler},
			{MethodName: "GetBGPNeighbors", Handler: stateGetBGPNeighborsHandler},
			{MethodName: "GetSystemInfo", Handler: stateGetSystemInfoHandler},
		},
	}, srv)
}

func unaryHandler[Req any](
	srv interface{},
	ctx context.Context,
	dec func(interface{}) error,
	interceptor googlegrpc.UnaryServerInterceptor,
	fullMethod string,
	call func(context.Context, *Req) (interface{}, error),
) (interface{}, error) {
	in := new(Req)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return call(ctx, in)
	}
	info := &googlegrpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: fullMethod,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return call(ctx, req.(*Req))
	}
	return interceptor(ctx, in, info, handler)
}

func configGetRunningHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[getRunningRequest](srv, ctx, dec, interceptor, "/"+configServiceName+"/GetRunning",
		func(ctx context.Context, _ *getRunningRequest) (interface{}, error) {
			text, version, err := srv.(configServiceServer).GetRunning(ctx)
			return &getRunningResponse{ConfigText: text, Version: version}, err
		})
}

func configGetCandidateHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[getCandidateRequest](srv, ctx, dec, interceptor, "/"+configServiceName+"/GetCandidate",
		func(ctx context.Context, req *getCandidateRequest) (interface{}, error) {
			text, err := srv.(configServiceServer).GetCandidate(ctx, req.SessionID)
			return &getCandidateResponse{ConfigText: text}, err
		})
}

func configEditCandidateHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[editCandidateRequest](srv, ctx, dec, interceptor, "/"+configServiceName+"/EditCandidate",
		func(ctx context.Context, req *editCandidateRequest) (interface{}, error) {
			return &editCandidateResponse{}, srv.(configServiceServer).EditCandidate(ctx, req.SessionID, req.ConfigText)
		})
}

func configCommitHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[commitRequest](srv, ctx, dec, interceptor, "/"+configServiceName+"/Commit",
		func(ctx context.Context, req *commitRequest) (interface{}, error) {
			commitID, version, err := srv.(configServiceServer).Commit(ctx, req.SessionID, req.User, req.Message)
			return &commitResponse{CommitID: commitID, Version: version}, err
		})
}

func configDiscardHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[discardRequest](srv, ctx, dec, interceptor, "/"+configServiceName+"/Discard",
		func(ctx context.Context, req *discardRequest) (interface{}, error) {
			return &discardResponse{}, srv.(configServiceServer).Discard(ctx, req.SessionID)
		})
}

func configDiffHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[diffRequest](srv, ctx, dec, interceptor, "/"+configServiceName+"/Diff",
		func(ctx context.Context, req *diffRequest) (interface{}, error) {
			text, hasChanges, err := srv.(configServiceServer).Diff(ctx, req.SessionID)
			return &diffResponse{DiffText: text, HasChanges: hasChanges}, err
		})
}

func configListHistoryHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[listHistoryRequest](srv, ctx, dec, interceptor, "/"+configServiceName+"/ListHistory",
		func(ctx context.Context, req *listHistoryRequest) (interface{}, error) {
			entries, err := srv.(configServiceServer).ListHistory(ctx, req.Limit, req.Offset)
			return &listHistoryResponse{Entries: entries}, err
		})
}

func sessionCreateHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[createSessionRequest](srv, ctx, dec, interceptor, "/"+sessionServiceName+"/CreateSession",
		func(ctx context.Context, req *createSessionRequest) (interface{}, error) {
			sessionID, err := srv.(sessionServiceServer).CreateSession(ctx, req.User)
			return &createSessionResponse{SessionID: sessionID}, err
		})
}

func sessionCloseHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[closeSessionRequest](srv, ctx, dec, interceptor, "/"+sessionServiceName+"/CloseSession",
		func(ctx context.Context, req *closeSessionRequest) (interface{}, error) {
			return &closeSessionResponse{}, srv.(sessionServiceServer).CloseSession(ctx, req.SessionID)
		})
}

func sessionAcquireLockHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[acquireLockRequest](srv, ctx, dec, interceptor, "/"+sessionServiceName+"/AcquireLock",
		func(ctx context.Context, req *acquireLockRequest) (interface{}, error) {
			return &acquireLockResponse{}, srv.(sessionServiceServer).AcquireLock(ctx, req.SessionID, req.User)
		})
}

func sessionReleaseLockHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[releaseLockRequest](srv, ctx, dec, interceptor, "/"+sessionServiceName+"/ReleaseLock",
		func(ctx context.Context, req *releaseLockRequest) (interface{}, error) {
			return &releaseLockResponse{}, srv.(sessionServiceServer).ReleaseLock(ctx, req.SessionID)
		})
}

func stateGetInterfacesHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[getInterfacesRequest](srv, ctx, dec, interceptor, "/"+stateServiceName+"/GetInterfaces",
		func(ctx context.Context, req *getInterfacesRequest) (interface{}, error) {
			interfaces, err := srv.(stateServiceServer).GetInterfaces(ctx, req.NameFilter)
			return &getInterfacesResponse{Interfaces: interfaces}, err
		})
}

func stateGetRoutesHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[getRoutesRequest](srv, ctx, dec, interceptor, "/"+stateServiceName+"/GetRoutes",
		func(ctx context.Context, req *getRoutesRequest) (interface{}, error) {
			routes, err := srv.(stateServiceServer).GetRoutes(ctx, req.PrefixFilter, req.ProtoFilter)
			return &getRoutesResponse{Routes: routes}, err
		})
}

func stateGetBGPNeighborsHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[getBGPNeighborsRequest](srv, ctx, dec, interceptor, "/"+stateServiceName+"/GetBGPNeighbors",
		func(ctx context.Context, _ *getBGPNeighborsRequest) (interface{}, error) {
			neighbors, err := srv.(stateServiceServer).GetBGPNeighbors(ctx)
			return &getBGPNeighborsResponse{Neighbors: neighbors}, err
		})
}

func stateGetSystemInfoHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor googlegrpc.UnaryServerInterceptor) (interface{}, error) {
	return unaryHandler[getSystemInfoRequest](srv, ctx, dec, interceptor, "/"+stateServiceName+"/GetSystemInfo",
		func(ctx context.Context, _ *getSystemInfoRequest) (interface{}, error) {
			info, err := srv.(stateServiceServer).GetSystemInfo(ctx)
			return &getSystemInfoResponse{Info: info}, err
		})
}
