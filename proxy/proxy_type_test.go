package proxy

import "testing"

func TestHTTPSProxyType(t *testing.T) {
	for input, want := range map[string]ProxyType{
		"http": ProxyTypeHTTP,
		"https": ProxyTypeHTTPS,
		" HTTPS ": ProxyTypeHTTPS,
		"socks4": ProxyTypeSOCKS4,
		"socks5": ProxyTypeSOCKS5,
	} {
		got,err:=ProxyTypeFromString(input)
		if err != nil || got != want || !got.IsValid() {t.Fatalf("parse %q = %q (%v), want %q",input,got,err,want)}
	}
	if ProxyTypeHTTP==ProxyTypeHTTPS {t.Fatal("HTTP and HTTPS must have different proxy types")}
}
