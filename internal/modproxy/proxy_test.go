package modproxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/mod/modfile"
)

type getterFunc func(string) (*http.Response, error)

func (f getterFunc) Get(url string) (*http.Response, error) {
	return f(url)
}

func TestListModuleVersions(t *testing.T) {
	// hard code 2 module proxies so that the "first proxy returns 404" test will actually have a
	// 2nd proxy to hit
	testProxies := []string{"https://one", "https://two"}
	type testCase struct {
		name     string
		p        Proxy
		expected []string
		checkErr func(*testing.T, error)
	}
	testErr := fmt.Errorf("oh no")
	cases := []testCase{
		{
			name: "server returned an error",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return nil, testErr
			}), testProxies...),
			checkErr: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, testErr)
			},
		},
		{
			name: "valid response/valid results",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte("v0.1.0\nv0.2.0"))),
				}, nil
			}), testProxies...),
			expected: []string{"v0.1.0", "v0.2.0"},
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "first proxy returns 404",
			p: New(func() getterFunc {
				n := 0
				return getterFunc(func(string) (*http.Response, error) {
					if n == 0 {
						n++
						return &http.Response{
							StatusCode: http.StatusNotFound,
						}, nil
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBuffer([]byte("v0.1.0\nv0.2.0"))),
					}, nil
				})
			}(), testProxies...),
			expected: []string{"v0.1.0", "v0.2.0"},
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
	}
	t.Parallel()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.p.GetModuleVersions("github.com/foo/bar")
			tc.checkErr(t, err)
			assert.ElementsMatch(t, tc.expected, got)
		})
	}
}

func TestGetCurrentVersion(t *testing.T) {
	type testCase struct {
		name              string
		p                 Proxy
		includePrerelease bool
		expected          string
		checkErr          func(*testing.T, error)
	}
	cases := []testCase{
		{
			name: "no versions",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte(""))),
				}, nil
			}), "https://proxy.golang.org"),
			expected: "",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
			},
		},
		{
			name: "only 1 version",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte("v0.1.0"))),
				}, nil
			}), "https://proxy.golang.org"),
			expected: "v0.1.0",
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "multiple versions",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte("v0.1.0\nv1.0.0\nv1.0.1\nv1.10.0\nv1.2.0\nv1.20.0"))),
				}, nil
			}), "https://proxy.golang.org"),
			expected: "v1.20.0",
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "multiple versions with prereleases",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte("v0.1.0\nv1.0.0\nv1.0.1\nv1.10.0\nv1.2.0\nv1.20.0\nv1.3.0-alpha"))),
				}, nil
			}), "https://proxy.golang.org"),
			expected: "v1.20.0",
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "multiple versions with prerelease for vNext",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte("v0.1.0\nv1.0.0\nv1.0.1\nv1.10.0\nv1.2.0\nv1.20.0\nv1.21.0-rc1\nv1.3.0-alpha"))),
				}, nil
			}), "https://proxy.golang.org"),
			expected: "v1.20.0",
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "multiple versions with prerelease for vNext when including prerelease",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte("v0.1.0\nv1.0.0\nv1.0.1\nv1.10.0\nv1.2.0\nv1.20.0\nv1.21.0-rc1\nv1.3.0-alpha"))),
				}, nil
			}), "https://proxy.golang.org"),
			includePrerelease: true,
			expected:          "v1.21.0-rc1",
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
	}
	t.Parallel()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.p.GetCurrentVersion("github.com/foo/bar", tc.includePrerelease)
			tc.checkErr(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestDownloadModFile(t *testing.T) {
	const modContents = "module github.com/foo/bar\n\ngo 1.18"
	// hard code 2 module proxies so that the "first proxy returns 404" test will actually have a
	// 2nd proxy to hit
	testProxies := []string{"https://one", "https://two"}
	type testCase struct {
		name     string
		p        Proxy
		expected string
		checkErr func(*testing.T, error)
	}
	testErr := fmt.Errorf("oh no")
	cases := []testCase{
		{
			name: "server returned an error",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{}, testErr
			}), testProxies...),
			checkErr: func(t *testing.T, err error) {
				assert.True(t, errors.Is(err, testErr))
			},
		},
		{
			name: "valid response",
			p: New(getterFunc(func(string) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBuffer([]byte(modContents))),
				}, nil
			}), testProxies...),
			expected: modContents,
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "first proxy returns 404",
			p: New(func() getterFunc {
				n := 0
				return getterFunc(func(string) (*http.Response, error) {
					if n == 0 {
						n++
						return &http.Response{
							StatusCode: http.StatusNotFound,
						}, nil
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBuffer([]byte(modContents))),
					}, nil
				})
			}(), testProxies...),
			expected: modContents,
			checkErr: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
	}
	t.Parallel()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.p.GetModFile("github.com/foo/bar", "v0.0.0")
			tc.checkErr(t, err)

			var mod *modfile.File
			if err == nil {
				mod, _ = modfile.ParseLax("github.com/foo/bar@v0.0.0/go.mod", []byte(tc.expected), nil)
			}
			assert.Equal(t, mod, got, "go.mod contents must match")
		})
	}
}
