package config

// Read config.json
import (
	"encoding/json"
	"os"
)

type ConfigFrom struct {
	User     string
	Pass     string
	Host     string
	Port     int
	From     string
	Display  string
	Bcc      []string
	Bounce   *string
	Hostname string
	Insecure bool

	AllowBCC bool
}
type Config struct {
	Beanstalk string
	From      map[string]ConfigFrom
}

type Email struct {
	From        string
	To          []string
	BCC         []string
	Subject     string
	Html        string
	Text        string
	HtmlEmbed   map[string]string // file.png => base64(bytes)
	Attachments map[string]string // file.png => base64(bytes)
}

var (
	C Config
)

func Init(f string) error {
	r, e := os.Open(f)
	if e != nil {
		return e
	}
	if e := json.NewDecoder(r).Decode(&C); e != nil {
		return e
	}
	return nil
}
