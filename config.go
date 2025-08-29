package elephantdocs

type Config struct {
	Modules []ModuleConfig `json:"modules"`
}

type ModuleConfig struct {
	Name    string                   `json:"name"`
	Clone   string                   `json:"clone,omitempty"`
	APIs    map[string]APIConfig     `json:"apis"`
	Include map[string]IncludeConfig `json:"include"`
}

type APIConfig struct {
	Title string `json:"title"`
}

type IncludeConfig struct {
	From string `json:"from"`
}
