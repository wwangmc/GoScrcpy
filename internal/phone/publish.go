package phone

// PublishFile exposes the same no-overwrite commit used by PocketFiles.
func PublishFile(source, target string) error { return publishFile(source, target) }
