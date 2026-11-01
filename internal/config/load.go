package config

import (
	"bytes"
	"os"
	P "path"
	"runtime"
	"strings"

	"github.com/Ember-Moth/libclash/internal/app"
	"github.com/Ember-Moth/libclash/internal/dns"

	"github.com/metacubex/mihomo/common/yaml"
	"github.com/metacubex/mihomo/config"
	"github.com/metacubex/mihomo/log"
)

func logDns(cfg *config.RawConfig) {
	bytes, err := yaml.Marshal(&cfg.DNS)
	if err != nil {
		log.Warnln("Marshal dns: %s", err.Error())

		return
	}

	log.Infoln("dns:")

	for _, line := range strings.Split(string(bytes), "\n") {
		log.Infoln("  %s", line)
	}
}

func UnmarshalAndPatch(profilePath string) (*config.RawConfig, error) {
	return unmarshalAndPatchFile(P.Join(profilePath, "config.yaml"), profilePath)
}

func unmarshalAndPatchFile(configPath, profilePath string) (*config.RawConfig, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	return unmarshalAndPatchData(configData, profilePath)
}

func unmarshalAndPatchData(configData []byte, profilePath string) (*config.RawConfig, error) {
	rawConfig, err := config.UnmarshalRawConfig(configData)
	if err != nil {
		return nil, err
	}

	if err := process(rawConfig, profilePath); err != nil {
		return nil, err
	}

	return rawConfig, nil
}

func Parse(rawConfig *config.RawConfig) (*config.Config, error) {
	cfg, err := config.ParseRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate releases providers created only for checking a configuration.
func Validate(rawConfig *config.RawConfig) error {
	cfg, err := Parse(rawConfig)
	if err != nil {
		return err
	}
	destroyProviders(cfg)
	return nil
}

func Load(path string) error {
	return loadFile(path, P.Join(path, "config.yaml"), false)
}

// LoadFromFile validates a replacement in the destination profile's context
// before changing its config.yaml or the running configuration.
func LoadFromFile(path, source string) error {
	return loadFile(path, source, true)
}

func loadFile(path, source string, install bool) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	rawCfg, err := unmarshalAndPatchData(data, path)
	if err != nil {
		log.Errorln("Load %s: %s", path, err.Error())

		return err
	}

	logDns(rawCfg)

	cfg, err := Parse(rawCfg)
	if err != nil {
		log.Errorln("Load %s: %s", path, err.Error())

		return err
	}

	if install {
		if err := writeFileAtomically(P.Join(path, "config.yaml"), bytes.NewReader(data)); err != nil {
			destroyProviders(cfg)
			return err
		}
	}

	// like hub.Parse()
	dns.ApplyConfig(cfg)

	app.ApplySubtitlePattern(rawCfg.ClashForAndroid.UiSubtitlePattern)

	runtime.GC()

	return nil
}

func LoadDefault() {
	cfg, err := config.Parse([]byte{})
	if err != nil {
		panic(err.Error())
	}

	dns.ApplyConfig(cfg)
}
