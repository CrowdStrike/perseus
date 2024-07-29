package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"golang.org/x/mod/module"

	"github.com/CrowdStrike/perseus/internal/store"
	"github.com/CrowdStrike/perseus/perseusapi"
	"github.com/CrowdStrike/perseus/perseusapi/perseusapiconnect"
)

var reMatchModuleMajorVersion = regexp.MustCompile(`.+/v([2-9]|[1-9][0-9]+)$`)

type connectServer struct {
	perseusapiconnect.UnimplementedPerseusServiceHandler

	store store.Store
}

func (s *connectServer) CreateModule(ctx context.Context, req *connect.Request[perseusapi.CreateModuleRequest]) (*connect.Response[perseusapi.CreateModuleResponse], error) {
	log.Debug("CreateModule() called", "module", req.Msg.GetModule().GetName(), "versions", req.Msg.GetModule().GetVersions())

	m := req.Msg.GetModule()
	if m.GetName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("module name is required"))
	}
	// validate the module + version(s)
	// . if no versions are provided, synthesize a version based on the module name so that we can
	//   delegate to golang.org/x/mod/module.Check()
	if vers := m.GetVersions(); len(vers) > 0 {
		for _, v := range vers {
			if err := module.Check(m.GetName(), v); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("version %q is invalid for module %q: %v", v, m.GetName(), err))
			}
		}
	} else {
		sv := "v0.0.0"
		matches := reMatchModuleMajorVersion.FindStringSubmatch(m.GetName())
		if len(matches) == 2 {
			sv = "v" + matches[1] + ".0.0"
		}
		if err := module.Check(m.GetName(), sv); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("module name %q is invalid: %v", m.GetName(), err))
		}
	}

	if err := s.store.SaveModule(ctx, m.GetName(), "", m.GetVersions()...); err != nil {
		log.Error(err, "error saving new module", "module", m.GetName(), "versions", m.GetVersions())
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unable to save module %q: a database operation failed", m.GetName()))
	}

	resp := connect.NewResponse(&perseusapi.CreateModuleResponse{
		Module: req.Msg.GetModule(),
	})
	return resp, nil
}

func (s *connectServer) ListModules(ctx context.Context, req *connect.Request[perseusapi.ListModulesRequest]) (*connect.Response[perseusapi.ListModulesResponse], error) {
	log.Debug("ListModules() called", "args", req.Msg.String())

	msg := req.Msg
	mods, pageToken, err := s.store.QueryModules(ctx, msg.Filter, msg.PageToken, int(msg.PageSize))
	if err != nil {
		log.Error(err, "error querying the database", "filter", msg.Filter, "pageToken", msg.PageToken, "pageSize", msg.PageSize)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Unable to query the database"))
	}
	resp := &perseusapi.ListModulesResponse{
		NextPageToken: pageToken,
	}
	for _, m := range mods {
		mod := &perseusapi.Module{
			Name: m.Name,
		}
		// include the latest version for each matched module
		versionQ := store.ModuleVersionQuery{
			ModuleFilter:      m.Name,
			LatestOnly:        true,
			IncludePrerelease: false,
		}
		vers, _, err := s.store.QueryModuleVersions(ctx, versionQ)
		if err != nil {
			log.Error(err, "unable to query for latest module version", "moduleFilter", m.Name, "latestOnly", true, "includePrerelease", false)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Unable to determine latest version for module %s: a database operation failed", m.Name))
		}
		// if no stable version exists, try to find a pre-release
		if len(vers) == 0 {
			versionQ.IncludePrerelease = true
			vers, _, err = s.store.QueryModuleVersions(ctx, versionQ)
			if err != nil {
				log.Error(err, "unable to query for latest module version", "moduleFilter", m.Name, "latestOnly", true, "includePrerelease", true)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Unable to determine latest version for module %s: a database operation failed", m.Name))
			}
		}
		// assign the latest version of the module, if found
		if len(vers) > 0 {
			mod.Versions = []string{"v" + vers[0].Version}
		}

		resp.Modules = append(resp.Modules, mod)
	}
	return connect.NewResponse(resp), nil
}

