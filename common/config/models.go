// config/models.go
package config

type scraperEndpoints struct {
	Division      string `mapstructure:"division"`
	Teacher       string `mapstructure:"teacher"`
	Room          string `mapstructure:"room"`
	DivisionsList string `mapstructure:"divisions_list"`
	TeachersList  string `mapstructure:"teachers_list"`
	RoomsList     string `mapstructure:"rooms_list"`
}

type scraperQuantities struct {
	Divisions int64 `mapstructure:"divisions"`
	Teachers  int64 `mapstructure:"teachers"`
	Rooms     int64 `mapstructure:"rooms"`
}

type ScraperConfig struct {
	BaseUrl    string            `mapstructure:"base_url"`
	Endpoints  scraperEndpoints  `mapstructure:"endpoints"`
	Quantities scraperQuantities `mapstructure:"quantities"`
}

type openWeatherEndpoints struct {
	CurrentWeather      string `mapstructure:"current_weather"`
	ForecastWeather     string `mapstructure:"forecast_weather"`
	CurrentAirPollution string `mapstructure:"current_air_pollution"`
}

type openWeatherConfig struct {
	BaseUrl   string               `mapstructure:"base_url"`
	Endpoints openWeatherEndpoints `mapstructure:"endpoints"`
	Lat       float64              `mapstructure:"lat"`
	Lon       float64              `mapstructure:"lon"`
}

type APIConfig struct {
	Port        int16            `mapstructure:"port"`
	OpenWeather openWeatherConfig `mapstructure:"open_weather"`
}

type GlobalConfig struct {
	Scraper ScraperConfig `mapstructure:"scraper"`
	API     APIConfig     `mapstructure:"api"`
}