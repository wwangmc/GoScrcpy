package scrcpy

func (s *Session) SerialNumber() string   { return s.Serial }
func (s *Session) Info() Metadata         { return s.Metadata }
func (s *Session) Encoding() string       { return "h264" }
func (s *Session) PointerIntervalMS() int { return 0 }
func (s *Session) Controls() []string {
	return []string{"touch", "scroll", "text", "keyboard", "back", "home", "recent", "power", "volumeDown", "volumeUp"}
}
func (s *Session) Settings() []string           { return []string{"maxSize", "maxFPS", "bitRate"} }
func (s *Session) ReadFrame() ([]byte, error)   { return ReadPacket(s.Video) }
func (s *Session) DoneChannel() <-chan struct{} { return s.Done }
