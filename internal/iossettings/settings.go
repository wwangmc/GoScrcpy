// Package iossettings applies system language, locale and time zone presets.
package iossettings

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
)

//go:embed data/region_setting.json
var presets embed.FS

type Choice struct {
	Region   string `json:"region"`
	Name     string `json:"name"`
	Locale   string `json:"locale"`
	Language string `json:"language"`
	TimeZone string `json:"timeZone"`
}

var once sync.Once
var catalog map[string]Choice
var loadError error

func load() {
	once.Do(func() {
		regionData, err := presets.ReadFile("data/region_setting.json")
		if err != nil {
			loadError = err
			return
		}
		var regions map[string]struct{ Name, Locale, Lang, TimeZone string }
		if err = json.Unmarshal(regionData, &regions); err != nil {
			loadError = err
			return
		}
		catalog = make(map[string]Choice)
		for key, region := range regions {
			key = strings.ToUpper(key)
			if region.Locale == "" || region.Lang == "" || region.TimeZone == "" || region.Name == "" {
				loadError = errors.New("内置 iOS 地区配置不完整")
				return
			}
			catalog[key] = Choice{key, region.Name, region.Locale, region.Lang, region.TimeZone}
		}
	})
}
func Lookup(region string) (Choice, error) {
	load()
	if loadError != nil {
		return Choice{}, loadError
	}
	entry, exists := catalog[strings.ToUpper(strings.TrimSpace(region))]
	if !exists {
		return Choice{}, errors.New("该地区尚未适配 iOS 一键设置")
	}
	return entry, nil
}
func Choices() ([]Choice, error) {
	load()
	if loadError != nil {
		return nil, loadError
	}
	choices := make([]Choice, 0, len(catalog))
	for _, entry := range catalog {
		choices = append(choices, entry)
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Region < choices[j].Region })
	return choices, nil
}

type JSONCaller interface {
	JSON(context.Context, string, any) (string, error)
}

func Apply(ctx context.Context, api JSONCaller, region string) (Choice, error) {
	choice, err := Lookup(region)
	if err != nil {
		return Choice{}, err
	}
	// Preserve the legacy POST/query contract, with an empty JSON object so neither
	// plugin receives application-specific configuration or carrier parameters.
	query := url.Values{"timeZone": {choice.TimeZone}, "locale": {choice.Locale}, "lang": {choice.Language}}
	_, err = api.JSON(ctx, "/setAllSetting?"+query.Encode(), map[string]string{})
	return choice, err
}
