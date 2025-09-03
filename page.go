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
	Title    string
	HRef     string
	Active   bool
	Children []MenuItem
}

func (m MenuItem) HasActive() bool {
	for i := range m.Children {
		if m.Children[i].Active || m.Children[i].HasActive() {
			return true
		}
	}

	return false
}
