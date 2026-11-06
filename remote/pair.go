package remote

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"

	"rsc.io/qr"
)

// PairURL is one address a phone can pair with.
type PairURL struct {
	URL   string
	Label string // "LAN", "Tailscale" or "this computer"
}

// tailnet is the CGNAT range that Tailscale assigns addresses from.
var tailnet = netip.MustParsePrefix("100.64.0.0/10")

// PairURLs returns the pairing URLs for a gateway that listens on listen.
// When listen names no host, it returns one URL for each IPv4 address of
// the machine's interfaces. The token is in the URL fragment, which the
// browser never sends to the server or puts in a Referer.
func PairURLs(listen, token string) ([]PairURL, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, fmt.Errorf("remote: listen address %q: %w", listen, err)
	}
	var hosts []netip.Addr
	if addr, err := netip.ParseAddr(host); err == nil && !addr.IsUnspecified() {
		hosts = []netip.Addr{addr}
	} else if host != "" && err != nil {
		// A host name: use it as it is.
		return []PairURL{{URL: pairURL(host, port, token), Label: "LAN"}}, nil
	} else {
		hosts = interfaceAddrs()
	}
	urls := make([]PairURL, 0, len(hosts))
	for _, addr := range hosts {
		urls = append(urls, PairURL{URL: pairURL(addr.String(), port, token), Label: addrLabel(addr)})
	}
	return urls, nil
}

func pairURL(host, port, token string) string {
	return "http://" + net.JoinHostPort(host, port) + "/#t=" + token
}

// interfaceAddrs returns the IPv4 addresses of the up interfaces, the
// non-loopback ones first. Phones on a LAN rarely reach a link-local or
// IPv6 address, so it leaves those out.
func interfaceAddrs() []netip.Addr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out, loopback []netip.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			addr := prefix.Addr()
			switch {
			case !addr.Is4() || addr.IsLinkLocalUnicast():
			case addr.IsLoopback():
				loopback = append(loopback, addr)
			default:
				out = append(out, addr)
			}
		}
	}
	return append(out, loopback...)
}

func addrLabel(addr netip.Addr) string {
	switch {
	case tailnet.Contains(addr):
		return "Tailscale"
	case addr.IsLoopback():
		return "this computer"
	}
	return "LAN"
}

// WriteQR draws text as a QR code with half-block characters, two modules
// per character row. It sets the colors itself, black on white, so the code
// scans on a dark terminal too.
func WriteQR(w io.Writer, text string) error {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return fmt.Errorf("remote: qr: %w", err)
	}
	const quiet = 2 // modules of white border; scanners need some
	var b strings.Builder
	for y := -quiet; y < code.Size+quiet; y += 2 {
		b.WriteString("\x1b[97;40m") // white foreground draws the top module
		for x := -quiet; x < code.Size+quiet; x++ {
			top, bottom := !code.Black(x, y), !code.Black(x, y+1)
			if y+1 >= code.Size+quiet {
				bottom = false // the row below the border is the terminal's
			}
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	_, err = io.WriteString(w, b.String())
	return err
}
