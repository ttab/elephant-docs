package elephantdocs

type Page struct {
	MetaTags   []map[string]string
	Title      string
	Language   string
	Menu       []MenuItem
	Breadcrumb []MenuItem
	// Protocols is set on the pages that document RPCs, and is what
	// decides whether the header carries the protocol toggle and the
	// tenant picker.
	Protocols *ProtocolSet
	Contents  any
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
