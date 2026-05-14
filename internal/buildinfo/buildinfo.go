package buildinfo

const defaultSourceURL = "https://github.com/outlook84/openlist-tvbox-gateway"

var (
	Version   = "dev"
	Commit    = ""
	SourceURL = defaultSourceURL
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	SourceURL string `json:"source_url"`
}

func Current() Info {
	sourceURL := SourceURL
	if sourceURL == "" {
		sourceURL = defaultSourceURL
	}
	return Info{
		Version:   Version,
		Commit:    Commit,
		SourceURL: sourceURL,
	}
}
