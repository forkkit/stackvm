package stackvm

func readVarCode(buf []byte) (n int, arg uint32, code uint8, ok bool) {
	for i, v := range buf {
		n++
		if v&0x80 == 0 {
			code = uint8(v)
			if i > 0 {
				code |= 0x80
			}
			ok = true
			return
		}
		if i == 5 {
			break
		}
		arg = arg<<7 | uint32(v&0x7f)
	}
	return
}
