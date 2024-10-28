// handlers/atmosphere.go
package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"time"

	"smuggr.xyz/optivum-bsf/common/models"

	"github.com/gin-gonic/gin"
)

func WeatherForecastHandler(c *gin.Context) {
    lang := c.DefaultQuery("lang", "en")
    units := c.DefaultQuery("units", "metric")
    apiKey := os.Getenv("OPENWEATHER_API_KEY")

    url := fmt.Sprintf("%s%s",
        Config.OpenWeather.BaseUrl,
        fmt.Sprintf(Config.OpenWeather.Endpoints.ForecastWeather, Config.OpenWeather.Lat, Config.OpenWeather.Lon, apiKey, lang, units, 40),
    )

    // #nosec G107
    resp, err := http.Get(url)
    if err != nil {
        Respond(c, http.StatusInternalServerError, models.APIResponse{
            Message: "failed to fetch forecast data",
            Success: false,
        })
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        Respond(c, http.StatusInternalServerError, models.APIResponse{
            Message: "failed to fetch forecast data",
            Success: false,
        })
        return
    }

    var openWeatherData map[string]interface{}
    if err := json.NewDecoder(resp.Body).Decode(&openWeatherData); err != nil {
        Respond(c, http.StatusInternalServerError, models.APIResponse{
            Message: "failed to parse forecast data",
            Success: false,
        })
        return
    }

    forecastList := openWeatherData["list"].([]interface{})
    forecastResponse := &models.ForecastResponse{
        Name: openWeatherData["city"].(map[string]interface{})["name"].(string),
    }

    forecastsByDate := make(map[string][]map[string]interface{})

    for _, forecast := range forecastList {
        forecastMap := forecast.(map[string]interface{})
        dt := int64(forecastMap["dt"].(float64))
        t := time.Unix(dt, 0).UTC()
        dateStr := t.Format("2006-01-02")

        forecastsByDate[dateStr] = append(forecastsByDate[dateStr], forecastMap)
    }

    var dates []string
    for dateStr := range forecastsByDate {
        dates = append(dates, dateStr)
    }
    sort.Strings(dates)

    now := time.Now().UTC()
    currentTimeOfDaySeconds := now.Hour()*3600 + now.Minute()*60 + now.Second()

    datesProcessed := 0
    for _, dateStr := range dates {
        forecastDate, err := time.Parse("2006-01-02", dateStr)
        if err != nil {
            continue
        }

        if forecastDate.Before(now.Truncate(24 * time.Hour)) {
            continue
        }

        if datesProcessed >= 3 {
            break
        }

        forecasts := forecastsByDate[dateStr]

        minTimeDiff := int64(1<<63 - 1)
        var closestForecast map[string]interface{}

        for _, fMap := range forecasts {
            fDt := int64(fMap["dt"].(float64))
            fTime := time.Unix(fDt, 0).UTC()

            fTimeOfDaySeconds := fTime.Hour() * 3600 + fTime.Minute() * 60 + fTime.Second()
            timeDiff := int64(math.Abs(float64(fTimeOfDaySeconds - currentTimeOfDaySeconds)))

            if timeDiff < minTimeDiff {
                minTimeDiff = timeDiff
                closestForecast = fMap
            }
        }

        if closestForecast != nil {
            condition := closestForecast["weather"].([]interface{})[0].(map[string]interface{})
            temperature := closestForecast["main"].(map[string]interface{})
            sunrise := int64(openWeatherData["city"].(map[string]interface{})["sunrise"].(float64))
            sunset := int64(openWeatherData["city"].(map[string]interface{})["sunset"].(float64))

            dt := int64(closestForecast["dt"].(float64))
            t := time.Unix(dt, 0).UTC()
            dayOfWeek := int64(t.Weekday())

            forecastResponse.Forecast = append(forecastResponse.Forecast, &models.Forecast{
                Condition: &models.Condition{
                    Name:        condition["main"].(string),
                    Description: condition["description"].(string),
                },
                Temperature: &models.Temperature{
                    Current: temperature["temp"].(float64),
                    Min:     temperature["temp_min"].(float64),
                    Max:     temperature["temp_max"].(float64),
                },
                Sunrise:   sunrise,
                Sunset:    sunset,
                DayOfWeek: dayOfWeek,
            })
        }

        datesProcessed++
    }

    Respond(c, http.StatusOK, forecastResponse)
}

func CurrentWeatherHandler(c *gin.Context) {
	lang := c.DefaultQuery("lang", "en")
	units := c.DefaultQuery("units", "metric")
	apiKey := os.Getenv("OPENWEATHER_API_KEY")

	url := fmt.Sprintf("%s%s",
		Config.OpenWeather.BaseUrl,
		fmt.Sprintf(Config.OpenWeather.Endpoints.CurrentWeather, Config.OpenWeather.Lat, Config.OpenWeather.Lon, apiKey, lang, units),
	)

	// #nosec G107 - URL is constructed from trusted configuration and validated
	resp, err := http.Get(url)
	if err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "failed to fetch current weather data",
			Success: false,
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "failed to fetch current weather data",
			Success: false,
		})
		return
	}

	var openWeatherData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&openWeatherData); err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "failed to parse current weather data",
			Success: false,
		})
		return
	}

	condition := openWeatherData["weather"].([]interface{})[0].(map[string]interface{})
	temperature := openWeatherData["main"].(map[string]interface{})
	sunrise := int64(openWeatherData["sys"].(map[string]interface{})["sunrise"].(float64))
	sunset := int64(openWeatherData["sys"].(map[string]interface{})["sunset"].(float64))

	currentWeatherResponse := &models.CurrentWeatherResponse{
		Name: openWeatherData["name"].(string),
		Condition: &models.Condition{
			Name:        condition["main"].(string),
			Description: condition["description"].(string),
		},
		Temperature: &models.Temperature{
			Current: temperature["temp"].(float64),
			Min:     temperature["temp_min"].(float64),
			Max:     temperature["temp_max"].(float64),
		},
		Sunrise: sunrise,
		Sunset:  sunset,
	}

	Respond(c, http.StatusOK, currentWeatherResponse)
}

func CurrentAirPollutionHandler(c *gin.Context) {
	apiKey := os.Getenv("OPENWEATHER_API_KEY")

	url := fmt.Sprintf("%s%s",
		Config.OpenWeather.BaseUrl,
		fmt.Sprintf(Config.OpenWeather.Endpoints.CurrentAirPollution, Config.OpenWeather.Lat, Config.OpenWeather.Lon, apiKey),
	)

	// #nosec G107
	resp, err := http.Get(url)
	if err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "failed to fetch air pollution data",
			Success: false,
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "failed to fetch air pollution data",
			Success: false,
		})
		return
	}

	var openWeatherData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&openWeatherData); err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "failed to parse air pollution data",
			Success: false,
		})
		return
	}

	components := openWeatherData["list"].([]interface{})[0].(map[string]interface{})["components"].(map[string]interface{})

	airPollutionResponse := &models.AirPollutionResponse{
		Components: map[string]float64{
			"co":    components["co"].(float64),
			"no":    components["no"].(float64),
			"no2":   components["no2"].(float64),
			"o3":    components["o3"].(float64),
			"so2":   components["so2"].(float64),
			"pm2_5": components["pm2_5"].(float64),
			"pm10":  components["pm10"].(float64),
			"nh3":   components["nh3"].(float64),
		},
	}

	Respond(c, http.StatusOK, airPollutionResponse)
}
