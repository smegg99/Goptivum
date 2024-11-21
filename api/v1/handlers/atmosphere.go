// handlers/atmosphere.go
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"smuggr.xyz/goptivum/common/models"

	"github.com/gin-gonic/gin"
)

// Used to simplify getting the API response
var openWeatherData struct {
	List []struct {
		Dt   int64 `json:"dt"`
		Main struct {
			Temp    float64 `json:"temp"`
			TempMin float64 `json:"temp_min"`
			TempMax float64 `json:"temp_max"`
		} `json:"main"`
		Weather []struct {
			Main        string `json:"main"`
			Description string `json:"description"`
		} `json:"weather"`
	} `json:"list"`
	City struct {
		Name    string  `json:"name"`
		Sunrise float64 `json:"sunrise"`
		Sunset  float64 `json:"sunset"`
	} `json:"city"`
}

func fetchLocalAirPollutionData() (*models.AirPollutionResponse, error) {
    url := fmt.Sprintf("%s%s",
        Config.LocalWeatherStation.BaseUrl,
        Config.LocalWeatherStation.Endpoints.CurrentAirPollution,
    )

    // #nosec G107
    resp, err := http.Get(url)
    if err != nil {
        return nil, fmt.Errorf("error reaching local weather station: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("local weather station returned status: %d", resp.StatusCode)
    }

    var localData map[string]float64
    if err := json.NewDecoder(resp.Body).Decode(&localData); err != nil {
        return nil, fmt.Errorf("error parsing local weather station data: %w", err)
    }

    return &models.AirPollutionResponse{
		// Basically only pm2_5 and pm10 are needed to calculate the AQI
        Components: map[string]float64{
            "pm2_5": localData["pm025"],
            "pm10":  localData["pm010"],
            "pm100": localData["pm100"],
        },
    }, nil
}

func fetchOpenWeatherAirPollutionData() (*models.AirPollutionResponse, error) {
    apiKey := os.Getenv("OPENWEATHER_API_KEY")
    url := fmt.Sprintf("%s%s",
        Config.OpenWeather.BaseUrl,
        fmt.Sprintf(Config.OpenWeather.Endpoints.CurrentAirPollution, Config.OpenWeather.Lat, Config.OpenWeather.Lon, apiKey),
    )

    // #nosec G107
    resp, err := http.Get(url)
    if err != nil {
        return nil, fmt.Errorf("error reaching OpenWeather: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("OpenWeather returned status: %d", resp.StatusCode)
    }

    var openWeatherData map[string]interface{}
    if err := json.NewDecoder(resp.Body).Decode(&openWeatherData); err != nil {
        return nil, fmt.Errorf("error parsing OpenWeather data: %w", err)
    }

    components := openWeatherData["list"].([]interface{})[0].(map[string]interface{})["components"].(map[string]interface{})
    return &models.AirPollutionResponse{
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
    }, nil
}

func WeatherForecastHandler(c *gin.Context) {
	lang := c.DefaultQuery("lang", "en")
	units := c.DefaultQuery("units", "metric")
	apiKey := os.Getenv("OPENWEATHER_API_KEY")

	url := fmt.Sprintf("%s%s",
		Config.OpenWeather.BaseUrl,
		fmt.Sprintf(Config.OpenWeather.Endpoints.ForecastWeather, Config.OpenWeather.Lat, Config.OpenWeather.Lon, apiKey, lang, units, 40),
	)

	// Fetch data from the OpenWeather API
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

	if err := json.NewDecoder(resp.Body).Decode(&openWeatherData); err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "failed to parse forecast data",
			Success: false,
		})
		return
	}

	now := time.Now().UTC()
	forecastResponse := &models.ForecastResponse{
		Name: openWeatherData.City.Name,
	}

	daysProcessed := 0
	for _, entry := range openWeatherData.List {
		entryTime := time.Unix(entry.Dt, 0).UTC()

		// Skip if it's today or not close to 12:00 PM
		if entryTime.Day() == now.Day() || entryTime.Hour() != 12 {
			continue
		}

		// Create a forecast using your models
		forecastResponse.Forecast = append(forecastResponse.Forecast, &models.Forecast{
			Condition: &models.Condition{
				Name:        entry.Weather[0].Main,
				Description: entry.Weather[0].Description,
			},
			Temperature: &models.Temperature{
				Current: entry.Main.Temp,
				Min:     entry.Main.TempMin,
				Max:     entry.Main.TempMax,
			},
			Sunrise:   int64(openWeatherData.City.Sunrise),
			Sunset:    int64(openWeatherData.City.Sunset),
			DayOfWeek: int64(entryTime.Weekday()),
		})

		daysProcessed++
		if daysProcessed == 3 {
			break
		}
	}

	// Respond with the forecast
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
    var airPollutionResponse *models.AirPollutionResponse
    var err error

    if Config.UseLocalWeatherStation {
        airPollutionResponse, err = fetchLocalAirPollutionData()
        if err != nil {
            fmt.Println("local weather station failed:", err)
        }
    }

    if airPollutionResponse == nil {
        airPollutionResponse, err = fetchOpenWeatherAirPollutionData()
        if err != nil {
            Respond(c, http.StatusInternalServerError, models.APIResponse{
                Message: "failed to fetch air pollution data from all sources",
                Success: false,
            })
            return
        }
    }

    Respond(c, http.StatusOK, airPollutionResponse)
}