package diary

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"boku/internal/domain"
	"boku/internal/platform/httpc"
)

const (
	WeatherAttribution    = "Open-Meteo · CC BY 4.0"
	WeatherAttributionURL = "https://open-meteo.com/"

	defaultGeocodingEndpoint = "https://geocoding-api.open-meteo.com/v1/search"
	defaultForecastEndpoint  = "https://api.open-meteo.com/v1/forecast"
	weatherResponseLimit     = 64 * 1024
)

// GeocodedPlace 是 Open-Meteo 地理编码返回的城市名称与经纬度（经纬度只用于当次查天气，不落盘）。
type GeocodedPlace struct {
	Name      string
	Latitude  float64
	Longitude float64
}

// WeatherService 使用 Open-Meteo 按城市名称采集当前天气快照。
type WeatherService struct {
	http             RawRequester
	geocodingBaseURL string
	forecastBaseURL  string
	now              func() time.Time
}

func NewWeatherService(http RawRequester, clock domain.Clock) *WeatherService {
	if clock == nil {
		clock = domain.SystemClock{}
	}
	return &WeatherService{
		http:             http,
		geocodingBaseURL: defaultGeocodingEndpoint,
		forecastBaseURL:  defaultForecastEndpoint,
		now:              clock.Now,
	}
}

func NewWeatherServiceWithEndpoints(http RawRequester, geocodingBaseURL, forecastBaseURL string, clock domain.Clock) *WeatherService {
	s := NewWeatherService(http, clock)
	if strings.TrimSpace(geocodingBaseURL) != "" {
		s.geocodingBaseURL = strings.TrimSpace(geocodingBaseURL)
	}
	if strings.TrimSpace(forecastBaseURL) != "" {
		s.forecastBaseURL = strings.TrimSpace(forecastBaseURL)
	}
	return s
}