func (s *connectServer) ListModuleVersions(ctx context.Context, req *connect.Request[perseusapi.ListModuleVersionsRequest]) (*connect.Response[perseusapi.ListModuleVersionsResponse], error) {
	log.Debug("ListModuleVersions() called", "req", req.Msg)

	msg := req.Msg
	mod, vfilter, vopt, pageToken := msg.GetModuleName(), msg.GetVersionFilter(), msg.GetVersionOption(), msg.GetPageToken()
	if mod == "" {
		mod = msg.GetModuleFilter()
		if mod == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("Either the module name or a module filter pattern must be specified"))
		}
	}
	switch vopt {
	case perseusapi.ModuleVersionOption_none:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("The version option cannot be 'none'"))
	case perseusapi.ModuleVersionOption_latest:
		if pageToken != "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("Paging is only supported when the version option is 'all'"))
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
		IncludePrerelease: msg.IncludePrerelease,
		LatestOnly:        msg.VersionOption == perseusapi.ModuleVersionOption_latest,
		PageToken:         msg.GetPageToken(),
		Count:             int(msg.GetPageSize()),
	})
	if err != nil {
		kvs := []any{
			"moduleFilter", mod,
			"versionFilter", vfilter,
			"includePrerelease", msg.IncludePrerelease,
			"latestOnly", msg.VersionOption == perseusapi.ModuleVersionOption_latest,
			"pageToken", msg.GetPageToken(),
			"pageSize", msg.GetPageSize(),
		}
		log.Error(err, "unable to query module versions", kvs...)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Unable to retrieve version list for module %s: a database operation failed", msg.GetModuleName()))
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

	return connect.NewResponse(&resp), nil
}

func (s *connectServer) UpdateDependencies(ctx context.Context, req *connect.Request[perseusapi.UpdateDependenciesRequest]) (*connect.Response[perseusapi.UpdateDependenciesResponse], error) {
	msg := req.Msg

	log.Debug("UpdateDependencies() called", "args", req.Msg)

	modName, modVer := msg.GetModuleName(), msg.GetVersion()
	if err := module.Check(modName, modVer); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid module/version: %v", err))
	}
	mod := store.Version{
		ModuleID: modName,
		SemVer:   strings.TrimPrefix(modVer, "v"),
	}
	deps := make([]store.Version, len(msg.GetDependencies()))
	for i, dep := range msg.GetDependencies() {
		depName, depVers := dep.GetName(), dep.GetVersions()
		if len(depVers) != 1 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("must specify exactly 1 version of a dependency"))
		} else if err := module.Check(depName, depVers[0]); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid module/version: %v", err))
		}

		deps[i] = store.Version{
			ModuleID: depName,
			SemVer:   strings.TrimPrefix(depVers[0], "v"),
		}
	}

	if err := s.store.SaveModuleDependencies(ctx, mod, deps...); err != nil {
		log.Error(err, "unable to save module dependencies", "module", mod, "dependencies", deps)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Unable to update the graph: database operation failed"))
	}

	resp := perseusapi.UpdateDependenciesResponse{}
	return connect.NewResponse(&resp), nil
}

func (s *connectServer) QueryDependencies(ctx context.Context, req *connect.Request[perseusapi.QueryDependenciesRequest]) (*connect.Response[perseusapi.QueryDependenciesResponse], error) {
	msg := req.Msg

	log.Debug("QueryDependencies() called", "request", msg.String())

	modName, modVer := msg.GetModuleName(), msg.GetVersion()
	if err := module.Check(modName, modVer); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid module/version: %v", err))
	}
	var (
		deps      []store.Version
		pageToken string
		err       error
	)
	switch msg.GetDirection() {
	case perseusapi.DependencyDirection_dependencies:
		deps, pageToken, err = s.store.GetDependees(ctx, modName, strings.TrimPrefix(modVer, "v"), msg.GetPageToken(), int(msg.GetPageSize()))
	case perseusapi.DependencyDirection_dependents:
		deps, pageToken, err = s.store.GetDependents(ctx, modName, strings.TrimPrefix(modVer, "v"), msg.GetPageToken(), int(msg.GetPageSize()))
	}
	if err != nil {
		kvs := []any{
			"module", modName,
			"version", modVer,
			"direction", msg.GetDirection().String(),
			"pageToken", msg.GetPageToken(),
			"pageSize", msg.GetPageSize(),
		}
		log.Error(err, "unable to query module dependencies", kvs...)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Unable to query the graph: a database operation failed"))
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
	return connect.NewResponse(&resp), nil
}
