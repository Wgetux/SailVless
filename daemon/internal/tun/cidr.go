package tun

import "net/netip"

// subtract returns base minus holes, as a list of prefixes. Used to carve
// networks that must go through the tunnel out of the excluded LAN ranges.
func subtract(base, holes []netip.Prefix) []netip.Prefix {
	var out []netip.Prefix
	for _, b := range base {
		out = append(out, cut(b.Masked(), holes)...)
	}
	return out
}

func cut(b netip.Prefix, holes []netip.Prefix) []netip.Prefix {
	overlaps := false
	for _, h := range holes {
		h = h.Masked()
		if h.Addr().Is4() != b.Addr().Is4() {
			continue
		}
		if h.Bits() <= b.Bits() && h.Contains(b.Addr()) {
			return nil // b lies entirely inside a hole
		}
		if b.Overlaps(h) {
			overlaps = true
		}
	}
	if !overlaps {
		return []netip.Prefix{b}
	}
	// split b into its two halves and recurse
	bits := b.Bits() + 1
	lo := netip.PrefixFrom(b.Addr(), bits)
	hiAddr := b.Addr()
	// set the bit at position b.Bits() to get the upper half
	raw := hiAddr.AsSlice()
	raw[b.Bits()/8] |= 0x80 >> (b.Bits() % 8)
	hiAddr, _ = netip.AddrFromSlice(raw)
	hi := netip.PrefixFrom(hiAddr, bits)
	return append(cut(lo, holes), cut(hi, holes)...)
}
