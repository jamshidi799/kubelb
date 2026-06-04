package configs

import (
	"github.com/spf13/viper"
)

type Config struct {
	LbIp       string
	KubeConfig string
}

func GetConfig() (*Config, error) {
	viper.SetConfigName("config")

	// Add search paths to find the file
	viper.AddConfigPath("/etc/kubelb/")
	viper.AddConfigPath("$HOME/.kubelb")
	viper.AddConfigPath(".")

	// Find and read the config file
	err := viper.ReadInConfig()

	// Handle errors
	if err != nil {
		return nil, err
	}

	var c Config
	if err = viper.Unmarshal(&c); err != nil {
		return nil, err
	}

	return &c, nil
}
