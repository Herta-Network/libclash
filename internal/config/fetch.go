package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	U "net/url"
	"os"
	P "path"
	"runtime"
	"time"

	"github.com/Ember-Moth/libclash/internal/app"

	clashHttp "github.com/metacubex/mihomo/component/http"
)

type Status struct {
	Action      string   `json:"action"`
	Args        []string `json:"args"`
	Progress    int      `json:"progress"`
	MaxProgress int      `json:"max"`
}

func openUrl(ctx context.Context, url string) (io.ReadCloser, error) {
	response, err := clashHttp.HttpRequest(ctx, url, http.MethodGet, http.Header{"User-Agent": {"ClashMetaForAndroid/" + app.VersionName()}}, nil)

	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %s", response.Status)
	}

	return response.Body, nil
}

func openContent(url string) (io.ReadCloser, error) {
	return app.OpenContent(url)
}

func fetch(url *U.URL, file string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var reader io.ReadCloser
	var err error

	switch url.Scheme {
	case "http", "https":
		reader, err = openUrl(ctx, url.String())
	case "content":
		reader, err = openContent(url.String())
	default:
		err = fmt.Errorf("unsupported scheme %s of %s", url.Scheme, url)
	}

	if err != nil {
		return err
	}

	defer reader.Close()

	return writeFileAtomically(file, reader)
}

func FetchAndValid(
	path string,
	url string,
	force bool,
	reportStatus func(string),
) error {
	configPath := P.Join(path, "config.yaml")
	candidatePath := configPath
	if reportStatus == nil {
		reportStatus = func(string) {}
	}

	_, statErr := os.Stat(configPath)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if os.IsNotExist(statErr) || force {
		url, err := U.Parse(url)
		if err != nil {
			return err
		}

		bytes, _ := json.Marshal(&Status{
			Action:      "FetchConfiguration",
			Args:        []string{url.Host},
			Progress:    -1,
			MaxProgress: -1,
		})

		reportStatus(string(bytes))

		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		candidate, err := os.CreateTemp(path, ".config-*.yaml")
		if err != nil {
			return err
		}
		candidatePath = candidate.Name()
		defer os.Remove(candidatePath)
		if err := candidate.Close(); err != nil {
			return err
		}
		if err := fetch(url, candidatePath); err != nil {
			return err
		}
	}

	defer runtime.GC()

	rawCfg, err := unmarshalAndPatchFile(candidatePath, path)
	if err != nil {
		return err
	}

	var providerErr error
	forEachProviders(rawCfg, func(index int, total int, name string, provider map[string]any, prefix string) {
		if providerErr != nil {
			return
		}
		bytes, _ := json.Marshal(&Status{
			Action:      "FetchProviders",
			Args:        []string{name},
			Progress:    index,
			MaxProgress: total,
		})

		reportStatus(string(bytes))

		u, uok := provider["url"]
		p, pok := provider["path"]

		if !uok || !pok {
			return
		}

		us, uok := u.(string)
		ps, pok := p.(string)

		if !uok || !pok {
			return
		}

		if _, err := os.Stat(ps); err == nil {
			return
		}

		url, err := U.Parse(us)
		if err != nil {
			providerErr = fmt.Errorf("provider %q: %w", name, err)
			return
		}

		if err := fetch(url, ps); err != nil {
			providerErr = fmt.Errorf("provider %q: %w", name, err)
		}
	})
	if providerErr != nil {
		return providerErr
	}

	bytes, _ := json.Marshal(&Status{
		Action:      "Verifying",
		Args:        []string{},
		Progress:    0xffff,
		MaxProgress: 0xffff,
	})

	reportStatus(string(bytes))

	if err := Validate(rawCfg); err != nil {
		return err
	}

	if candidatePath != configPath {
		return os.Rename(candidatePath, configPath)
	}
	return nil
}
