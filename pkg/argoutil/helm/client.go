package helm

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	"gopkg.in/yaml.v3"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"

	"github.com/MacroPower/kclipper/pkg/argoutil/sync"
	"github.com/MacroPower/kclipper/pkg/pathutil"
)

var (
	globalLock = sync.NewKeyLock()
	indexLock  = sync.NewKeyLock()

	ErrOCINotEnabled = errors.New("could not perform the action when oci is not enabled")
)

type Creds struct {
	Username           string
	Password           string
	CAPath             string
	CertData           []byte
	KeyData            []byte
	InsecureSkipVerify bool
}

type Client interface {
	CleanChartCache(chart string, version string, project string) error
	PullChart(chart string, version string, project string, passCredentials bool, manifestMaxExtractedSize int64, disableManifestMaxExtractedSize bool) (string, error)
	ExtractChart(chart string, version string, project string, passCredentials bool, manifestMaxExtractedSize int64, disableManifestMaxExtractedSize bool) (string, io.Closer, error)
	GetIndex(noCache bool, maxIndexSize int64) (*Index, error)
	GetTags(chart string, noCache bool) (*TagsList, error)
	TestHelmOCI() (bool, error)
}

type ClientOpts func(c *nativeHelmChart)

func WithChartPaths(chartPaths pathutil.TempPaths) ClientOpts {
	return func(c *nativeHelmChart) {
		c.chartCachePaths = chartPaths
	}
}

func NewClient(repoURL string, creds Creds, enableOci bool, proxy string, noProxy string, opts ...ClientOpts) Client {
	return NewClientWithLock(repoURL, creds, globalLock, enableOci, proxy, noProxy, opts...)
}

func NewClientWithLock(repoURL string, creds Creds, repoLock sync.KeyLock, enableOci bool, proxy string, noProxy string, opts ...ClientOpts) Client {
	c := &nativeHelmChart{
		repoURL:         repoURL,
		creds:           creds,
		repoLock:        repoLock,
		enableOci:       enableOci,
		proxy:           proxy,
		noProxy:         noProxy,
		chartCachePaths: pathutil.NewRandomizedTempPaths(os.TempDir()),
	}
	for i := range opts {
		opts[i](c)
	}
	return c
}

var _ Client = &nativeHelmChart{}

type nativeHelmChart struct {
	chartCachePaths pathutil.TempPaths
	repoURL         string
	creds           Creds
	repoLock        sync.KeyLock
	enableOci       bool
	proxy           string
	noProxy         string
}

func fileExist(filePath string) (bool, error) {
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("error checking file existence for %s: %w", filePath, err)
	}
	return true, nil
}

func (c *nativeHelmChart) CleanChartCache(chart string, version string, project string) error {
	cachePath, err := c.getCachedChartPath(chart, version, project)
	if err != nil {
		return fmt.Errorf("error getting cached chart path: %w", err)
	}
	if err := os.RemoveAll(cachePath); err != nil {
		return fmt.Errorf("error removing chart cache at %s: %w", cachePath, err)
	}
	return nil
}

func untarChart(tempDir string, cachedChartPath string, manifestMaxExtractedSize int64, disableManifestMaxExtractedSize bool) error {
	if disableManifestMaxExtractedSize {
		manifestMaxExtractedSize = 0
	}
	reader, err := os.Open(cachedChartPath)
	if err != nil {
		return fmt.Errorf("error opening cached chart path %s: %w", cachedChartPath, err)
	}
	return gunzip(tempDir, reader, manifestMaxExtractedSize, false)
}

