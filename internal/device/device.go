package device

// Device is the shared device list used by every transport and the same UI.
type Device struct {
	Serial     string   `json:"serial"`
	State      string   `json:"state"`
	Model      string   `json:"model"`
	Product    string   `json:"product"`
	Transport  string   `json:"transport"`
	Platform   string   `json:"platform"`
	Error      string   `json:"error,omitempty"`
	Codec      string   `json:"codec"`
	VideoModes []string `json:"videoModes,omitempty"`
	Controls   []string `json:"controls,omitempty"`
	Settings   []string `json:"settings,omitempty"`
}
