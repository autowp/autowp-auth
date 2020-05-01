package auth

// Host ...
type Host struct {
	Language string `yaml:"language" mapstructure:"language"`
	Hostname string `yaml:"hostname" mapstructure:"hostname"`
	Timezone string `yaml:"timezone" mapstructure:"timezone"`
}
