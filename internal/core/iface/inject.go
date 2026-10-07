package iface

// UTF16 returns the UTF-16 code units of s, the unit SendInput's KEYEVENTF_UNICODE consumes. Go
// strings are UTF-8, so a non-BMP rune becomes a surrogate pair — two units — and must be sent as
// such, or the character is corrupted.
func UTF16(s string) []uint16 {
	if s == "" {
		return nil
	}
	// UTF-16 encoding of a string is at most len(s) units for ASCII and never more than len(s)
	// for any input (a 4-byte rune is 2 units, a 3-byte rune is 1 unit).
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r < 0x10000 {
			out = append(out, uint16(r))
			continue
		}
		r -= 0x10000
		out = append(out, 0xD800+uint16(r>>10), 0xDC00+uint16(r&0x3FF))
	}
	return out
}
