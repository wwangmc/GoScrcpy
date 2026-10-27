// Package iossettings ports the region and carrier presets from the user's Chrome extension.
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

//go:embed data/region_setting.json data/opt.json
var presets embed.FS

type Choice struct {
	Region       string `json:"region"`
	Name         string `json:"name"`
	Locale       string `json:"locale"`
	Language     string `json:"language"`
	TimeZone     string `json:"timeZone"`
	CarrierCount int    `json:"carrierCount"`
}
type preset struct {
	choice Choice
	body   json.RawMessage
}

var once sync.Once
var catalog map[string]preset
var loadError error

func load() {
	once.Do(func() {
		regionData, err := presets.ReadFile("data/region_setting.json")
		if err != nil {
			loadError = err
			return
		}
		carrierData, err := presets.ReadFile("data/opt.json")
		if err != nil {
			loadError = err
			return
		}
		var regions map[string]struct{ Locale, Lang, TimeZone string }
		var carriers map[string]json.RawMessage
		if err = json.Unmarshal(regionData, &regions); err != nil {
			loadError = err
			return
		}
		if err = json.Unmarshal(carrierData, &carriers); err != nil {
			loadError = err
			return
		}
		normalized := make(map[string]json.RawMessage, len(carriers))
		for key, value := range carriers {
			normalized[strings.ToUpper(key)] = value
		}
		catalog = make(map[string]preset)
		for key, region := range regions {
			key = strings.ToUpper(key)
			body, exists := normalized[key]
			if !exists {
				continue
			}
			var carrier struct {
				Name string            `json:"country_name"`
				Opts []json.RawMessage `json:"opts"`
			}
			if err = json.Unmarshal(body, &carrier); err != nil {
				loadError = err
				return
			}
			if region.Locale == "" || region.Lang == "" || region.TimeZone == "" || carrier.Name == "" || len(carrier.Opts) == 0 {
				loadError = errors.New("内置 iOS 地区配置不完整")
				return
			}
			catalog[key] = preset{Choice{key, carrier.Name, region.Locale, region.Lang, region.TimeZone, len(carrier.Opts)}, body}
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
	return entry.choice, nil
}
func Choices() ([]Choice, error) {
	load()
	if loadError != nil {
		return nil, loadError
	}
	choices := make([]Choice, 0, len(catalog))
	for _, entry := range catalog {
		choices = append(choices, entry.choice)
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
	// Keep the extension's query names and complete carrier payload unchanged.
	query := url.Values{"timeZone": {choice.TimeZone}, "locale": {choice.Locale}, "lang": {choice.Language}}
	_, err = api.JSON(ctx, "/setAllSetting?"+query.Encode(), catalog[choice.Region].body)
	return choice, err
}