func (s *WeatherService) Geocode(ctx context.Context, rawPlace string) (GeocodedPlace, error) {
	place := strings.TrimSpace(rawPlace)
	if place == "" || len([]rune(place)) > 60 {
		return GeocodedPlace{}, fmt.Errorf("%w: 请输入城市或地区名称", domain.ErrInvalidArgument)
	}
	if s == nil || s.http == nil {
		return GeocodedPlace{}, domain.ErrProviderUnavailable
	}
	u, err := url.Parse(s.geocodingBaseURL)
	if err != nil {
		return GeocodedPlace{}, &domain.BadResponseError{ProviderID: "open-meteo", Reason: "天气地点请求无效"}
	}
	q := u.Query()
	q.Set("name", place)
	q.Set("count", "1")
	q.Set("language", "zh")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	resp, err := s.http.Raw(ctx, "GET", u.String(), nil, nil, httpc.RawOptions{
		Timeout:  12 * time.Second,
		MaxBytes: weatherResponseLimit,
	})
	if err != nil {
		return GeocodedPlace{}, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return GeocodedPlace{}, &domain.BadResponseError{
			ProviderID: "open-meteo",
			HTTPStatus: resp.Status,
			Reason:     "天气地点查询失败",
		}
	}
	var payload struct {
		Results []struct {
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
		} `json:"results"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return GeocodedPlace{}, &domain.BadResponseError{ProviderID: "open-meteo", Reason: "天气地点响应无效"}
	}
	if len(payload.Results) == 0 {
		return GeocodedPlace{}, &domain.BadResponseError{ProviderID: "open-meteo", Reason: "没有找到这个地点"}
	}
	first := payload.Results[0]
	if !validCoord(first.Latitude, first.Longitude) {
		return GeocodedPlace{}, &domain.BadResponseError{ProviderID: "open-meteo", Reason: "天气地点坐标无效"}
	}
	name := strings.TrimSpace(first.Name)
	if name == "" {
		name = place
	}
	ctxName := ""
	for _, candidate := range []string{first.Admin1, first.Country} {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" && trimmed != name {
			ctxName = trimmed
			break
		}
	}
	display := name
	if ctxName != "" {
		display = name + " · " + ctxName
	}
	return GeocodedPlace{
		Name:      display,
		Latitude:  first.Latitude,
		Longitude: first.Longitude,
	}, nil
}

func (s *WeatherService) Snapshot(ctx context.Context, place string, latitude, longitude float64) (WeatherSnapshot, error) {
	if !validCoord(latitude, longitude) {
		return WeatherSnapshot{}, fmt.Errorf("%w: 无效的天气位置", domain.ErrInvalidArgument)
	}
	if s == nil || s.http == nil {
		return WeatherSnapshot{}, domain.ErrProviderUnavailable
	}
	u, err := url.Parse(s.forecastBaseURL)
	if err != nil {
		return WeatherSnapshot{}, &domain.BadResponseError{ProviderID: "open-meteo", Reason: "天气请求无效"}
	}
	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%.2f", latitude))
	q.Set("longitude", fmt.Sprintf("%.2f", longitude))
	q.Set("current", "temperature_2m,weather_code")
	q.Set("temperature_unit", "celsius")
	q.Set("timezone", "UTC")
	u.RawQuery = q.Encode()

	resp, err := s.http.Raw(ctx, "GET", u.String(), nil, nil, httpc.RawOptions{
		Timeout:  12 * time.Second,
		MaxBytes: weatherResponseLimit,
	})
	if err != nil {
		return WeatherSnapshot{}, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return WeatherSnapshot{}, &domain.BadResponseError{
			ProviderID: "open-meteo",
			HTTPStatus: resp.Status,
			Reason:     "天气查询失败",
		}
	}
	var payload struct {
		Current *struct {
			Time        string   `json:"time"`
			Temperature *float64 `json:"temperature_2m"`
			WeatherCode *int     `json:"weather_code"`
		} `json:"current"`
		CurrentUnits *struct {
			Temperature string `json:"temperature_2m"`
		} `json:"current_units"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil || payload.Current == nil || payload.Current.Temperature == nil || payload.Current.WeatherCode == nil {
		return WeatherSnapshot{}, &domain.BadResponseError{ProviderID: "open-meteo", Reason: "天气响应无效"}
	}
	temp := *payload.Current.Temperature
	if math.IsNaN(temp) || math.IsInf(temp, 0) || temp < -100 || temp > 100 {
		return WeatherSnapshot{}, &domain.BadResponseError{ProviderID: "open-meteo", Reason: "天气响应温度异常"}
	}
	desc, symbol := wmoCondition(*payload.Current.WeatherCode)
	unit := "°C"
	if payload.CurrentUnits != nil && strings.TrimSpace(payload.CurrentUnits.Temperature) != "" {
		unit = strings.TrimSpace(payload.CurrentUnits.Temperature)
	}
	now := s.now().UTC()
	observed := parseOpenMeteoTime(payload.Current.Time)
	if observed.IsZero() {
		observed = now
	}
	return WeatherSnapshot{
		Place:              strings.TrimSpace(place),
		Description:        desc,
		TemperatureCelsius: temp,
		Unit:               unit,
		SymbolName:         symbol,
		Kind:               "current",
		ObservedAt:         observed,
		FetchedAt:          now,
	}, nil
}

func (s *WeatherService) SnapshotForPlace(ctx context.Context, place string) (WeatherSnapshot, error) {
	geo, err := s.Geocode(ctx, place)
	if err != nil {
		return WeatherSnapshot{}, err
	}
	return s.Snapshot(ctx, geo.Name, geo.Latitude, geo.Longitude)
}

func validCoord(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsInf(lat, 0) &&
		!math.IsNaN(lon) && !math.IsInf(lon, 0) &&
		math.Abs(lat) <= 90 && math.Abs(lon) <= 180
}

func wmoCondition(code int) (description, symbolName string) {
	switch code {
	case 0:
		return "晴", "sun.max.fill"
	case 1:
		return "大部晴朗", "cloud.sun.fill"
	case 2:
		return "多云", "cloud.sun.fill"
	case 3:
		return "阴", "cloud.fill"
	case 45, 48:
		return "雾", "cloud.fog.fill"
	case 51, 53, 55:
		return "毛毛雨", "cloud.drizzle.fill"
	case 56, 57, 66, 67:
		return "冻雨", "cloud.rain.fill"
	case 61, 63, 65, 80, 81, 82:
		return "雨", "cloud.rain.fill"
	case 71, 73, 75, 77, 85, 86:
		return "雪", "cloud.snow.fill"
	case 95, 96, 99:
		return "雷暴", "cloud.bolt.rain.fill"
	default:
		return "天气", "cloud.fill"
	}
}

func parseOpenMeteoTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02T15:04", raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
