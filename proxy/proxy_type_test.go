package proxy

import "testing"

func TestProxyTypeFromString(t *testing.T) {
	tests := map[string]ProxyType{
		"http":    ProxyTypeHTTP,
		"https":   ProxyTypeHTTPS,
		" HTTPS ": ProxyTypeHTTPS,
		"socks4":  ProxyTypeSOCKS4,
		"socks5":  ProxyTypeSOCKS5,
		"socks5h": ProxyTypeSOCKS5,
		"s5":      ProxyTypeSOCKS5,
		"sock5":   ProxyTypeSOCKS5,
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := ProxyTypeFromString(input)
			if err != nil {
				t.Fatalf("ProxyTypeFromString(%q) error = %v", input, err)
			}
			if got != want {
				t.Fatalf("ProxyTypeFromString(%q) = %q, want %q", input, got, want)
			}
			if !got.IsValid() {
				t.Fatalf("ProxyTypeFromString(%q) returned invalid proxy type %q", input, got)
			}
		})
	}
}

func TestParseProxyConfigSocks5h(t *testing.T) {
	cfg, err := ParseProxyConfig("socks5h://user:pass@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("ParseProxyConfig() error = %v", err)
	}

	if cfg.Type != ProxyTypeSOCKS5 {
		t.Fatalf("proxy type = %q, want %q", cfg.Type, ProxyTypeSOCKS5)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 1080 {
		t.Fatalf("proxy address = %s:%d, want 127.0.0.1:1080", cfg.Host, cfg.Port)
	}
	if cfg.Username == nil || *cfg.Username != "user" {
		t.Fatalf("proxy username = %v, want user", cfg.Username)
	}
	if cfg.Password == nil || *cfg.Password != "pass" {
		t.Fatalf("proxy password was not parsed correctly")
	}
}

func TestHTTPAndHTTPSProxyTypesAreDifferent(t *testing.T) {
	if ProxyTypeHTTP == ProxyTypeHTTPS {
		t.Fatal("HTTP and HTTPS must have different proxy types")
	}
}
