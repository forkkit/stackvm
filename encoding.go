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

func putVarCode(buf []byte, arg uint32, code uint8) (n int) {
	var (
		tmp [6]byte
		i   int
	)
	tmp[i] = code & 0x7f
	if code&0x80 != 0 {
		i++
		for ; i < len(tmp); i++ {
			tmp[i] = byte(arg) | 0x80
			arg >>= 7
			if arg == 0 {
				break
			}
		}
	}
	for i >= 0 && n < len(buf) {
		buf[n] = tmp[i]
		i--
		n++
	}
	return n
}
