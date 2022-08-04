package server

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CrowdStrike/perseus/internal/store"
	"github.com/CrowdStrike/perseus/perseusapi"
)

var _ perseusapi.PerseusServiceServer = (*grpcServer)(nil)

type grpcServer struct {
	perseusapi.UnimplementedPerseusServiceServer

	store store.Store
}

// NewGRPCServer constructs and returns a new gRPC server instance
func NewGRPCServer(store store.Store) perseusapi.PerseusServiceServer {
	s := grpcServer{
		store: store,
	}
	return &s
}

func (s *grpcServer) CreateModule(ctx context.Context, req *perseusapi.CreateModuleRequest) (*perseusapi.CreateModuleResponse, error) {
	log.Printf("CreateModule() called\ncreating new module %q with version(s) %v\n", req.GetModule().GetName(), req.GetModule().GetVersions())

	m := req.GetModule()
	if m.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "module name is required")
	}

	moduleID, err := s.store.SaveModule(ctx, m.GetName(), "")
	if err != nil {
		return nil, status.Errorf(codes.Internal, fmt.Sprintf("unable to save module %q: %v", m.GetName(), err))
	}
	if len(m.Versions) > 0 {
		if err = s.store.SaveModuleVersions(ctx, moduleID, m.Versions...); err != nil {
			return nil, status.Errorf(codes.Internal, fmt.Sprintf("unable to save module %q: %v", m.GetName(), err))
		}
	}

	resp := perseusapi.CreateModuleResponse{
		Module: req.GetModule(),
	}
	return &resp, nil
}

func (s *grpcServer) ListModules(ctx context.Context, req *perseusapi.ListModulesRequest) (*perseusapi.ListModulesResponse, error) {
	log.Println("ListModules() called")
	log.Println("args:", req.String())

	mods, pageToken, err := s.store.QueryModules(ctx, req.Filter, req.PageToken, int(req.PageSize))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to query the database: %v", err)
	}
	resp := perseusapi.ListModulesResponse{
		NextPageToken: pageToken,
	}
	for _, m := range mods {
		mod := &perseusapi.Module{
			Name: m.Name,
		}
		resp.Modules = append(resp.Modules, mod)
	}
	return &resp, nil
}

func (s *grpcServer) ListModuleVersions(ctx context.Context, req *perseusapi.ListModuleVersionsRequest) (*perseusapi.ListModuleVersionsResponse, error) {
	log.Println("ListModuleVersions() called")

	mod, vopt, pageToken := req.GetModuleName(), req.GetVersionOption(), req.GetPageToken()
	if mod == "" {
		return nil, status.Errorf(codes.InvalidArgument, "The module name must be specified")
	}
	switch vopt {
	case perseusapi.ModuleVersionOption_none:
		return nil, status.Errorf(codes.InvalidArgument, "The version option cannot be 'none'")
	case perseusapi.ModuleVersionOption_latest:
		if pageToken != "" {
			return nil, status.Errorf(codes.InvalidArgument, "Paging is only support when the version option is 'all'")
		}
	default:
		// all good
	}

	var (
		vers []store.Version
		err  error
	)
	vers, pageToken, err = s.store.QueryModuleVersions(ctx, req.GetModuleName(), req.GetPageToken(), int(req.GetPageSize()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to retrieve version list for module %s: %v", req.GetModuleName(), err)
	}

	resp := perseusapi.ListModuleVersionsResponse{
		ModuleName:    req.GetModuleName(),
		NextPageToken: pageToken,
	}
	for _, v := range vers {
		resp.Versions = append(resp.Versions, v.SemVer)
		if req.GetVersionOption() == perseusapi.ModuleVersionOption_latest {
			break
		}
	}

	return &resp, nil
}

func (s *grpcServer) QueryDependencies(ctx context.Context, req *perseusapi.QueryDependenciesRequest) (*perseusapi.QueryDependenciesResponse, error) {
	log.Println("QueryDependencies() called")
	log.Printf("request: %s\n", req.String())

	var (
		deps      []store.Version
		pageToken string
		err       error
	)
	switch req.GetDirection() {
	case perseusapi.DependencyDirection_dependencies:
		deps, pageToken, err = s.store.GetDependees(ctx, req.GetModuleName(), req.GetVersion(), req.GetPageToken(), int(req.GetPageSize()))
	case perseusapi.DependencyDirection_dependents:
		deps, pageToken, err = s.store.GetDependents(ctx, req.GetModuleName(), req.GetVersion(), req.GetPageToken(), int(req.GetPageSize()))
	}
	if err != nil {
		log.Println("query error:", err)
		return nil, status.Errorf(codes.Internal, "Unable to query the graph: %v", err)
	}
	resp := perseusapi.QueryDependenciesResponse{
		NextPageToken: pageToken,
	}
	for _, d := range deps {
		resp.Modules = append(resp.Modules, &perseusapi.Module{
			Name:     d.ModuleID,
			Versions: []string{"v" + d.SemVer},
		})
	}
	return &resp, nil
}
