package modproxy

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// Proxy wraps a Getter and a list of proxy URLs to provide the required module proxy operations
type Proxy struct {
	g       Getter
	proxies []string
}

// New returns a Proxy instance that will use g to execute HTTP requests against the module proxies
// in urls.
func New(g Getter, urls ...string) Proxy {
	if len(urls) == 0 {
		urls = getModProxies()
	}
	return Proxy{
		g:       g,
		proxies: urls,
	}
}

// NewFromEnv returns a Proxy instance that will use g to execute HTTP requests against the module proxies
// configured in the system environment
func NewFromEnv(g Getter) Proxy {
	return New(g)
}

// GetCurrentVersion returns the highest known version of the specified module, as returned by list of
// module proxies configured on p.
func (p Proxy) GetCurrentVersion(mod string, includePrerelease bool) (string, error) {
	versions, err := p.GetModuleVersions(mod)
	if err != nil {
		return "", err
	}
	latest := versions[0]
	for _, v := range versions {
		if semver.Compare(v, latest) > 0 && (semver.Prerelease(v) == "" || includePrerelease) {
			latest = v
		}
	}
	return latest, nil
}

// GetModuleVersions retrieve a list of module versions for the specified module by querying the list
// of module proxies configured on p.
func (p Proxy) GetModuleVersions(mod string) ([]string, error) {
	for _, proxy := range p.proxies {
		url := proxy + "/" + path.Join(mod, "@v/list")
		resp, err := p.g.Get(url)
		if err != nil {
			return nil, fmt.Errorf("error fetching module versions from %s: %w", proxy, err)
		}
		defer func() {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}()
		switch resp.StatusCode {
		case http.StatusOK:
			// no error check here b/c we've already checked the error from Get() and the response status code
			data, _ := io.ReadAll(resp.Body)
			// the response is a plain text list of module versions delimited by newlines
			// - see https://go.dev/ref/mod#goproxy-protocol
			if len(data) == 0 {
				// proceed to the next proxy, if present, if we got no data
				continue
			}
			ss := strings.Split(string(data), "\n")
			res := make([]string, len(ss))
			for i, s := range ss {
				res[i] = strings.TrimSpace(s)
			}
			return res, nil
		case http.StatusNotFound, http.StatusGone:
			// try the next proxy
			continue
		default:
			return nil, fmt.Errorf("unexpected response code (%s) from %s", resp.Status, proxy)
		}
	}
	return nil, fmt.Errorf("no versions found for %s", mod)
}

// GetModFile retrieves the go.mod file for the specified module by querying the list of module proxies
// configured on p.
func (p Proxy) GetModFile(mod, version string) (*modfile.File, error) {
	for _, proxy := range p.proxies {
		u := proxy + "/" + path.Join(mod, "@v", semver.Canonical(version)+".mod")
		resp, err := p.g.Get(u)
		if err != nil {
			return nil, fmt.Errorf("error fetching module versions from %s: %w", u, err)
		}
		defer func() {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}()
		switch resp.StatusCode {
		case http.StatusOK:
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("error reading the module proxy respons from %s: %w", u, err)
			}
			f, err := modfile.ParseLax(mod+"@"+version+"/go.mod", data, nil)
			if err != nil {
				return nil, fmt.Errorf("error parsing go.mod from %s: %w", u, err)
			}
			return f, nil
		case http.StatusNotFound, http.StatusGone:
			// try the next proxy
			continue
		default:
			return nil, fmt.Errorf("unexpected response code from %s: %s", u, resp.Status)
		}
	}
	return nil, fmt.Errorf("the specified module was not found")
}

// Getter defines a type, such as http.Client, that can perform an HTTP GET request and return
// the result.
//
// This interface is defined so that consumers and tests can provide potentially customized implementations,
// but http.DefaultClient (or some other constructed http.Client instance) will likely be the most
// common implementation used.
type Getter interface {
	Get(url string) (*http.Response, error)
}

// GetCurrentVersion returns the highest known version of the specified module, as returned by the
// system's module proxy.
func GetCurrentVersion(g Getter, mod string, includePrerelease bool) (string, error) {
	p := NewFromEnv(g)
	return p.GetCurrentVersion(mod, includePrerelease)
}

// GetModuleVersions uses the provided getter instance to retrieve a list of module versions for the
// specified module by querying the system Go module proxy ($GOPROXY)
func GetModuleVersions(g Getter, mod string) ([]string, error) {
	p := NewFromEnv(g)
	return p.GetModuleVersions(mod)
}

// GetModFile uses the provided getter instance to retrieve the go.mod file for the specified module
// by querying the system Go module proxy ($GOPROXY)
func GetModFile(g Getter, mod, version string) (*modfile.File, error) {
	p := NewFromEnv(g)
	return p.GetModFile(mod, version)
}

// getModProxies returns a list of Go module proxies by parsing the GOPROXY environment variable.  If
// no proxy is set ($GOPROXY is unset or "") this function returns a single result containing the
// Google public proxy.
func getModProxies() []string {
	ev := os.Getenv("GOPROXY")
	if ev == "" {
		return []string{"https://proxy.golang.org"}
	}
	var results []string
	for _, s := range strings.Split(ev, ",") {
		// not trying to deal with pulling go.mod directly from various VCSs for now
		if s != "direct" {
			results = append(results, s)
		}
	}
	return results
}
