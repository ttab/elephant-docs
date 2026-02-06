package elephantdocs

type Config struct {
	Modules []ModuleConfig     `json:"modules"`
	Schemas *SchemaGroupConfig `json:"schemas,omitempty"`
}

type SchemaGroupConfig struct {
	Title string            `json:"title"`
	Repo  string            `json:"repo"`
	Clone string            `json:"clone,omitempty"`
	Sets  []SchemaSetConfig `json:"sets"`
}

type SchemaSetConfig struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	File  string `json:"file"`
}

type ModuleConfig struct {
	Title   string                   `json:"title"`
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
