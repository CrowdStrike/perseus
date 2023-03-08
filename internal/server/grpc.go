package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/module"
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

var (
	reMatchModuleMajorVersion = regexp.MustCompile(`.+/v([2-9]|[1-9][0-9]+)$`)
)

// newGRPCServer constructs and returns a new gRPC server instance
func newGRPCServer(store store.Store) perseusapi.PerseusServiceServer {
	s := grpcServer{
		store: store,
	}
	return &s
}

func (s *grpcServer) CreateModule(ctx context.Context, req *perseusapi.CreateModuleRequest) (*perseusapi.CreateModuleResponse, error) {
	debugLog("CreateModule() called", "module", req.GetModule().GetName(), "versions", req.GetModule().GetVersions())

	m := req.GetModule()
	if m.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "module name is required")
	}
	// validate the module + version(s)
	// . if no versions are provided, synthesize a version based on the module name so that we can
	//   delegate to golang.org/x/mod/module.Check()
	if vers := m.GetVersions(); len(vers) > 0 {
		for _, v := range vers {
			if err := module.Check(m.GetName(), v); err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "version %q is invalid for module %q: %v", v, m.GetName(), err)
			}
		}
	} else {
		sv := "v0.0.0"
		matches := reMatchModuleMajorVersion.FindStringSubmatch(m.GetName())
		if len(matches) == 2 {
			sv = "v" + matches[1] + ".0.0"
		}
		if err := module.Check(m.GetName(), sv); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "module name %q is invalid: %v", m.GetName(), err)
		}
	}

	err := s.store.SaveModule(ctx, m.GetName(), "", m.GetVersions()...)
	if err != nil {
		debugLog("unable to save module", "module", m.GetName(), "err", err)
		return nil, status.Errorf(codes.Internal, fmt.Sprintf("unable to save module %q: a database operation failed", m.GetName()))
	}

	resp := perseusapi.CreateModuleResponse{
		Module: req.GetModule(),
	}
	return &resp, nil
}

func (s *grpcServer) ListModules(ctx context.Context, req *perseusapi.ListModulesRequest) (*perseusapi.ListModulesResponse, error) {
	debugLog("ListModules() called", "args", req.String())

	mods, pageToken, err := s.store.QueryModules(ctx, req.Filter, req.PageToken, int(req.PageSize))
	if err != nil {
		debugLog("unable to query modules", "filter", req.Filter, "err", err)
		return nil, status.Errorf(codes.Internal, "Unable to query the database")
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
	debugLog("ListModuleVersions() called", "req", req)

	mod, vfilter, vopt, pageToken := req.GetModuleName(), req.GetVersionFilter(), req.GetVersionOption(), req.GetPageToken()
	if mod == "" {
		mod = req.GetModuleFilter()
		if mod == "" {
			return nil, status.Errorf(codes.InvalidArgument, "Either the module name or a module filter pattern must be specified")
		}
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
		vers []store.ModuleVersionQueryResult
		err  error
	)
	vers, pageToken, err = s.store.QueryModuleVersions(ctx, store.ModuleVersionQuery{
		ModuleFilter:      mod,
		VersionFilter:     vfilter,
		IncludePrerelease: req.IncludePrerelease,
		LatestOnly:        req.VersionOption == perseusapi.ModuleVersionOption_latest,
		PageToken:         req.GetPageToken(),
		Count:             int(req.GetPageSize()),
	})
	if err != nil {
		debugLog("unable to query module versions", "moduleFilter", mod, "versionFilter", vfilter, "err", err)
		return nil, status.Errorf(codes.Internal, "Unable to retrieve version list for module %s: a database operation failed", req.GetModuleName())
	}

	resp := perseusapi.ListModuleVersionsResponse{
		NextPageToken: pageToken,
	}
	// external API is 1 result per module with a list of versions so group the data layer results
	// to match that structure
	var currMod *perseusapi.Module
	for _, v := range vers {
		if currMod == nil || currMod.Name != v.Module {
			currMod = &perseusapi.Module{
				Name: v.Module,
			}
			resp.Modules = append(resp.Modules, currMod)
		}
		currMod.Versions = append(currMod.Versions, "v"+v.Version)
	}

	return &resp, nil
}

func (s *grpcServer) QueryDependencies(ctx context.Context, req *perseusapi.QueryDependenciesRequest) (*perseusapi.QueryDependenciesResponse, error) {
	debugLog("QueryDependencies() called", "request", req.String())

	modName, modVer := req.GetModuleName(), req.GetVersion()
	if err := module.Check(modName, modVer); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid module/version: %v", err)
	}
	var (
		deps      []store.Version
		pageToken string
		err       error
	)
	switch req.GetDirection() {
	case perseusapi.DependencyDirection_dependencies:
		deps, pageToken, err = s.store.GetDependees(ctx, modName, strings.TrimPrefix(modVer, "v"), req.GetPageToken(), int(req.GetPageSize()))
	case perseusapi.DependencyDirection_dependents:
		deps, pageToken, err = s.store.GetDependents(ctx, modName, strings.TrimPrefix(modVer, "v"), req.GetPageToken(), int(req.GetPageSize()))
	}
	if err != nil {
		debugLog("unable to query module dependencies", "module", modName, "version", modVer, "direction", req.GetDirection().String(), "err", err)
		return nil, status.Errorf(codes.Internal, "Unable to query the graph: a database operation failed")
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

func (s *grpcServer) UpdateDependencies(ctx context.Context, req *perseusapi.UpdateDependenciesRequest) (*perseusapi.UpdateDependenciesResponse, error) {
	debugLog("UpdateDependencies() called", "args", req)

	modName, modVer := req.GetModuleName(), req.GetVersion()
	if err := module.Check(modName, modVer); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid module/version: %v", err)
	}
	mod := store.Version{
		ModuleID: modName,
		SemVer:   strings.TrimPrefix(modVer, "v"),
	}
	deps := make([]store.Version, len(req.GetDependencies()))
	for i, dep := range req.GetDependencies() {
		depName, depVers := dep.GetName(), dep.GetVersions()
		if len(depVers) != 1 {
			return nil, status.Errorf(codes.InvalidArgument, "must specify exactly 1 version of a dependency")
		} else if err := module.Check(depName, depVers[0]); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid module/version: %v", err)
		}

		deps[i] = store.Version{
			ModuleID: depName,
			SemVer:   strings.TrimPrefix(depVers[0], "v"),
		}
	}

	if err := s.store.SaveModuleDependencies(ctx, mod, deps...); err != nil {
		debugLog("unable to save module dependencies", "module", mod, "err", err)
		return nil, status.Errorf(codes.Internal, "Unable to update the graph: database operation failed")
	}

	resp := perseusapi.UpdateDependenciesResponse{}
	return &resp, nil
}