func (c *nativeHelmChart) PullChart(chart string, version string, project string, passCredentials bool, manifestMaxExtractedSize int64, disableManifestMaxExtractedSize bool) (string, error) {
	// always use Helm V3 since we don't have chart content to determine correct Helm version
	helmCmd, err := NewCmdWithVersion("", c.enableOci, c.proxy, c.noProxy)
	if err != nil {
		return "", fmt.Errorf("error creating Helm command: %w", err)
	}
	defer helmCmd.Close()

	cachedChartPath, err := c.getCachedChartPath(chart, version, project)
	if err != nil {
		return "", fmt.Errorf("error getting cached chart path: %w", err)
	}

	c.repoLock.Lock(cachedChartPath)
	defer c.repoLock.Unlock(cachedChartPath)

	// check if chart tar is already downloaded
	exists, err := fileExist(cachedChartPath)
	if err != nil {
		return "", fmt.Errorf("error checking existence of cached chart path: %w", err)
	}

	if !exists {
		// create empty temp directory to extract chart from the registry
		tempDest, err := createTempDir(os.TempDir())
		if err != nil {
			return "", fmt.Errorf("error creating temporary destination directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(tempDest) }()

		if c.enableOci {
			if c.creds.Password != "" && c.creds.Username != "" {
				_, err = helmCmd.RegistryLogin(c.repoURL, c.creds)
				if err != nil {
					return "", fmt.Errorf("error logging into OCI registry: %w", err)
				}

				defer func() {
					_, _ = helmCmd.RegistryLogout(c.repoURL, c.creds)
				}()
			}

			// 'helm pull' ensures that chart is downloaded into temp directory
			_, err = helmCmd.PullOCI(c.repoURL, chart, version, tempDest, c.creds)
			if err != nil {
				return "", fmt.Errorf("error pulling OCI chart: %w", err)
			}
		} else {
			_, err = helmCmd.Fetch(c.repoURL, chart, version, tempDest, c.creds, passCredentials)
			if err != nil {
				return "", fmt.Errorf("error fetching chart: %w", err)
			}
		}

		// 'helm pull/fetch' file downloads chart into the tgz file and we move that to where we want it
		infos, err := os.ReadDir(tempDest)
		if err != nil {
			return "", fmt.Errorf("error reading directory %s: %w", tempDest, err)
		}
		if len(infos) != 1 {
			return "", fmt.Errorf("expected 1 file, found %v", len(infos))
		}

		chartFilePath := filepath.Join(tempDest, infos[0].Name())

		err = os.Rename(chartFilePath, cachedChartPath)
		if err != nil {
			return "", fmt.Errorf("error renaming file from %s to %s: %w", chartFilePath, cachedChartPath, err)
		}
	}

	return cachedChartPath, nil
}

func (c *nativeHelmChart) ExtractChart(chart string, version string, project string, passCredentials bool, manifestMaxExtractedSize int64, disableManifestMaxExtractedSize bool) (string, io.Closer, error) {
	// throw away temp directory that stores extracted chart and should be deleted as soon as no longer needed by returned closer
	tempDir, err := createTempDir(os.TempDir())
	if err != nil {
		return "", nil, fmt.Errorf("error creating temporary directory: %w", err)
	}

	cachedChartPath, err := c.PullChart(chart, version, project, passCredentials, manifestMaxExtractedSize, disableManifestMaxExtractedSize)
	if err != nil {
		return "", nil, fmt.Errorf("error extracting chart: %w", err)
	}

	err = untarChart(tempDir, cachedChartPath, manifestMaxExtractedSize, disableManifestMaxExtractedSize)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return "", nil, fmt.Errorf("error untarring chart: %w", err)
	}

	return path.Join(tempDir, normalizeChartName(chart)), newInlineCloser(func() error {
		return os.RemoveAll(tempDir)
	}), nil
}

func (c *nativeHelmChart) GetIndex(noCache bool, maxIndexSize int64) (*Index, error) {
	indexLock.Lock(c.repoURL)
	defer indexLock.Unlock(c.repoURL)

	var data []byte

	if len(data) == 0 {
		start := time.Now()
		var err error
		data, err = c.loadRepoIndex(maxIndexSize)
		if err != nil {
			return nil, fmt.Errorf("error loading repo index: %w", err)
		}
		slog.Info("got index data", "seconds", time.Since(start).Seconds())
	}

	index := &Index{}
	err := yaml.NewDecoder(bytes.NewBuffer(data)).Decode(index)
	if err != nil {
		return nil, fmt.Errorf("error decoding index: %w", err)
	}

	return index, nil
}

func (c *nativeHelmChart) TestHelmOCI() (bool, error) {
	start := time.Now()

	tmpDir, err := os.MkdirTemp("", "helm")
	if err != nil {
		return false, fmt.Errorf("error creating temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	helmCmd, err := NewCmdWithVersion(tmpDir, c.enableOci, c.proxy, c.noProxy)
	if err != nil {
		return false, fmt.Errorf("error creating Helm command: %w", err)
	}
	defer helmCmd.Close()

	// Looks like there is no good way to test access to OCI repo if credentials are not provided
	// just assume it is accessible
	if c.creds.Username != "" && c.creds.Password != "" {
		_, err = helmCmd.RegistryLogin(c.repoURL, c.creds)
		if err != nil {
			return false, fmt.Errorf("error logging into OCI registry: %w", err)
		}
		defer func() {
			_, _ = helmCmd.RegistryLogout(c.repoURL, c.creds)
		}()

		slog.Info("tested helm oci repository", "seconds", time.Since(start).Seconds())
	}
	return true, nil
}

func (c *nativeHelmChart) loadRepoIndex(maxIndexSize int64) ([]byte, error) {
	indexURL, err := getIndexURL(c.repoURL)
	if err != nil {
		return nil, fmt.Errorf("error getting index URL: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}
	if c.creds.Username != "" || c.creds.Password != "" {
		// only basic supported
		req.SetBasicAuth(c.creds.Username, c.creds.Password)
	}

	tlsConf, err := newTLSConfig(c.creds)
	if err != nil {
		return nil, fmt.Errorf("error creating TLS config: %w", err)
	}

	tr := &http.Transport{
		Proxy:             getCallback(c.proxy, c.noProxy),
		TLSClientConfig:   tlsConf,
		DisableKeepAlives: true,
	}
	client := http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making HTTP request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed to get index: " + resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexSize))
	if err != nil {
		return nil, fmt.Errorf("error reading index response body: %w", err)
	}

	return data, nil
}

func newTLSConfig(creds Creds) (*tls.Config, error) {
	//nolint:gosec
	tlsConfig := &tls.Config{InsecureSkipVerify: creds.InsecureSkipVerify}

	if creds.CAPath != "" {
		caData, err := os.ReadFile(creds.CAPath)
		if err != nil {
			return nil, fmt.Errorf("error reading CA file %s: %w", creds.CAPath, err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caData)
		tlsConfig.RootCAs = caCertPool
	}

	// If a client cert & key is provided then configure TLS config accordingly.
	if len(creds.CertData) > 0 && len(creds.KeyData) > 0 {
		cert, err := tls.X509KeyPair(creds.CertData, creds.KeyData)
		if err != nil {
			return nil, fmt.Errorf("error creating X509 key pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	//nolint:staticcheck
	tlsConfig.BuildNameToCertificate()

	return tlsConfig, nil
}

// Normalize a chart name for file system use, that is, if chart name is foo/bar/baz, returns the last component as chart name.
func normalizeChartName(chart string) string {
	strings.Join(strings.Split(chart, "/"), "_")
	_, nc := path.Split(chart)
	// We do not want to return the empty string or something else related to filesystem access
	// Instead, return original string
	if nc == "" || nc == "." || nc == ".." {
		return chart
	}
	return nc
}

func (c *nativeHelmChart) getCachedChartPath(chart string, version string, project string) (string, error) {
	keyData, err := json.Marshal(map[string]string{"url": c.repoURL, "chart": chart, "version": version, "project": project})
	if err != nil {
		return "", fmt.Errorf("error marshaling cache key data: %w", err)
	}
	chartPath, err := c.chartCachePaths.GetPath(string(keyData))
	if err != nil {
		return "", fmt.Errorf("error getting chart cache path: %w", err)
	}
	return chartPath, nil
}

// Ensures that given OCI registries URL does not have protocol.
func IsHelmOciRepo(repoURL string) bool {
	if repoURL == "" {
		return false
	}
	parsed, err := url.Parse(repoURL)
	// the URL parser treat hostname as either path or opaque if scheme is not specified, so hostname must be empty
	return err == nil && parsed.Host == ""
}

func getIndexURL(rawURL string) (string, error) {
	indexFile := "index.yaml"
	repoURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("error parsing repository URL: %w", err)
	}
	repoURL.Path = path.Join(repoURL.Path, indexFile)
	repoURL.RawPath = path.Join(repoURL.RawPath, indexFile)
	return repoURL.String(), nil
}

func (c *nativeHelmChart) GetTags(chart string, noCache bool) (*TagsList, error) {
	if !c.enableOci {
		return nil, ErrOCINotEnabled
	}

	tagsURL := strings.Replace(fmt.Sprintf("%s/%s", c.repoURL, chart), "https://", "", 1)
	indexLock.Lock(tagsURL)
	defer indexLock.Unlock(tagsURL)

	var data []byte

	tags := &TagsList{}
	if len(data) == 0 {
		start := time.Now()
		repo, err := remote.NewRepository(tagsURL)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize repository: %w", err)
		}
		tlsConf, err := newTLSConfig(c.creds)
		if err != nil {
			return nil, fmt.Errorf("failed setup tlsConfig: %w", err)
		}
		client := &http.Client{Transport: &http.Transport{
			Proxy:             getCallback(c.proxy, c.noProxy),
			TLSClientConfig:   tlsConf,
			DisableKeepAlives: true,
		}}

		repoHost, _, _ := strings.Cut(tagsURL, "/")
		repo.Client = &auth.Client{
			Client: client,
			Cache:  nil,
			Credential: auth.StaticCredential(repoHost, auth.Credential{
				Username: c.creds.Username,
				Password: c.creds.Password,
			}),
		}

		ctx := context.Background()
		err = repo.Tags(ctx, "", func(tagsResult []string) error {
			for _, tag := range tagsResult {
				// By convention: Change underscore (_) back to plus (+) to get valid SemVer
				convertedTag := strings.ReplaceAll(tag, "_", "+")
				tags.Tags = append(tags.Tags, convertedTag)
			}

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get tags: %w", err)
		}
		slog.Info("got tags", "seconds", time.Since(start).Seconds(), "chart", chart, "repo", c.repoURL)
	} else {
		err := json.Unmarshal(data, tags)
		if err != nil {
			return nil, fmt.Errorf("failed to decode tags: %w", err)
		}
	}

	return tags, nil
}

// getCallback returns the proxy callback function.
func getCallback(proxy string, noProxy string) func(*http.Request) (*url.URL, error) {
	if proxy != "" {
		c := httpproxy.Config{
			HTTPProxy:  proxy,
			HTTPSProxy: proxy,
			NoProxy:    noProxy,
		}
		return func(r *http.Request) (*url.URL, error) {
			if r != nil {
				return c.ProxyFunc()(r.URL)
			}
			return url.Parse(c.HTTPProxy)
		}
	}
	// read proxy from env variable if custom proxy is missing
	return http.ProxyFromEnvironment
}
