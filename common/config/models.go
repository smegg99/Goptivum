// config/models.go
package config

type GeneralConfig struct {
	Debug           bool `mapstructure:"debug"`
	SyncSchedule    bool `mapstructure:"sync_schedule"`
	RelayOnDuration int `mapstructure:"relay_on_duration"`
}

type ScraperConfig struct {
	OptivumBaseUrl     string `mapstructure:"optivum_base_url"`
	DivisionEndpoint   string   `mapstructure:"division_endpoint"`
	TeacherEndpoint    string   `mapstructure:"teacher_endpoint"`
	RoomEndpoint       string   `mapstructure:"room_endpoint"`
}

type DevicesConfig struct {
	DisplayAddress uint16 `mapstructure:"display_address"`
	RTCAddress     uint16 `mapstructure:"rtc_address"`
	I2CBus         string `mapstructure:"i2c_bus"`
	RelayPin       string `mapstructure:"relay_pin"`
}

type GlobalConfig struct {
	General GeneralConfig `mapstructure:"general"`
	Scraper ScraperConfig `mapstructure:"scraper"`
	Devices DevicesConfig `mapstructure:"devices"`
}