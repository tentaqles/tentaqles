package sense

// CaptureOn reports whether a client's capture setting lets the hook record anything. value is the
// setting as internal/lobe/settings reads it: only "on" records. "off", and a wrong value (which
// settings reads as its safe value "off"), record nothing.
func CaptureOn(value string) bool { return value == "on" }
