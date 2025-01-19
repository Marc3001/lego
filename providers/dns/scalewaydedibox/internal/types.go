package internal

// Record holds the API representation of a Domain Record.
type Record struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"data"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority,omitempty"`
}

// Version holds the API representation of a zone version.
type Version struct {
	UUIDRef      string `json:"uuid_ref"`
	Name         string `json:"Name"`
	CreationDate string `json:"creation_date"`
	Domain       struct {
		Ref string `json:"$ref"`
	} `json:"domain"`
	Zone struct {
		Ref string `json:"$ref"`
	} `json:"zone"`
	Active bool `json:"active"`
}
