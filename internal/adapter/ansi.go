package adapter

// stripANSI removes CSI/OSC escape sequences so completion patterns can match
// against the plain text of styled TUI output.
func stripANSI(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		if p[i] != 0x1b {
			out = append(out, p[i])
			continue
		}
		if i+1 >= len(p) {
			break
		}
		switch p[i+1] {
		case '[': // CSI: ESC [ ... final byte 0x40–0x7e
			i += 2
			for i < len(p) && (p[i] < 0x40 || p[i] > 0x7e) {
				i++
			}
		case ']': // OSC: ESC ] ... BEL or ESC \
			i += 2
			for i < len(p) && p[i] != 0x07 && p[i] != 0x1b {
				i++
			}
			if i+1 < len(p) && p[i] == 0x1b && p[i+1] == '\\' {
				i++
			}
		default: // two-byte escape
			i++
		}
	}
	return out
}
