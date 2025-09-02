package elephantdocs

type Page struct {
	MetaTags   []map[string]string
	Title      string
	Language   string
	Menu       []MenuItem
	Breadcrumb []MenuItem
	Contents   any
}

type MenuItem struct {
	Title  string
	HRef   string
	Active bool
}
